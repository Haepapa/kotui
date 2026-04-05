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

func shellExecutorTool(box *mcp.Sandbox, gate *SudoGate) mcp.ToolDef {
	description := "Run a shell command inside the project workspace. " +
		"Stdout and stderr are captured and returned. " +
		"sudo requires Boss approval — the command will pause until authorised. " +
		"Default timeout is 30 seconds (max 300)."
	if gate == nil {
		description = "Run a shell command inside the project workspace. " +
			"Stdout and stderr are captured and returned. " +
			"sudo is never permitted. Default timeout is 30 seconds (max 300)."
	}
	return mcp.ToolDef{
		Name:        "shell_executor",
		Clearance:   models.ClearanceSpecialist,
		Description: description,
		Schema:      shellSchema,
		Handler:     shellHandler(box, gate),
	}
}

func shellHandler(box *mcp.Sandbox, gate *SudoGate) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		command, _ := args["command"].(string)
		if command == "" {
			return "", fmt.Errorf("shell_executor: command must not be empty")
		}

		// sudo handling: gate-approval when available, hard-block otherwise.
		if err := mcp.ValidateSudo(args); err != nil {
			return "", err
		}
		if containsSudo(command) {
			if gate == nil {
				return "", fmt.Errorf("shell_executor: sudo is not permitted")
			}
			// Generate a stable ID for this approval request using the command hash.
			approvalID := sudoApprovalID(command)
			approved, gateErr := gate.Request(ctx, approvalID, command)
			if gateErr != nil {
				return "", &mcp.MCPError{
					IsRecoverable: false,
					Suggestion:    gateErr.Error(),
					Underlying:    fmt.Errorf("shell_executor: sudo gate: %w", gateErr),
				}
			}
			if !approved {
				return "", &mcp.MCPError{
					IsRecoverable: false,
					Suggestion:    "The Boss rejected this sudo command. Propose an alternative approach that does not require elevated privileges.",
					Underlying:    fmt.Errorf("shell_executor: sudo command rejected by Boss"),
				}
			}
			// Approved — strip 'sudo' prefix and run as normal (sandbox already restricts scope).
			command = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(command), "sudo"))
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
			combinedOutput := output
			if output != "" {
				exitMsg = fmt.Sprintf("%s\n%s", exitMsg, output)
			}

			// Classify common recoverable vs non-recoverable shell errors.
			lowerOut := strings.ToLower(combinedOutput + runErr.Error())
			switch {
			case strings.Contains(lowerOut, "command not found") ||
				strings.Contains(lowerOut, "no such file or directory") && strings.Contains(lowerOut, "exec"):
				return "", &mcp.MCPError{
					IsRecoverable: true,
					Suggestion:    "The command was not found. Check whether the tool is installed or use an alternative command.",
					Underlying:    fmt.Errorf("%s", exitMsg),
				}
			case strings.Contains(lowerOut, "permission denied"):
				return "", &mcp.MCPError{
					IsRecoverable: false,
					Suggestion:    "Permission denied running this command — cannot proceed.",
					Underlying:    fmt.Errorf("%s", exitMsg),
				}
			case strings.Contains(lowerOut, "context deadline exceeded") ||
				strings.Contains(lowerOut, "signal: killed"):
				return "", &mcp.MCPError{
					IsRecoverable: true,
					Suggestion:    "The command timed out. Try increasing timeout_seconds or breaking it into smaller steps.",
					Underlying:    fmt.Errorf("%s", exitMsg),
				}
			default:
				return "", fmt.Errorf("%s", exitMsg)
			}
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

// sudoApprovalID generates a deterministic short ID from the command string
// for use as the sudo approval DB record ID.
func sudoApprovalID(cmd string) string {
	// Simple FNV-inspired hash to avoid importing crypto packages.
	h := uint64(14695981039346656037)
	for i := 0; i < len(cmd); i++ {
		h ^= uint64(cmd[i])
		h *= 1099511628211
	}
	return fmt.Sprintf("sudo-%016x", h)
}
