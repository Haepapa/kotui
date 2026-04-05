package orchestrator

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/ollama"
)

// reflectMinBossMessages is the minimum number of Boss messages in a DM
// session before a reflection is triggered.
const reflectMinBossMessages = 3

// Reflect runs a background self-reflection on the agent's soul.md and
// persona.md based on what the Boss said during a DM session.
//
// It is always called from a P3 CogQueue job so it never blocks the Boss.
// The method:
//  1. Reads the current soul.md + persona.md from disk.
//  2. Runs a single LLM turn with a structured reflection prompt.
//  3. Parses UPDATE_SOUL / UPDATE_PERSONA blocks from the response.
//  4. If either block contains content, overwrites the respective file.
//
// dataDir is the app-level data directory (same as OrchestratorConfig.DataDir).
// history is the full DM session in plain text, boss + agent turns interleaved.
func (ra *RunningAgent) Reflect(ctx context.Context, dataDir, history string) error {
	paths := agent.AgentPaths(dataDir, ra.AgentID)

	soulBytes, err := os.ReadFile(paths.SoulPath)
	if err != nil {
		return fmt.Errorf("reflect %s: read soul.md: %w", ra.AgentID, err)
	}
	personaBytes, err := os.ReadFile(paths.PersonaPath)
	if err != nil {
		return fmt.Errorf("reflect %s: read persona.md: %w", ra.AgentID, err)
	}

	userMsg := buildReflectionPrompt(string(soulBytes), string(personaBytes), history)

	result, inferErr := ra.inferrer.Chat(ctx, ollama.ChatRequest{
		Model: ra.Model,
		Messages: []ollama.ChatMessage{
			{Role: "system", Content: reflectionSystemPrompt},
			{Role: "user", Content: userMsg},
		},
		Stream:    false,
		KeepAlive: ra.KeepAlive,
	})
	if inferErr != nil {
		return fmt.Errorf("reflect %s: inference: %w", ra.AgentID, inferErr)
	}

	newSoul, newPersona := parseReflectionResponse(result.Content)

	if newSoul != "" && newSoul != strings.TrimSpace(string(soulBytes)) {
		if wErr := os.WriteFile(paths.SoulPath, []byte(newSoul+"\n"), 0o644); wErr != nil {
			return fmt.Errorf("reflect %s: write soul.md: %w", ra.AgentID, wErr)
		}
	}
	if newPersona != "" && newPersona != strings.TrimSpace(string(personaBytes)) {
		if wErr := os.WriteFile(paths.PersonaPath, []byte(newPersona+"\n"), 0o644); wErr != nil {
			return fmt.Errorf("reflect %s: write persona.md: %w", ra.AgentID, wErr)
		}
	}

	return nil
}

// countBossMessages returns the number of "user" role messages in a
// RunningAgent's history. Each DM turn from the Boss adds one user entry.
func (ra *RunningAgent) countBossMessages() int {
	n := 0
	for _, m := range ra.history {
		if m.Role == "user" {
			n++
		}
	}
	return n
}

// buildHistoryText formats the RunningAgent's conversation history as a
// plain-text transcript suitable for the reflection prompt.
func (ra *RunningAgent) buildHistoryText() string {
	var sb strings.Builder
	for _, m := range ra.history {
		label := "Agent"
		if m.Role == "user" {
			label = "Boss"
		}
		sb.WriteString(label + ": " + m.Content + "\n\n")
	}
	return sb.String()
}

// reflectionSystemPrompt is used for the one-shot reflection inference call.
const reflectionSystemPrompt = `You are performing a private self-reflection on your own identity files.
Your task is to analyse a recent conversation with the Boss and decide whether your soul.md or persona.md should be updated to better align with how the Boss interacts with you.

Rules:
- Only propose changes when there is clear, repeated evidence in the conversation (not a single message).
- Changes must be conservative and specific. Do not rewrite entire files.
- If no meaningful adjustment is warranted, output exactly: NO_CHANGES
- Output MUST follow the exact format below with no extra commentary.

Output format (use this exactly):
UPDATE_SOUL:
<full updated soul.md content, or leave blank to keep unchanged>
END_UPDATE_SOUL

UPDATE_PERSONA:
<full updated persona.md content, or leave blank to keep unchanged>
END_UPDATE_PERSONA`

// buildReflectionPrompt constructs the user message for the reflection call.
func buildReflectionPrompt(soul, persona, history string) string {
	return fmt.Sprintf(
		"## Current soul.md\n\n%s\n\n"+
			"## Current persona.md\n\n%s\n\n"+
			"## Recent DM Session Transcript\n\n%s\n\n"+
			"Review the transcript carefully. "+
			"Do any patterns suggest your soul.md or persona.md should evolve? "+
			"Output UPDATE_SOUL / UPDATE_PERSONA blocks or NO_CHANGES.",
		strings.TrimSpace(soul),
		strings.TrimSpace(persona),
		strings.TrimSpace(history),
	)
}

// parseReflectionResponse extracts the soul and persona content from a
// reflection response. Returns empty strings when a section is absent or
// the model output NO_CHANGES.
func parseReflectionResponse(resp string) (soul, persona string) {
	if strings.Contains(resp, "NO_CHANGES") {
		return "", ""
	}
	soul = extractBlock(resp, "UPDATE_SOUL:", "END_UPDATE_SOUL")
	persona = extractBlock(resp, "UPDATE_PERSONA:", "END_UPDATE_PERSONA")
	return
}

// extractBlock pulls the text between startMarker and endMarker.
func extractBlock(text, start, end string) string {
	si := strings.Index(text, start)
	if si < 0 {
		return ""
	}
	after := text[si+len(start):]
	ei := strings.Index(after, end)
	if ei < 0 {
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(after[:ei])
}
