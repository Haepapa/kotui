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
		// Sanitize any literal control chars a model may have embedded in
		// string values before attempting to unmarshal.
		line = sanitizeJSONControlChars(line)
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

	// Use a JSON-string-aware extractor so that { } inside string values
	// (e.g. Python dict literals in a "content" field) don't fool the brace
	// counter into terminating early.
	candidate := extractJSONObject(stripped)
	if candidate == "" {
		return nil
	}

	// Sanitize literal control characters (newlines, tabs) inside JSON string
	// values. Models generating multiline file content often embed raw newlines
	// in the "content" field instead of \n escape sequences, producing invalid
	// JSON that json.Unmarshal would otherwise reject.
	candidate = sanitizeJSONControlChars(candidate)

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

// extractJSONObject finds the first outermost {...} object in text, correctly
// skipping { and } characters that appear inside JSON string values.
// Returns the raw JSON text including the braces, or "" if none found.
func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escaped := false

	for i := start; i < len(text); i++ {
		c := text[i]

		if escaped {
			escaped = false
			continue
		}
		if c == '\\' && inString {
			escaped = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}

		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start : i+1]
			}
		}
	}
	return ""
}

// sanitizeJSONControlChars replaces literal control characters (newline, carriage
// return, tab) that appear inside JSON string values with their proper escape
// sequences. This fixes tool calls emitted by models that put raw newlines inside
// multiline file-content strings rather than writing \n escape sequences.
//
// Characters outside of string values are left untouched so that structural
// whitespace is preserved.
func sanitizeJSONControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			b.WriteByte(c)
			continue
		}
		if c == '\\' && inString {
			escaped = true
			b.WriteByte(c)
			continue
		}
		if c == '"' {
			inString = !inString
			b.WriteByte(c)
			continue
		}
		if inString {
			switch c {
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				b.WriteByte(c)
			}
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
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

// stripToolCallLines removes signal-only JSON lines from a response so only
// human-readable prose remains for storage and display.
// Strips: tool call objects, confidence signal objects, escalation objects,
// and propose_handbook objects.
// Task-list arrays ([{...}]) are intentionally NOT stripped here — the
// orchestrator's parseTaskList() must be able to read them from the returned
// string. Task-list filtering for display is handled by the frontend's
// stripSignalLines() function (streaming bubble) and never reaches the user
// in the persisted summary because the decomposed text is only forwarded to
// the chat when len(tasks)==0 (in which case there is no task list anyway).
// Also handles concatenated JSON objects on one line (e.g. a confidence
// object immediately followed by a tool call on the same line).
func stripToolCallLines(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		if isSignalLine(strings.TrimSpace(line)) {
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// humanReadableDecomposed strips all signal lines AND task-list JSON arrays from
// a decomposed response, leaving only the prose the Lead wrote around them.
// This is used to extract a social preamble/acknowledgement for display to the
// Boss before workers begin execution.
func humanReadableDecomposed(text string) string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		// Skip signal lines (tool calls, confidence, escalation)
		if isSignalLine(trimmed) {
			continue
		}
		// Skip task-list JSON arrays
		if strings.HasPrefix(trimmed, "[") {
			var check []TaskItem
			if json.Unmarshal([]byte(trimmed), &check) == nil && len(check) > 0 {
				continue
			}
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}
// JSON signal objects that should be hidden from the user.
// Recognised signals: tool call {"tool":...}, confidence {"confidence_score":...},
// escalation {"escalation_needed":true,...}, propose_handbook {"propose_handbook":...}.
// Task-list arrays are NOT treated as signal lines here (see stripToolCallLines).
func isSignalLine(line string) bool {
	if line == "" {
		return false
	}
	// Only process lines starting with '{' (object signals).
	// Lines starting with '[' (task lists) are deliberately kept.
	if line[0] != '{' {
		return false
	}
	// Scan and decode one or more consecutive JSON objects.
	// If every object on the line is a recognised signal, strip the line.
	remaining := line
	foundSignal := false
	for len(remaining) > 0 {
		remaining = strings.TrimSpace(remaining)
		if len(remaining) == 0 {
			break
		}
		if remaining[0] != '{' {
			return false // non-object content after signal — keep the line
		}
		var probe map[string]any
		dec := json.NewDecoder(strings.NewReader(remaining))
		if err := dec.Decode(&probe); err != nil {
			return false // not valid JSON — keep the line
		}
		_, hasToolKey := probe["tool"]
		_, hasCS := probe["confidence_score"]
		_, hasPropose := probe["propose_handbook"]
		esc, _ := probe["escalation_needed"].(bool)
		if !hasToolKey && !hasCS && !hasPropose && !esc {
			return false // valid object but not a recognised signal — keep
		}
		foundSignal = true
		remaining = remaining[dec.InputOffset():]
	}
	return foundSignal
}
