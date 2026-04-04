package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/haepapa/kotui/internal/agent"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

var selfUpdateSchema = json.RawMessage(`{
	"type": "object",
	"required": ["agent_id", "file", "content", "summary"],
	"properties": {
		"agent_id": {
			"type": "string",
			"description": "Your own agent ID exactly as it appears in your system prompt."
		},
		"file": {
			"type": "string",
			"description": "Which brain file to update. Must be one of: soul, persona, skills."
		},
		"content": {
			"type": "string",
			"description": "Complete new Markdown content for the brain file. This fully replaces the existing file."
		},
		"summary": {
			"type": "string",
			"description": "Brief one-sentence description of what you changed and why. Shown to the Boss."
		}
	}
}`)

// SelfUpdateTool returns an MCP ToolDef that lets a Lead-clearance agent
// update its own brain files (soul.md, persona.md, or skills.md).
// dataDir is the application DataDir. onUpdate, if non-nil, is called after a
// successful write so the caller can recompose instruction.md and notify the frontend.
func SelfUpdateTool(dataDir string, onUpdate func(agentID, file, summary string)) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "update_self",
		Clearance: models.ClearanceLead,
		Description: "Update one of your own persistent brain files (soul.md, persona.md, or skills.md). " +
			"Changes persist across sessions. Use your internal agent_id (shown at the top of your system prompt) — NOT your display name.\n\n" +
			"Example — renaming yourself:\n" +
			"{\"tool\": \"update_self\", \"args\": {\"agent_id\": \"lead\", \"file\": \"persona\", \"content\": \"# Persona\\n\\n## Name\\nAlfred\\n\", \"summary\": \"Renamed to Alfred per Boss request\"}}\n\n" +
			"Important: the entire call must be on ONE line.",
		Schema:  selfUpdateSchema,
		Handler: selfUpdateHandler(dataDir, onUpdate),
	}
}

func selfUpdateHandler(dataDir string, onUpdate func(agentID, file, summary string)) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		agentID, _ := args["agent_id"].(string)
		file, _ := args["file"].(string)
		content, _ := args["content"].(string)
		summary, _ := args["summary"].(string)

		if agentID == "" {
			return "", fmt.Errorf("agent_id is required")
		}
		if content == "" {
			return "", fmt.Errorf("content is required")
		}

		paths := agent.AgentPaths(dataDir, agentID)

		var targetPath string
		switch file {
		case "soul":
			targetPath = paths.SoulPath
		case "persona":
			targetPath = paths.PersonaPath
		case "skills":
			targetPath = paths.SkillsPath
		default:
			return "", fmt.Errorf("unknown file %q: must be soul, persona, or skills", file)
		}

		if err := os.WriteFile(targetPath, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write %s.md: %w", file, err)
		}

		if onUpdate != nil {
			onUpdate(agentID, file, summary)
		}

		return fmt.Sprintf("Updated %s.md — %s", file, summary), nil
	}
}
