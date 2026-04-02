package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

const (
	shellDefaultTimeout = 30 * time.Second
	shellMaxTimeout     = 300 * time.Second
)

var shellSchema = json.RawMessage(`{
	"type": "object",
	"required": ["command"],
	"properties": {
		"command": {
			"type": "string",
			"description": "Shell command to execute (passed to sh -c). Must not contain sudo."
		},
		"working_dir": {
			"type": "string",
			"description": "Working directory (relative to project workspace). Defaults to workspace root."
		},
		"timeout_seconds": {
			"type": "number",
			"description": "Max execution time in seconds. Default 30, max 300."
		}
	}
}`)

func shellExecutorTool(box *mcp.Sandbox) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "shell_executor",
		Clearance: models.ClearanceSpecialist,
		Description: "Run a shell command inside the project workspace. " +
			"Stdout and stderr are captured and returned. " +
			"sudo is never permitted. Default timeout is 30 seconds (max 300).",
		Schema:  shellSchema,
		Handler: shellHandler(box),
	}
}

func shellHandler(box *mcp.Sandbox) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		command, _ := args["command"].(string)
		if command == "" {
			return "", fmt.Errorf("shell_executor: command must not be empty")
		}

		// Hard block on sudo — check both args map and command string.
		if err := mcp.ValidateSudo(args); err != nil {
			return "", err
		}
		if containsSudo(command) {
			return "", fmt.Errorf("shell_executor: sudo is not permitted in command")
		}

		// Resolve working directory.
		workDir := box.Root()
		if wd, _ := args["working_dir"].(string); wd != "" {
			resolved, err := box.Resolve(wd)
			if err != nil {
				return "", err
			}
			workDir = resolved
		}

		// Parse timeout.
		timeout := shellDefaultTimeout
		if ts, ok := args["timeout_seconds"]; ok {
			secs := toFloat64(ts)
			if secs > 0 {
				d := time.Duration(secs) * time.Second
				if d > shellMaxTimeout {
					d = shellMaxTimeout
				}
				timeout = d
			}
		}

		// Execute with deadline.
		cmdCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		cmd := exec.CommandContext(cmdCtx, "sh", "-c", command)
		cmd.Dir = workDir

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		runErr := cmd.Run()

		var sb strings.Builder
		if stdout.Len() > 0 {
			sb.WriteString("STDOUT:\n")
			sb.WriteString(stdout.String())
		}
		if stderr.Len() > 0 {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("STDERR:\n")
			sb.WriteString(stderr.String())
		}
		output := sb.String()

		if runErr != nil {
			exitMsg := fmt.Sprintf("command failed: %v", runErr)
			if output != "" {
				return "", fmt.Errorf("%s\n%s", exitMsg, output)
			}
			return "", fmt.Errorf("%s", exitMsg)
		}

		if output == "" {
			return "(no output)", nil
		}
		return output, nil
	}
}

// containsSudo checks whether any token in the command string is "sudo".
func containsSudo(command string) bool {
	for _, token := range strings.Fields(command) {
		if token == "sudo" {
			return true
		}
	}
	return false
}

// toFloat64 converts numeric interface{} values to float64.
func toFloat64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}
