package orchestrator

import (
	"encoding/json"
	"strings"

	"github.com/haepapa/kotui/pkg/models"
)

// EscalationSignal is the structured payload emitted by an agent when it
// cannot handle a task within its capability ceiling.
type EscalationSignal struct {
	EscalationNeeded    bool   `json:"escalation_needed"`
	Reason              string `json:"reason"`
	CapabilityRequired  string `json:"capability_required"`
}

// TaskItem is one sub-task in a Lead decomposition list.
type TaskItem struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Assignee    string `json:"assignee"` // "lead" | "specialist"
}

// parseToolCall scans text for the first line containing a valid MCP tool call:
//
//	{"tool": "name", "args": {...}}
//
// Returns nil if no tool call is found.
func parseToolCall(text string) *models.ToolCall {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var raw struct {
			Tool string         `json:"tool"`
			Args map[string]any `json:"args"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		if raw.Tool == "" {
			continue
		}
		if raw.Args == nil {
			raw.Args = map[string]any{}
		}
		return &models.ToolCall{
			ToolName: raw.Tool,
			Args:     raw.Args,
		}
	}
	return nil
}

// parseEscalation scans text for the escalation_needed signal defined in
// handbook.md. Returns nil if the signal is not present.
func parseEscalation(text string) *EscalationSignal {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var sig EscalationSignal
		if err := json.Unmarshal([]byte(line), &sig); err != nil {
			continue
		}
		if sig.EscalationNeeded {
			return &sig
		}
	}
	return nil
}

// parseTaskList tries to extract a JSON array of TaskItems from agent output.
// Agents are prompted to emit a JSON array on a single line when decomposing.
// Returns nil if no valid list is found.
func parseTaskList(text string) []TaskItem {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") {
			continue
		}
		var tasks []TaskItem
		if err := json.Unmarshal([]byte(line), &tasks); err != nil {
			continue
		}
		if len(tasks) > 0 {
			return tasks
		}
	}
	return nil
}

// stripToolCallLines removes all detected tool call lines from a response so
// only the human-readable prose remains for display.
func stripToolCallLines(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{") {
			var probe map[string]any
			if json.Unmarshal([]byte(trimmed), &probe) == nil {
				if _, hasToolKey := probe["tool"]; hasToolKey {
					continue
				}
				if esc, _ := probe["escalation_needed"].(bool); esc {
					continue
				}
			}
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
