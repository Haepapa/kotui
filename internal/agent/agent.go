// Package agent manages the full lifecycle of Kotui agents:
// identity filesystem, system prompt composition, journaling, and
// skill proposals. Phase 6 wires in the Ollama client and Dispatcher
// to enable actual inference.
package agent

import (
	"fmt"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// Agent is the runtime representation of a spawned agent.
// It holds identity, the current compiled system prompt, and the
// capability-ceiling description used for escalation decisions.
type Agent struct {
	ID        string
	Name      string
	Role      models.AgentRole
	Model     string
	ProjectID string

	paths       IdentityPaths
	dataDir     string
	spawnedAt   time.Time
	handbookPath string

	// instruction is the currently active compiled system prompt.
	// It is fully replaced on Culture Update (never patched in-place).
	instruction string
}

// SpawnConfig carries all parameters needed to bring an agent online.
type SpawnConfig struct {
	ID                  string
	Name                string
	Role                models.AgentRole
	Model               string
	ProjectID           string
	DataDir             string
	CompanyIdentityPath string // path to COMPANY_IDENTITY.md
	HandbookPath        string // path to user-edited handbook.md; empty falls back to embedded
	MCPFragment         string // tool descriptions from mcp.Engine.SystemPromptFragment()
	PastExperience      string // recalled journal entries formatted by memory.FormatRecall
}

// Spawn initialises an agent:
//  1. Creates (or loads) the identity filesystem
//  2. Composes and writes the system prompt (instruction.md)
//
// It does NOT load the Ollama model — the orchestrator (Phase 6) manages
// model keep_alive separately.
func Spawn(cfg SpawnConfig) (*Agent, error) {
	paths := agentPaths(cfg.DataDir, cfg.ID)

	if err := initIdentity(paths, cfg.ID, cfg.Name, cfg.Role, cfg.Model); err != nil {
		return nil, fmt.Errorf("agent.Spawn: init identity for %s: %w", cfg.ID, err)
	}

	a := &Agent{
		ID:           cfg.ID,
		Name:         cfg.Name,
		Role:         cfg.Role,
		Model:        cfg.Model,
		ProjectID:    cfg.ProjectID,
		paths:        paths,
		dataDir:      cfg.DataDir,
		spawnedAt:    time.Now(),
		handbookPath: cfg.HandbookPath,
	}

	prompt, err := compose(paths, cfg.ID, cfg.CompanyIdentityPath, cfg.HandbookPath, cfg.MCPFragment, cfg.PastExperience)
	if err != nil {
		return nil, fmt.Errorf("agent.Spawn: compose prompt for %s: %w", cfg.ID, err)
	}
	if err := writeInstruction(paths, prompt); err != nil {
		return nil, fmt.Errorf("agent.Spawn: write instruction for %s: %w", cfg.ID, err)
	}
	a.instruction = prompt
	return a, nil
}

// SystemPrompt returns the current compiled system prompt.
// This must be injected into every Ollama chat request as the system message.
func (a *Agent) SystemPrompt() string { return a.instruction }

// Clearance maps the agent's Role to the corresponding MCP clearance level.
func (a *Agent) Clearance() models.Clearance { return RoleClearance(a.Role) }

// CultureUpdate fully replaces the agent's instruction with a new prompt
// that incorporates updated company values or handbook. This is a COMPLETE
// replacement, not a patch — LLMs must receive the new system prompt on the
// next turn. companyIdentityPath must point to the updated COMPANY_IDENTITY.md.
func (a *Agent) CultureUpdate(companyIdentityPath, handbookPath, mcpFragment string) error {
	a.handbookPath = handbookPath
	prompt, err := compose(a.paths, a.ID, companyIdentityPath, handbookPath, mcpFragment, "")
	if err != nil {
		return fmt.Errorf("agent.CultureUpdate %s: %w", a.ID, err)
	}
	if err := writeInstruction(a.paths, prompt); err != nil {
		return fmt.Errorf("agent.CultureUpdate %s: write instruction: %w", a.ID, err)
	}
	a.instruction = prompt
	return nil
}

// Teardown writes a journal entry and returns the keep_alive directive
// the orchestrator should send to Ollama when unloading this agent's model.
// Returns 0 meaning "unload immediately" — the orchestrator decides if it
// wants to honour this or keep the model warm.
func (a *Agent) Teardown(entry JournalEntry) (keepAlive int, err error) {
	if entry.Date.IsZero() {
		entry.Date = time.Now()
	}
	if wErr := writeJournal(a.paths, entry); wErr != nil {
		return 0, fmt.Errorf("agent.Teardown %s: journal: %w", a.ID, wErr)
	}
	return 0, nil
}

// Paths returns the agent's identity paths (exported for Phase 6 use).
func (a *Agent) Paths() IdentityPaths { return a.paths }

// RoleClearance maps an AgentRole to its MCP Clearance level.
func RoleClearance(role models.AgentRole) models.Clearance {
	switch role {
	case models.RoleLead:
		return models.ClearanceLead
	case models.RoleSpecialist, models.RoleWatchman:
		return models.ClearanceSpecialist
	default:
		return models.ClearanceTrial
	}
}
