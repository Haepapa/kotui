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
	ID            string `json:"id"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	Assignee      string `json:"assignee"`      // "lead" | "specialist"
	Justification string `json:"justification"` // why this agent/approach was chosen
}

// ConfidenceSignal is emitted by an agent on a dedicated line before a tool
// call to assert how certain it is about the planned action.
// CS ≥ 0.7: proceed. CS < 0.7: orchestrator surfaces a consultation request.
type ConfidenceSignal struct {
	ConfidenceScore float64 `json:"confidence_score"`
	Reason          string  `json:"reason"`
}

// parseConfidenceSignal scans response lines for a confidence signal emitted
// per the handbook Confidence Assessment protocol.  Uses a fast
// strings.Contains pre-check to avoid JSON parsing on every line.
// Returns nil if no signal is found.
func parseConfidenceSignal(text string) *ConfidenceSignal {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "confidence_score") {
			continue
		}
		var sig ConfidenceSignal
		if err := json.Unmarshal([]byte(line), &sig); err != nil {
			continue
		}
		return &sig
	}
	return nil
}


//
//	{"tool": "name", "args": {...}}
//
// The call may be on a single line OR inside a fenced ```json block.
// Returns nil if no tool call is found.
func parseToolCall(text string) *models.ToolCall {
	// First pass: look for a single-line tool call anywhere in the text.
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

	// Second pass: try to find a JSON block inside a ```json ... ``` fence or
	// any multi-line block that begins with { and ends with }.
	// This handles the case where the model formats the tool call across lines.
	if tc := parseToolCallFromBlock(text); tc != nil {
		return tc
	}

	return nil
}

// parseToolCallFromBlock tries to extract a tool call from a multi-line JSON
// block. Handles ```json fences and bare { ... } blocks.
func parseToolCallFromBlock(text string) *models.ToolCall {
	// Strip ```json fences if present.
	stripped := text
	if idx := strings.Index(text, "```"); idx >= 0 {
		end := strings.Index(text[idx+3:], "```")
		if end >= 0 {
			block := text[idx+3 : idx+3+end]
			// Remove language tag (e.g. "json\n")
			if nl := strings.Index(block, "\n"); nl >= 0 {
				block = block[nl+1:]
			}
			stripped = strings.TrimSpace(block)
		}
	}

	// Find the outermost { ... } span and try to parse as tool call.
	start := strings.Index(stripped, "{")
	if start < 0 {
		return nil
	}
	// Walk forward counting brace depth to find the matching close.
	depth := 0
	end := -1
	for i := start; i < len(stripped); i++ {
		switch stripped[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return nil
	}

	candidate := strings.ReplaceAll(stripped[start:end+1], "\n", " ")
	var raw struct {
		Tool string         `json:"tool"`
		Args map[string]any `json:"args"`
	}
	if err := json.Unmarshal([]byte(candidate), &raw); err != nil {
		return nil
	}
	if raw.Tool == "" {
		return nil
	}
	if raw.Args == nil {
		raw.Args = map[string]any{}
	}
	return &models.ToolCall{
		ToolName: raw.Tool,
		Args:     raw.Args,
	}
}

// extractThinkBlocks separates <think>...</think> content from the main response.
// Handles multiple blocks and unclosed blocks (still streaming).
// Returns (thinkContent, cleanedResponse).
func extractThinkBlocks(text string) (think, response string) {
	const openTag = "<think>"
	const closeTag = "</think>"
	var thinkBuf, resBuf strings.Builder
	rest := text
	for {
		start := strings.Index(rest, openTag)
		if start < 0 {
			resBuf.WriteString(rest)
			break
		}
		resBuf.WriteString(rest[:start])
		rest = rest[start+len(openTag):]
		end := strings.Index(rest, closeTag)
		if end < 0 {
			// Unclosed block — treat remainder as thinking (still streaming)
			thinkBuf.WriteString(rest)
			break
		}
		thinkBuf.WriteString(rest[:end])
		rest = rest[end+len(closeTag):]
	}
	return strings.TrimSpace(thinkBuf.String()), strings.TrimSpace(resBuf.String())
}
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

// HandbookProposal is the structured payload emitted by the Lead Optimizer
// when it identifies improvements to the team handbook.
type HandbookProposal struct {
	ProposeHandbook bool   `json:"propose_handbook"`
	Diff            string `json:"diff"` // Proposed markdown section to append
}

// parseHandbookProposal scans response lines for a handbook proposal signal.
// Returns nil if no proposal is found or ProposeHandbook is false.
func parseHandbookProposal(text string) *HandbookProposal {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") || !strings.Contains(line, "propose_handbook") {
			continue
		}
		var sig HandbookProposal
		if err := json.Unmarshal([]byte(line), &sig); err != nil {
			continue
		}
		if sig.ProposeHandbook && sig.Diff != "" {
			return &sig
		}
	}
	return nil
}


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
// Also strips confidence signal lines and escalation lines.
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
				if _, hasCS := probe["confidence_score"]; hasCS {
					continue
				}
				if _, hasPropose := probe["propose_handbook"]; hasPropose {
					continue
				}
			}
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
