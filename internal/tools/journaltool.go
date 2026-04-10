package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

var writeJournalSchema = json.RawMessage(`{
	"type": "object",
	"required": ["agent_id", "task", "outcome", "summary"],
	"properties": {
		"agent_id": {
			"type": "string",
			"description": "Your own agent ID exactly as it appears in your system prompt (e.g. 'lead')."
		},
		"task": {
			"type": "string",
			"description": "One-line description of what the task or conversation was about."
		},
		"outcome": {
			"type": "string",
			"description": "Result: success | partial | failure"
		},
		"summary": {
			"type": "string",
			"description": "2–4 sentences: what was done, what the result was, and anything notable."
		},
		"lessons": {
			"type": "string",
			"description": "What you would do differently next time, or 'none'."
		},
		"skills_proposed": {
			"type": "string",
			"description": "Comma-separated list of new skills or tools identified during this task, or 'none'."
		}
	}
}`)

// WriteJournalTool returns an MCP ToolDef that lets agents write a journal
// entry recording a completed task. This is how agents maintain their work
// diary — call it at the end of every significant task or conversation.
func WriteJournalTool(dataDir string) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "write_journal",
		Clearance: models.ClearanceLead,
		Description: "Write a journal entry to record a completed task or conversation. " +
			"Treat this like a work diary — write an entry for every significant piece of work: " +
			"what the task was, the outcome (success/partial/failure), a short summary of what happened, " +
			"any lessons learned, and new skills or tools discovered. " +
			"Call this when the Boss says the task is complete, when you finish a multi-step piece of work, " +
			"or at the end of any meaningful conversation.\n\n" +
			"Example:\n" +
			"{\"tool\": \"write_journal\", \"args\": {\"agent_id\": \"lead\", \"task\": \"Create Python sort script\", " +
			"\"outcome\": \"success\", \"summary\": \"Wrote sort_dict.py that sorts by key or value. " +
			"Script tested and saved to scripts/ directory.\", \"lessons\": \"none\", \"skills_proposed\": \"none\"}}\n\n" +
			"The entire call must be on ONE line.",
		Schema:  writeJournalSchema,
		Handler: writeJournalHandler(dataDir),
	}
}

func writeJournalHandler(dataDir string) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		agentID, _ := args["agent_id"].(string)
		task, _ := args["task"].(string)
		outcome, _ := args["outcome"].(string)
		summary, _ := args["summary"].(string)
		lessons, _ := args["lessons"].(string)
		skillsProposed, _ := args["skills_proposed"].(string)

		if agentID == "" {
			return "", fmt.Errorf("write_journal: agent_id is required")
		}
		if task == "" {
			return "", fmt.Errorf("write_journal: task is required")
		}
		if summary == "" {
			return "", fmt.Errorf("write_journal: summary is required")
		}

		outcome = strings.ToLower(strings.TrimSpace(outcome))
		switch outcome {
		case "success", "partial", "failure":
			// valid
		default:
			outcome = "success"
		}

		paths := agent.AgentPaths(dataDir, agentID)
		entry := agent.JournalEntry{
			Task:           task,
			Outcome:        outcome,
			Summary:        summary,
			Lessons:        lessons,
			SkillsProposed: skillsProposed,
		}

		if err := agent.WriteJournal(paths, entry); err != nil {
			return "", fmt.Errorf("write_journal: %w", err)
		}

		return fmt.Sprintf("Journal entry written — task: %q, outcome: %s", task, outcome), nil
	}
}
