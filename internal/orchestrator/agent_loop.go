package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/pkg/models"
)

const maxToolLoops = 20 // prevent infinite tool-call cycles

// RunningAgent is an in-memory agent with its full conversation history.
// It is created when the agent spawns and discarded on teardown.
type RunningAgent struct {
	AgentID   string
	AgentName string
	Model     string
	Clearance models.Clearance
	KeepAlive *ollama.KeepAlive

	inferrer  Inferrer
	mcpEng    *mcp.Engine
	history   []ollama.ChatMessage
	sysPrompt string

	// OnRaw, if non-nil, receives raw activity events (API calls, tool calls,
	// tool results, errors). Callers set this before calling Turn/TurnStream to
	// route log entries to the appropriate Dispatcher channel.
	OnRaw func(kind models.MessageKind, content string)
}

// rawLog calls OnRaw if set. Safe to call when OnRaw is nil.
func (ra *RunningAgent) rawLog(kind models.MessageKind, content string) {
	if ra.OnRaw != nil {
		ra.OnRaw(kind, content)
	}
}

// boolPtr returns a pointer to the given bool — used for optional JSON fields.
func boolPtr(b bool) *bool { return &b }

// newRunningAgent creates a RunningAgent from a spawned agent.Agent.
func newRunningAgent(
	agentID, name, model string,
	clearance models.Clearance,
	keepAlive *ollama.KeepAlive,
	sysPrompt string,
	inferrer Inferrer,
	mcpEng *mcp.Engine,
) *RunningAgent {
	return &RunningAgent{
		AgentID:   agentID,
		AgentName: name,
		Model:     model,
		Clearance: clearance,
		KeepAlive: keepAlive,
		inferrer:  inferrer,
		mcpEng:    mcpEng,
		sysPrompt: sysPrompt,
		history:   []ollama.ChatMessage{},
	}
}

// Turn sends a user message, runs the agentic loop (tool calls), and returns
// the final assistant text.
//
// The loop continues as long as the model emits tool call JSON. Tool results
// are appended and the model is re-called. After maxToolLoops iterations the
// loop is forcibly terminated.
//
// Returns EscalationNeededError if the agent signals escalation_needed.
func (ra *RunningAgent) Turn(ctx context.Context, userContent string) (string, error) {
	ra.history = append(ra.history, ollama.ChatMessage{
		Role:    "user",
		Content: userContent,
	})

	for loop := 0; loop < maxToolLoops; loop++ {
		msgs := ra.buildMessages()

		ra.rawLog(models.KindSystemEvent, fmt.Sprintf("→ POST /api/chat  model=%s  agent=%s", ra.Model, ra.AgentName))
		start := time.Now()
		result, err := ra.inferrer.Chat(ctx, ollama.ChatRequest{
			Model:     ra.Model,
			Messages:  msgs,
			KeepAlive: ra.KeepAlive,
			Think:     boolPtr(true),
		})
		elapsed := time.Since(start)
		if err != nil {
			ra.rawLog(models.KindSystemEvent, fmt.Sprintf("✗ ollama error after %.2fs: %v", elapsed.Seconds(), err))
			return "", fmt.Errorf("agent %s: inference: %w", ra.AgentName, err)
		}
		ra.rawLog(models.KindSystemEvent, fmt.Sprintf("← /api/chat done  %.2fs  agent=%s", elapsed.Seconds(), ra.AgentName))

		// Extract native thinking content (Ollama ≥0.7) and synthesise it as
		// <think> blocks so the same extraction logic handles all cases uniformly.
		rawResponse := result.Content
		if result.Thinking != "" {
			rawResponse = "<think>" + result.Thinking + "</think>" + result.Content
		}

		// Split thinking from visible response; log thinking to raw activity.
		thinkContent, response := extractThinkBlocks(rawResponse)
		if thinkContent != "" {
			ra.rawLog(models.KindSystemEvent, "💭 thinking:\n"+thinkContent)
		}

		// Check for escalation signal first.
		if sig := parseEscalation(response); sig != nil {
			return "", &EscalationNeededError{
				AgentID:            ra.AgentID,
				AgentName:          ra.AgentName,
				Reason:             sig.Reason,
				CapabilityRequired: sig.CapabilityRequired,
			}
		}

		// Check for a tool call.
		toolCall := parseToolCall(response)
		if toolCall == nil {
			// No tool call — this is the final response.
			ra.history = append(ra.history, ollama.ChatMessage{
				Role:    "assistant",
				Content: response,
			})
			return stripToolCallLines(response), nil
		}

		// Assign a stable ID for tracing.
		toolCall.ID = fmt.Sprintf("%s-loop%d", ra.AgentID, loop)

		ra.rawLog(models.KindToolCall, fmt.Sprintf("→ tool: %s  args=%s", toolCall.ToolName, fmtArgs(toolCall.Args)))

		// Execute the tool.
		toolResult, execErr := ra.mcpEng.Execute(ctx, ra.Clearance, *toolCall)

		if execErr != nil {
			ra.rawLog(models.KindToolResult, fmt.Sprintf("← tool %s error: %v", toolCall.ToolName, execErr))
		} else {
			ra.rawLog(models.KindToolResult, fmt.Sprintf("← tool %s: %s", toolCall.ToolName, truncate(toolResult.Output, 300)))
		}

		// Append the assistant's tool-call message.
		ra.history = append(ra.history, ollama.ChatMessage{
			Role:    "assistant",
			Content: response,
		})

		// Append the tool result as a user message (Ollama doesn't have a
		// native tool role in all models, so we present results inline).
		var resultContent string
		if execErr != nil {
			resultContent = fmt.Sprintf("Tool %q failed: %v", toolCall.ToolName, execErr)
		} else {
			resultContent = fmt.Sprintf("Tool %q result:\n%s", toolCall.ToolName, toolResult.Output)
		}
		ra.history = append(ra.history, ollama.ChatMessage{
			Role:    "user",
			Content: resultContent,
		})
	}

	return "", fmt.Errorf("agent %s: exceeded maximum tool loop iterations (%d)", ra.AgentName, maxToolLoops)
}

// TurnStream is identical to Turn but calls onChunk for every streamed token
// so callers can show a live typing animation or pipe output to a frontend.
// Tool-loop iterations are also streamed. onChunk may be nil.
func (ra *RunningAgent) TurnStream(ctx context.Context, userContent string, onChunk func(string)) (string, error) {
	ra.history = append(ra.history, ollama.ChatMessage{
		Role:    "user",
		Content: userContent,
	})

	for loop := 0; loop < maxToolLoops; loop++ {
		msgs := ra.buildMessages()

		ra.rawLog(models.KindSystemEvent, fmt.Sprintf("→ POST /api/chat  model=%s  agent=%s", ra.Model, ra.AgentName))
		start := time.Now()
		ch, err := ra.inferrer.ChatStream(ctx, ollama.ChatRequest{
			Model:     ra.Model,
			Messages:  msgs,
			KeepAlive: ra.KeepAlive,
			Think:     boolPtr(true),
		})
		if err != nil {
			elapsed := time.Since(start)
			ra.rawLog(models.KindSystemEvent, fmt.Sprintf("✗ ollama error after %.2fs: %v", elapsed.Seconds(), err))
			return "", fmt.Errorf("agent %s: inference: %w", ra.AgentName, err)
		}

		// Track thinking and content separately during streaming.
		// Thinking tokens are synthesised into <think>...</think> so the frontend
		// parseThink() renders them as a collapsible "thinking…" block, and
		// extractThinkBlocks() can strip them cleanly before storage.
		var sb strings.Builder
		thinkingStarted := false
		thinkingEnded := false
		for chunk := range ch {
			if chunk.Thinking != "" {
				if !thinkingStarted {
					sb.WriteString("<think>")
					if onChunk != nil {
						onChunk("<think>")
					}
					thinkingStarted = true
				}
				sb.WriteString(chunk.Thinking)
				if onChunk != nil {
					onChunk(chunk.Thinking)
				}
			}
			if chunk.Content != "" {
				if thinkingStarted && !thinkingEnded {
					sb.WriteString("</think>")
					if onChunk != nil {
						onChunk("</think>")
					}
					thinkingEnded = true
				}
				sb.WriteString(chunk.Content)
				if onChunk != nil {
					onChunk(chunk.Content)
				}
			}
			if chunk.Done {
				break
			}
		}
		// Close any unclosed think block (rare — model stopped mid-think).
		if thinkingStarted && !thinkingEnded {
			sb.WriteString("</think>")
			if onChunk != nil {
				onChunk("</think>")
			}
		}
		elapsed := time.Since(start)
		rawResponse := sb.String()
		if rawResponse == "" {
			ra.rawLog(models.KindSystemEvent, fmt.Sprintf("✗ empty response after %.2fs  agent=%s", elapsed.Seconds(), ra.AgentName))
			return "", fmt.Errorf("agent %s: empty response from model", ra.AgentName)
		}
		ra.rawLog(models.KindSystemEvent, fmt.Sprintf("← /api/chat done  %.2fs  agent=%s", elapsed.Seconds(), ra.AgentName))

		// Split thinking from visible response; log thinking to raw activity.
		thinkContent, response := extractThinkBlocks(rawResponse)
		if thinkContent != "" {
			ra.rawLog(models.KindSystemEvent, "💭 thinking:\n"+thinkContent)
		}

		if sig := parseEscalation(response); sig != nil {
			return "", &EscalationNeededError{
				AgentID:            ra.AgentID,
				AgentName:          ra.AgentName,
				Reason:             sig.Reason,
				CapabilityRequired: sig.CapabilityRequired,
			}
		}

		toolCall := parseToolCall(response)
		if toolCall == nil {
			ra.history = append(ra.history, ollama.ChatMessage{
				Role:    "assistant",
				Content: response,
			})
			return stripToolCallLines(response), nil
		}

		toolCall.ID = fmt.Sprintf("%s-loop%d", ra.AgentID, loop)

		ra.rawLog(models.KindToolCall, fmt.Sprintf("→ tool: %s  args=%s", toolCall.ToolName, fmtArgs(toolCall.Args)))

		toolResult, execErr := ra.mcpEng.Execute(ctx, ra.Clearance, *toolCall)

		if execErr != nil {
			ra.rawLog(models.KindToolResult, fmt.Sprintf("← tool %s error: %v", toolCall.ToolName, execErr))
		} else {
			ra.rawLog(models.KindToolResult, fmt.Sprintf("← tool %s: %s", toolCall.ToolName, truncate(toolResult.Output, 300)))
		}

		ra.history = append(ra.history, ollama.ChatMessage{
			Role:    "assistant",
			Content: response,
		})

		var resultContent string
		if execErr != nil {
			resultContent = fmt.Sprintf("Tool %q failed: %v", toolCall.ToolName, execErr)
		} else {
			resultContent = fmt.Sprintf("Tool %q result:\n%s", toolCall.ToolName, toolResult.Output)
		}
		ra.history = append(ra.history, ollama.ChatMessage{
			Role:    "user",
			Content: resultContent,
		})
	}

	return "", fmt.Errorf("agent %s: exceeded maximum tool loop iterations (%d)", ra.AgentName, maxToolLoops)
}

// ResetContext clears the conversation history and installs a new system prompt.
// Called on Culture Update — the agent must receive the updated values on its
// very next inference call.
func (ra *RunningAgent) ResetContext(newSysPrompt string) {
	ra.history = nil
	ra.sysPrompt = newSysPrompt
}

// History returns a copy of the current conversation history.
func (ra *RunningAgent) History() []ollama.ChatMessage {
	out := make([]ollama.ChatMessage, len(ra.history))
	copy(out, ra.history)
	return out
}

// SystemPrompt returns the active system prompt.
func (ra *RunningAgent) SystemPrompt() string { return ra.sysPrompt }

// buildMessages assembles the full message list sent to Ollama:
// system prompt (if set) + conversation history.
func (ra *RunningAgent) buildMessages() []ollama.ChatMessage {
	var msgs []ollama.ChatMessage
	if ra.sysPrompt != "" {
		msgs = append(msgs, ollama.ChatMessage{
			Role:    "system",
			Content: ra.sysPrompt,
		})
	}
	msgs = append(msgs, ra.history...)
	return msgs
}

// fmtArgs renders tool call arguments as compact JSON for the dev console.
// Falls back to fmt.Sprint if JSON encoding fails.
func fmtArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return fmt.Sprint(args)
	}
	s := string(b)
	if len(s) > 200 {
		s = s[:197] + "..."
	}
	return s
}

// EscalationNeededError is returned by Turn() when the agent signals it
// cannot handle the current task within its capability ceiling.
type EscalationNeededError struct {
	AgentID            string
	AgentName          string
	Reason             string
	CapabilityRequired string
}

func (e *EscalationNeededError) Error() string {
	return fmt.Sprintf("agent %s (%s): escalation needed — %s [requires: %s]",
		e.AgentName, e.AgentID, e.Reason, e.CapabilityRequired)
}

// dmTurnPrompt wraps a DM message with a structured pre-flight that encourages
// the agent to reason before responding — the DM equivalent of decomposePrompt.
// It explicitly prompts the agent to recognise identity instructions and act on
// them with update_self before composing its reply.
func dmTurnPrompt(message string) string {
	return fmt.Sprintf(`You have a direct message from the Boss. Before composing your reply, reason through:

1. What is the Boss communicating? (introduction / question / instruction / task)
2. Does this affect your identity?
   - New name → update persona.md via update_self
   - Personality / communication style → update persona.md via update_self
   - Values or principles → update soul.md via update_self
   - Skills → update skills.md via update_self
   If yes, call update_self FIRST, then reply.
3. Does this require any other tool calls?
4. What tone is appropriate for your reply?

Message from Boss:
---
%s
---

Respond now. If you updated a brain file first, briefly confirm it (e.g. "Done — I've saved Alfred as my name."), then continue naturally.`, strings.TrimSpace(message))
}
// a Boss command into a list of sub-tasks.
func decomposePrompt(bossCommand string) string {
	return fmt.Sprintf(`You are the Lead agent in a team channel. A message has arrived:

---
%s
---

Decide how to respond:

**If this is a task or request that requires work** (e.g. "build X", "write Y", "research Z", "find out…"):
1. Decompose it into sub-tasks as a JSON array on ONE line, then briefly explain your plan.
   Format: [{"id":"t1","title":"short title","description":"detail","assignee":"lead|specialist"},...]
   Rules: assignee is "lead" (planning/verification) or "specialist" (execution); 1–2 sentence descriptions; dependencies first.

**If this is conversational** (greetings, general discussion, questions, "hi team", etc.):
1. Respond naturally and warmly as the team lead — no JSON, no task list.
2. Acknowledge any team members mentioned and set a positive tone.

Respond appropriately now.`, strings.TrimSpace(bossCommand))
}
