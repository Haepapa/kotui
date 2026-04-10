// Package mcp implements the Model Context Protocol tool-calling framework.
//
// Architecture:
//
//	Registry  — stores tool definitions; generates system-prompt fragments
//	PermissionGate — enforces clearance hierarchy (Lead > Specialist > Trial)
//	Sandbox   — scopes all file paths to the active project workspace
//	Executor  — validates args, invokes handlers, retries, escalates
//	Engine    — composes the above into the public API consumed by agents
//
// Immutable Laws (tested in mcp_test.go; must never regress):
//  1. A Trial agent CANNOT invoke a Specialist or Lead tool.
//  2. A Specialist agent CANNOT invoke a Lead tool.
//  3. A file path outside the sandbox root CANNOT be resolved.
//  4. A tool handler that fails 3 times MUST produce an EscalationError.
package mcp

import (
	"context"
	"encoding/json"

	"github.com/haepapa/kotui/pkg/models"
)

// Handler is the function signature for all MCP tool implementations.
type Handler func(ctx context.Context, args map[string]any) (string, error)

// ToolDef is the static description of a registered tool.
// The Handler field is set at registration time and is never serialised.
type ToolDef struct {
	Name        string          // unique snake_case identifier
	Description string          // injected into the agent system prompt
	Schema      json.RawMessage // JSON Schema {"type":"object","properties":{...},"required":[...]}
	Clearance   models.Clearance
	Handler     Handler `json:"-"`
}

// MCPError is a rich error returned by tool handlers. It carries a
// recoverability flag and an actionable suggestion so the executor can decide
// whether to keep retrying and so the agent can self-correct.
type MCPError struct {
	// IsRecoverable indicates whether the agent could fix this by changing its
	// approach (e.g. wrong path → list first). Non-recoverable errors (e.g.
	// permission denied) skip remaining retries and escalate immediately.
	IsRecoverable bool
	// Suggestion is a short, actionable instruction for the agent, e.g.
	// "Use operation=list to find the correct filename first."
	Suggestion string
	// Underlying is the raw OS/library error.
	Underlying error
}

func (e *MCPError) Error() string {
	base := e.Underlying.Error()
	if e.Suggestion != "" {
		return base + " — " + e.Suggestion
	}
	return base
}

func (e *MCPError) Unwrap() error { return e.Underlying }

// EscalationError is returned when a tool handler fails after all retries.
// The orchestrator must pause the parent task and notify the Boss.
type EscalationError struct {
	ToolName string
	Attempts int
	Last     error
}

func (e *EscalationError) Error() string {
	return "mcp: escalation after " + itoa(e.Attempts) + " attempts on tool " + e.ToolName + ": " + e.Last.Error()
}

func (e *EscalationError) Unwrap() error { return e.Last }

// PermissionError is returned when an agent's clearance is insufficient.
type PermissionError struct {
	AgentClearance    models.Clearance
	RequiredClearance models.Clearance
	ToolName          string
}

func (e *PermissionError) Error() string {
	return "mcp: permission denied — agent clearance " + e.AgentClearance.String() +
		" cannot invoke tool " + e.ToolName + " (requires " + e.RequiredClearance.String() + ")"
}

// SandboxError is returned when a path escapes the project workspace.
type SandboxError struct {
	Path string
	Root string
}

func (e *SandboxError) Error() string {
	return "mcp: sandbox violation — path " + e.Path + " escapes root " + e.Root
}

// Engine is the top-level MCP coordinator.
// Construct one per application lifetime via New().
type Engine struct {
	registry *Registry
	gate     *PermissionGate
	sandbox  *Sandbox
	executor *Executor
}

// New creates a fully wired Engine.
// sandboxRoot must be the absolute path to the active project workspace
// (e.g. "/data/projects/my-project"). Pass "" to disable sandbox enforcement
// (useful in tests that don't touch the filesystem).
func New(sandboxRoot string) *Engine {
	r := newRegistry()
	g := newPermissionGate()
	s := newSandbox(sandboxRoot)
	e := newExecutor(r, g, s)
	return &Engine{registry: r, gate: g, sandbox: s, executor: e}
}

// Register adds a tool to the registry.
// Returns an error if the name is already registered or the schema is invalid JSON.
func (eng *Engine) Register(def ToolDef) error {
	return eng.registry.register(def)
}

// Execute invokes a tool on behalf of an agent with the given clearance.
// Returns an EscalationError after maxAttempts failures.
func (eng *Engine) Execute(ctx context.Context, clearance models.Clearance, call models.ToolCall) (models.ToolResult, error) {
	return eng.executor.execute(ctx, clearance, call)
}

// SystemPromptFragment returns a Markdown block describing available tools for
// the given clearance level. Inject this into the agent's system prompt.
func (eng *Engine) SystemPromptFragment(clearance models.Clearance) string {
	return eng.registry.systemPromptFragment(clearance)
}

// Sandbox returns the underlying Sandbox so callers can resolve paths before
// constructing tool args (e.g. when the agent returns a relative path).
func (eng *Engine) Sandbox() *Sandbox { return eng.sandbox }

// ToolInfo is a serialisable summary of a registered MCP tool, used by the
// frontend to display the tool catalogue.
type ToolInfo struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Clearance   string          `json:"clearance"`
	Schema      json.RawMessage `json:"schema"`
}

// ListTools returns metadata for all tools accessible at the given clearance
// level, in registration order.
func (eng *Engine) ListTools(clearance models.Clearance) []ToolInfo {
	defs := eng.registry.listForClearance(clearance)
	out := make([]ToolInfo, len(defs))
	for i, d := range defs {
		out[i] = ToolInfo{
			Name:        d.Name,
			Description: d.Description,
			Clearance:   d.Clearance.String(),
			Schema:      d.Schema,
		}
	}
	return out
}

// itoa converts a small int to string without importing strconv everywhere.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 4)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}
