package orchestrator

import (
	"context"
	"fmt"
	"strings"

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
}

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

		result, err := ra.inferrer.Chat(ctx, ollama.ChatRequest{
			Model:     ra.Model,
			Messages:  msgs,
			KeepAlive: ra.KeepAlive,
		})
		if err != nil {
			return "", fmt.Errorf("agent %s: inference: %w", ra.AgentName, err)
		}

		response := result.Content

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

		// Execute the tool.
		toolResult, execErr := ra.mcpEng.Execute(ctx, ra.Clearance, *toolCall)

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

		ch, err := ra.inferrer.ChatStream(ctx, ollama.ChatRequest{
			Model:     ra.Model,
			Messages:  msgs,
			KeepAlive: ra.KeepAlive,
		})
		if err != nil {
			return "", fmt.Errorf("agent %s: inference: %w", ra.AgentName, err)
		}

		var sb strings.Builder
		for chunk := range ch {
			if chunk.Done {
				break
			}
			sb.WriteString(chunk.Content)
			if onChunk != nil {
				onChunk(chunk.Content)
			}
		}
		response := sb.String()
		if response == "" {
			return "", fmt.Errorf("agent %s: empty response from model", ra.AgentName)
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
		toolResult, execErr := ra.mcpEng.Execute(ctx, ra.Clearance, *toolCall)

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

// decomposePrompt returns the instruction used to ask the Lead to decompose
// a Boss command into a list of sub-tasks.
func decomposePrompt(bossCommand string) string {
	return fmt.Sprintf(`The Boss has issued the following command:

---
%s
---

Decompose this into an ordered list of sub-tasks. Emit the list as a single JSON array on one line, then briefly explain your plan.

Format:
[{"id":"t1","title":"short title","description":"detail","assignee":"specialist"},...]

Rules:
- assignee must be "lead" (for planning/verification tasks) or "specialist" (for execution tasks)
- keep each description to 1–2 sentences
- order tasks so dependencies come first`, strings.TrimSpace(bossCommand))
}
