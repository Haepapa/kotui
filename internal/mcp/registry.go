package mcp

import (
	"fmt"
	"strings"
	"sync"

	"github.com/haepapa/kotui/pkg/models"
)

// Registry stores tool definitions and generates system prompt fragments.
// Thread-safe: multiple agents may query it concurrently.
type Registry struct {
	mu    sync.RWMutex
	tools map[string]ToolDef
	order []string // insertion order for stable output
}

func newRegistry() *Registry {
	return &Registry{tools: make(map[string]ToolDef)}
}

// register adds a tool. Returns an error if the name is already taken or
// if the schema is not valid JSON.
func (r *Registry) register(def ToolDef) error {
	if def.Name == "" {
		return fmt.Errorf("mcp: tool name must not be empty")
	}
	if len(def.Schema) > 0 {
		// Ensure schema is valid JSON before storing.
		var probe any
		if err := jsonUnmarshal(def.Schema, &probe); err != nil {
			return fmt.Errorf("mcp: tool %q schema is invalid JSON: %w", def.Name, err)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[def.Name]; exists {
		return fmt.Errorf("mcp: tool %q already registered", def.Name)
	}
	r.tools[def.Name] = def
	r.order = append(r.order, def.Name)
	return nil
}

// lookup returns a tool definition by name.
func (r *Registry) lookup(name string) (ToolDef, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.tools[name]
	return def, ok
}

// listForClearance returns all tools that the given clearance level may invoke,
// preserving insertion order.
func (r *Registry) listForClearance(c models.Clearance) []ToolDef {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []ToolDef
	for _, name := range r.order {
		def := r.tools[name]
		if c >= def.Clearance {
			out = append(out, def)
		}
	}
	return out
}

// systemPromptFragment generates a Markdown block describing available tools
// for injection into an agent's system prompt.
func (r *Registry) systemPromptFragment(c models.Clearance) string {
	tools := r.listForClearance(c)
	if len(tools) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("Call tools by responding with a JSON object on its own line:\n")
	sb.WriteString("```json\n{\"tool\": \"<name>\", \"args\": {<args>}}\n```\n\n")
	for _, t := range tools {
		sb.WriteString(fmt.Sprintf("### `%s` (clearance: %s)\n", t.Name, t.Clearance.String()))
		sb.WriteString(t.Description + "\n")
		if len(t.Schema) > 0 {
			sb.WriteString("\nSchema:\n```json\n")
			sb.Write(t.Schema)
			sb.WriteString("\n```\n")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
