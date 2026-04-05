package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

var filesystemSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation", "path"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "read | write | delete | list"
		},
		"path": {
			"type": "string",
			"description": "File or directory path (relative to project workspace or absolute within it)"
		},
		"content": {
			"type": "string",
			"description": "File content — required for the write operation"
		}
	}
}`)

func filesystemTool(box *mcp.Sandbox, onFileWrite func(string)) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "filesystem",
		Clearance: models.ClearanceSpecialist,
		Description: "Read, write, delete, or list files in the project workspace. " +
			"All paths are sandboxed to the active project directory. " +
			"Delete always creates a .kotui_bak backup first.",
		Schema:  filesystemSchema,
		Handler: filesystemHandler(box, onFileWrite),
	}
}

func filesystemHandler(box *mcp.Sandbox, onFileWrite func(string)) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		op, _ := args["operation"].(string)
		path, _ := args["path"].(string)

		resolved, err := box.Resolve(path)
		if err != nil {
			return "", err
		}

		switch op {
		case "read":
			return fsRead(resolved, path)
		case "write":
			content, _ := args["content"].(string)
			return fsWrite(resolved, path, content, onFileWrite)
		case "delete":
			return fsDelete(box, resolved, path)
		case "list":
			return fsList(resolved, path)
		default:
			return "", fmt.Errorf("filesystem: unknown operation %q (must be read, write, delete, or list)", op)
		}
	}
}

func fsRead(resolved, displayPath string) (string, error) {
	data, err := os.ReadFile(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &mcp.MCPError{
				IsRecoverable: true,
				Suggestion:    "The file does not exist. Use operation=list to discover available files, then retry with the correct path.",
				Underlying:    fmt.Errorf("filesystem: read %s: %w", displayPath, err),
			}
		}
		if os.IsPermission(err) {
			return "", &mcp.MCPError{
				IsRecoverable: false,
				Suggestion:    "Permission denied reading this file — cannot proceed.",
				Underlying:    fmt.Errorf("filesystem: read %s: %w", displayPath, err),
			}
		}
		return "", fmt.Errorf("filesystem: read %s: %w", displayPath, err)
	}
	return string(data), nil
}

func fsWrite(resolved, displayPath, content string, onFileWrite func(string)) (string, error) {
	if err := os.MkdirAll(filepath.Dir(resolved), 0o755); err != nil {
		if os.IsPermission(err) {
			return "", &mcp.MCPError{
				IsRecoverable: false,
				Suggestion:    "Permission denied creating directories — cannot write to this location.",
				Underlying:    fmt.Errorf("filesystem: mkdir for %s: %w", displayPath, err),
			}
		}
		return "", fmt.Errorf("filesystem: mkdir for %s: %w", displayPath, err)
	}
	if err := os.WriteFile(resolved, []byte(content), 0o644); err != nil {
		if os.IsPermission(err) {
			return "", &mcp.MCPError{
				IsRecoverable: false,
				Suggestion:    "Permission denied writing this file — try a different path inside the workspace.",
				Underlying:    fmt.Errorf("filesystem: write %s: %w", displayPath, err),
			}
		}
		return "", fmt.Errorf("filesystem: write %s: %w", displayPath, err)
	}
	if onFileWrite != nil {
		onFileWrite(displayPath)
	}
	return fmt.Sprintf("written %d bytes to %s", len(content), displayPath), nil
}

func fsDelete(box *mcp.Sandbox, resolved, displayPath string) (string, error) {
	// Backup-before-delete is an Immutable Law.
	backupPath := box.BackupPath(resolved)
	if data, err := os.ReadFile(resolved); err == nil {
		_ = os.WriteFile(backupPath, data, 0o644)
	}
	if err := os.Remove(resolved); err != nil {
		if os.IsNotExist(err) {
			return "", &mcp.MCPError{
				IsRecoverable: true,
				Suggestion:    "The file does not exist. Use operation=list to verify the path before deleting.",
				Underlying:    fmt.Errorf("filesystem: delete %s: %w", displayPath, err),
			}
		}
		if os.IsPermission(err) {
			return "", &mcp.MCPError{
				IsRecoverable: false,
				Suggestion:    "Permission denied deleting this file.",
				Underlying:    fmt.Errorf("filesystem: delete %s: %w", displayPath, err),
			}
		}
		return "", fmt.Errorf("filesystem: delete %s: %w", displayPath, err)
	}
	return fmt.Sprintf("deleted %s (backup saved to %s)", displayPath, backupPath), nil
}

func fsList(resolved, displayPath string) (string, error) {
	entries, err := os.ReadDir(resolved)
	if err != nil {
		if os.IsNotExist(err) {
			return "", &mcp.MCPError{
				IsRecoverable: true,
				Suggestion:    "The directory does not exist. Try listing the workspace root (use path=.) to find available directories.",
				Underlying:    fmt.Errorf("filesystem: list %s: %w", displayPath, err),
			}
		}
		if os.IsPermission(err) {
			return "", &mcp.MCPError{
				IsRecoverable: false,
				Suggestion:    "Permission denied listing this directory.",
				Underlying:    fmt.Errorf("filesystem: list %s: %w", displayPath, err),
			}
		}
		return "", fmt.Errorf("filesystem: list %s: %w", displayPath, err)
	}
	var lines []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	sort.Strings(lines)
	if len(lines) == 0 {
		return "(empty directory)", nil
	}
	return strings.Join(lines, "\n"), nil
}
