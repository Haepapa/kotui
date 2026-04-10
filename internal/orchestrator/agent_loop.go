package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
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

	// pendingHistoryEntry, if set, overrides what gets stored in history for the
	// NEXT TurnStream call. Use InjectHistoryEntry to set it. This lets callers
	// store the raw user message in history while sending a richer augmented
	// version (e.g. decomposePrompt / dmTurnPrompt) to the model for inference.
	pendingHistoryEntry *string

	// OnRaw, if non-nil, receives raw activity events (API calls, tool calls,
	// tool results, errors). Callers set this before calling Turn/TurnStream to
	// route log entries to the appropriate Dispatcher channel.
	OnRaw func(kind models.MessageKind, content string)

	// LastThinking holds the thinking content from the most recent Turn/TurnStream
	// call. Set after extractThinkBlocks; empty when the model produced no thinking.
	LastThinking string
}

// InjectHistoryEntry sets the content that will be stored in the history user
// message for the NEXT TurnStream call. The injected content (raw user message)
// replaces what would otherwise be stored — the augmented/wrapped inference
// content. This prevents prompt-engineering wrappers (decomposePrompt,
// dmTurnPrompt) from polluting the history and confusing follow-up turns.
func (ra *RunningAgent) InjectHistoryEntry(raw string) {
	ra.pendingHistoryEntry = &raw
}

// rawLog calls OnRaw if set. Safe to call when OnRaw is nil.
func (ra *RunningAgent) rawLog(kind models.MessageKind, content string) {
	if ra.OnRaw != nil {
		ra.OnRaw(kind, content)
	}
}

// boolPtr returns a pointer to the given bool — used for optional JSON fields.
func boolPtr(b bool) *bool { return &b }

// LowConfidenceError is returned by Turn/TurnStream when the agent emits a
// confidence signal with CS < 0.7. The caller should surface a consultation
// message to the Boss rather than treating this as a hard error.
type LowConfidenceError struct {
	AgentID string
	Score   float64
	Reason  string
}

func (e *LowConfidenceError) Error() string {
	return fmt.Sprintf("agent %s: low confidence (%.0f%%): %s", e.AgentID, e.Score*100, e.Reason)
}

// InferenceTimeoutError is returned when the model stream is cut by the
// per-request timeout (context.DeadlineExceeded from ChatStream).
type InferenceTimeoutError struct {
	AgentID  string
	Elapsed  float64 // seconds
	TimeoutS float64 // configured timeout in seconds
}

func (e *InferenceTimeoutError) Error() string {
	return fmt.Sprintf("agent %s: inference timed out after %.0fs (limit %.0fs)", e.AgentID, e.Elapsed, e.TimeoutS)
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

// TurnOnce sends a single inference call with custom model options and returns
// the raw response text. Unlike Turn, it does not run the tool loop, does not
// append to history, and does not check confidence/escalation signals. It is
// intended for lightweight classification calls where speed matters more than
// agent behaviour.
func (ra *RunningAgent) TurnOnce(ctx context.Context, prompt string, opts *ollama.ModelOptions) (string, error) {
	msgs := []ollama.ChatMessage{{Role: "user", Content: prompt}}
	if ra.sysPrompt != "" {
		msgs = append([]ollama.ChatMessage{{Role: "system", Content: ra.sysPrompt}}, msgs...)
	}
	result, err := ra.inferrer.Chat(ctx, ollama.ChatRequest{
		Model:     ra.Model,
		Messages:  msgs,
		KeepAlive: ra.KeepAlive,
		Think:     boolPtr(false),
		Options:   opts,
	})
	if err != nil {
		return "", err
	}
	_, response := extractThinkBlocks(result.Content)
	return strings.TrimSpace(response), nil
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
			Options: &ollama.ModelOptions{
				ThinkBudget: 2048,
			},
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
		ra.LastThinking = thinkContent
		if thinkContent != "" {
			ra.rawLog(models.KindSystemEvent, "💭 thinking:\n"+thinkContent)
		}

		// Check confidence signal: if CS < 0.7 stop and surface a consultation.
		if cs := parseConfidenceSignal(response); cs != nil && cs.ConfidenceScore < 0.7 {
			ra.rawLog(models.KindSystemEvent, fmt.Sprintf("🔍 low confidence (%.0f%%): %s", cs.ConfidenceScore*100, cs.Reason))
			// Preserve the agent's clarification question in history so that
			// the next boss message has context about what was already asked.
			if visible := stripToolCallLines(response); visible != "" {
				ra.history = append(ra.history, ollama.ChatMessage{Role: "assistant", Content: visible})
			}
			return "", &LowConfidenceError{AgentID: ra.AgentID, Score: cs.ConfidenceScore, Reason: cs.Reason}
		}

		// Check for escalation signal first.
		if sig := parseEscalation(response); sig != nil {
			if visible := stripToolCallLines(response); visible != "" {
				ra.history = append(ra.history, ollama.ChatMessage{Role: "assistant", Content: visible})
			}
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
	// Store the raw content in history (not the augmented inference content).
	historyContent := userContent
	if ra.pendingHistoryEntry != nil {
		historyContent = *ra.pendingHistoryEntry
		ra.pendingHistoryEntry = nil
	}
	ra.history = append(ra.history, ollama.ChatMessage{
		Role:    "user",
		Content: historyContent,
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
			Options: &ollama.ModelOptions{
				ThinkBudget: 2048,
			},
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
		var streamErr error // set if the stream was cut by a context/network error
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
				streamErr = chunk.Err
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

		// If the stream was cut by a deadline or cancellation, surface a typed error.
		if streamErr != nil {
			if errors.Is(streamErr, context.DeadlineExceeded) {
				ra.rawLog(models.KindSystemEvent, fmt.Sprintf("⏱ inference timed out after %.0fs  agent=%s", elapsed.Seconds(), ra.AgentName))
				return "", &InferenceTimeoutError{
					AgentID:  ra.AgentID,
					Elapsed:  elapsed.Seconds(),
					TimeoutS: elapsed.Seconds(), // actual elapsed ≈ configured timeout
				}
			}
			if errors.Is(streamErr, context.Canceled) {
				ra.rawLog(models.KindSystemEvent, fmt.Sprintf("⛔ inference cancelled after %.0fs  agent=%s", elapsed.Seconds(), ra.AgentName))
				return "", context.Canceled
			}
		}

		rawResponse := sb.String()
		if rawResponse == "" {
			ra.rawLog(models.KindSystemEvent, fmt.Sprintf("✗ empty response after %.2fs  agent=%s", elapsed.Seconds(), ra.AgentName))
			return "", fmt.Errorf("agent %s: empty response from model", ra.AgentName)
		}
		ra.rawLog(models.KindSystemEvent, fmt.Sprintf("← /api/chat done  %.2fs  agent=%s", elapsed.Seconds(), ra.AgentName))

		// Split thinking from visible response; log thinking to raw activity.
		thinkContent, response := extractThinkBlocks(rawResponse)
		ra.LastThinking = thinkContent
		if thinkContent != "" {
			ra.rawLog(models.KindSystemEvent, "💭 thinking:\n"+thinkContent)
		}

		// Check confidence signal: if CS < 0.7 stop and surface a consultation.
		if cs := parseConfidenceSignal(response); cs != nil && cs.ConfidenceScore < 0.7 {
			ra.rawLog(models.KindSystemEvent, fmt.Sprintf("🔍 low confidence (%.0f%%): %s", cs.ConfidenceScore*100, cs.Reason))
			// Preserve the agent's clarification question in history so that
			// the next boss message has context about what was already asked.
			if visible := stripToolCallLines(response); visible != "" {
				ra.history = append(ra.history, ollama.ChatMessage{Role: "assistant", Content: visible})
			}
			return "", &LowConfidenceError{AgentID: ra.AgentID, Score: cs.ConfidenceScore, Reason: cs.Reason}
		}

		if sig := parseEscalation(response); sig != nil {
			if visible := stripToolCallLines(response); visible != "" {
				ra.history = append(ra.history, ollama.ChatMessage{Role: "assistant", Content: visible})
			}
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

// LastAssistantMessage returns the most recent assistant-role message in
// history, or an empty string if none exists yet. Used by the orchestrator to
// inject a "your previous response was…" reminder into follow-up prompts so
// small models don't ignore their own prior output.
func (ra *RunningAgent) LastAssistantMessage() string {
	for i := len(ra.history) - 1; i >= 0; i-- {
		if ra.history[i].Role == "assistant" {
			return ra.history[i].Content
		}
	}
	return ""
}

// SeedHistory pre-populates the agent's conversation history from persisted
// messages so it can recall prior exchanges after an app restart. Only call
// this before the first TurnStream — calling it mid-session will corrupt the
// history ordering.
func (ra *RunningAgent) SeedHistory(msgs []ollama.ChatMessage) {
	if len(msgs) == 0 {
		return
	}
	ra.history = append(msgs, ra.history...)
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
	return fmt.Sprintf(`**Think concisely: work through each step once only. Do not second-guess yourself.**

You have a direct message from the Boss. Before composing your reply, work through each step in order:

1. **Understand**: What is the Boss communicating? (introduction / question / instruction / task)

2. **Identity check**: Does this affect your identity?
   - New name → update persona.md via update_self
   - Personality / communication style → update persona.md via update_self
   - Values or principles → update soul.md via update_self
   - Skills → update skills.md via update_self
   If yes, call update_self FIRST, then continue.

3. **Tool call check**: Does this require any tool calls (other than update_self)?
   - If YES: STOP. Before calling any tool, you MUST first assess your confidence.
     Output this JSON on its own line: {"confidence_score": <0.0–1.0>, "reason": "<why>"}
     - Score ≥ 0.7 → proceed with the tool call immediately after.
     - Score < 0.7 → output ONLY the confidence signal. Do NOT call the tool. Explain what information you need to proceed.
   - If NO: continue to step 4.

4. **Ambiguity check**: Is the request clear enough to act on?
   - If the request is ambiguous, vague, or could cause unintended consequences, DO NOT guess.
     Ask the Boss one specific clarifying question instead.

5. **Tone**: What tone is appropriate for your reply?

6. **Journal**: Once you have delivered your final reply, write a journal entry using write_journal.
   Record: what the task was, the outcome, a brief summary, any lessons, and skills discovered.
   Do this for EVERY meaningful interaction — treat it as your daily work diary.

Message from Boss:
---
%s
---

Respond now. Work through each step before writing your reply.`, strings.TrimSpace(message))
}
// a Boss command into a list of sub-tasks.
func decomposePrompt(bossCommand string) string {
	return fmt.Sprintf(`**Think budget: you have limited thinking tokens. Decide quickly. Do NOT repeat the same reasoning point twice. Once you identify the category, act immediately.**

You are the Lead agent in a team channel. A message has arrived:

---
%s
---

**Step 0 — Understand what type of message this is:**

Before doing anything, decide which category this falls into:

**(A) A specific, executable task** — a direct instruction with a clear deliverable.
  Signals: imperative verbs ("write", "build", "fix", "create", "find", "analyse"), short and action-focused, names a concrete output.

**(B) A project brief, introduction, or context-setting message** — the Boss is sharing background, explaining what they plan to work on, or setting the scene for future work. They are NOT asking you to start executing right now.
  Signals: future intent ("you will help me", "I want to", "we will be", "I'm going to"), a greeting combined with a project description, no explicit instruction verb, or the message reads more like "here's what I'm working on" than "go do X".

**(C) Conversational** — a greeting, question, general discussion, or "hi team".

---

**If (B) — project brief or context-setting:**
Respond warmly as a team lead would. Acknowledge what the Boss has shared, express genuine interest, and ask one focused clarifying question that will help when work begins. Do NOT output a task list. Do NOT start any execution. This is a conversation, not a task.

**If (C) — conversational:**
Respond naturally and warmly. Acknowledge any team members mentioned. No JSON, no task list.

**If (A) — specific task:**
1. Assess whether it is clear enough to act on.
   - If ambiguous or risky: ask the Boss one specific clarifying question. Do NOT guess.
   - If clear: briefly acknowledge it in one sentence, then output the task list as a JSON array on ONE line, then briefly explain your plan.
     The JSON array is MANDATORY — plain text task lists will not be processed and no work will be done.
     Format: [{"id":"t1","title":"short title","description":"detail","assignee":"lead|specialist","justification":"one sentence — why this agent/approach for this task"},...]
     Rules: assignee is "lead" (planning/verification) or "specialist" (execution); 1–2 sentence descriptions; justification is mandatory and specific.
     Example:
     Sounds like a clear task — let me get the team on it.
     [{"id":"t1","title":"Write sorting script","description":"Write a Python script that sorts a list of dicts.","assignee":"specialist","justification":"Specialist executes code generation."}]
2. Before making ANY tool call, output a confidence signal on its own line:
   {"confidence_score": <0.0–1.0>, "reason": "<why>"}
   - Score ≥ 0.7 → proceed with the tool call.
   - Score < 0.7 → output ONLY the signal; do NOT proceed. Explain what's needed.
3. **After completing a task or giving a final answer**, write a journal entry using write_journal.
   Record what was done, the outcome, a brief summary, and any lessons learned.
   Treat this as your daily work diary — write an entry for every meaningful piece of work.

Respond now.`, strings.TrimSpace(bossCommand))
}

// briefAckPrompt is used when the message is classified as a project introduction
// or context-setting brief. The model should respond warmly and ask one question.
func briefAckPrompt(command string) string {
	return fmt.Sprintf(`**Think concisely — one pass only.**

You are the Lead agent. The Boss has sent a project introduction or context brief — they are sharing what they plan to work on, NOT asking you to start executing yet.

Respond as a team lead would on the first day of a new project:
- Acknowledge warmly and show genuine interest
- Pick out one interesting or important detail to comment on
- Ask ONE focused clarifying question that will be useful when work begins
- Keep it brief — 2–4 sentences total

Do NOT output a task list. Do NOT start executing. This is the start of a conversation.

Message from Boss:
---
%s
---

Respond now.`, strings.TrimSpace(command))
}

// chatReplyPrompt is used when the message is classified as casual conversation,
// a greeting, or a general question with no executable task.
func chatReplyPrompt(command string) string {
	return fmt.Sprintf(`**Think concisely — one pass only.**

You are the Lead agent. The Boss has sent a conversational message — a greeting, question, or general discussion.

Respond naturally as a colleague would. Be warm, direct, and concise. No task lists, no JSON, no formal structure.

Message from Boss:
---
%s
---

Respond now.`, strings.TrimSpace(command))
}

// followUpPrompt is used when the Boss's message is a direct action request
// on something the Lead just produced — "save that", "put it in a file",
// "output it to X", "run that", "rename it". The model should execute immediately
// rather than re-planning.
func followUpPrompt(command, lastReply string) string {
	trimmed := lastReply
	if len(trimmed) > 2000 {
		trimmed = trimmed[:2000] + "\n…[truncated — full output is in your conversation history]"
	}
	return fmt.Sprintf(`**Think concisely — one pass only. Act immediately.**

You are the Lead agent. The Boss is asking you to act on something you just produced. This is NOT a new task — it is a direct follow-up to your previous response.

Your previous response was:
---
%s
---

The Boss now says:
---
%s
---

**What to do:**
Execute immediately. If this requires a tool call (e.g. writing a file, reading a file, running code):
1. Output a confidence signal on its own line: {"confidence_score": <0.0–1.0>, "reason": "<why>"}
2. If score ≥ 0.7 — call the tool immediately. Do NOT re-plan, do NOT output a new task list.
3. If score < 0.7 — ask ONE clarifying question only.

If no tool is needed, respond directly and concisely.

Respond now.`, strings.TrimSpace(trimmed), strings.TrimSpace(command))
}

// classifyPrompt returns a short, fast classification prompt. It is designed to
// be run with Think:false and a very small NumPredict so the model returns a
// single word as quickly as possible.
//
// Valid outputs: TASK | BRIEF | CHAT | FOLLOWUP
func classifyPrompt(command, lastReply string) string {
	contextHint := ""
	if lastReply != "" {
		preview := lastReply
		if len(preview) > 300 {
			preview = preview[:300] + "…"
		}
		contextHint = fmt.Sprintf("\n\nContext — the agent's previous response was:\n%s", preview)
	}
	return fmt.Sprintf(`Classify the following message in ONE WORD. Reply with exactly one of:

TASK     — a direct instruction to create, write, build, fix, or do something new
BRIEF    — context-setting or project introduction; the sender is explaining what they'll be working on, not asking for immediate action
CHAT     — greeting, casual question, or general conversation with no concrete action requested
FOLLOWUP — a short directive that builds directly on the agent's immediately preceding response (e.g. "save that", "output it to a file", "put that in scripts/", "run that", "rename it")%s

Message: %s

Classification (ONE WORD only):`, contextHint, strings.TrimSpace(command))
}
