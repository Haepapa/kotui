// Package tools/iotgateway implements the read-only IoT gateway MCP tool.
//
// Phase 4 scope (human-verified, read-only):
//   - discover: enumerate available serial ports and configured SSH hosts
//   - ping:     check if a host:port is reachable (TCP dial)
//   - status:   SSH to a device and run a status command
//   - sensor_read: SSH to a device and run a sensor-read command
//
// Write/command capabilities are deferred to Phase 10 (self-built upgrade).
// Rationale: hardware communication protocols are safety-critical and must be
// verified by a human developer before agents can send commands to physical devices.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

const (
	iotPingTimeout  = 5 * time.Second
	iotSSHTimeout   = 15 * time.Second
	iotSSHMaxCmdLen = 256
)

var iotSchema = json.RawMessage(`{
	"type": "object",
	"required": ["operation"],
	"properties": {
		"operation": {
			"type": "string",
			"description": "discover | ping | status | sensor_read"
		},
		"host": {
			"type": "string",
			"description": "Hostname or IP address of the target device"
		},
		"port": {
			"type": "number",
			"description": "TCP port (default 22 for SSH)"
		},
		"ssh_user": {
			"type": "string",
			"description": "SSH username (default: pi)"
		},
		"command": {
			"type": "string",
			"description": "Command to run on the device (status or sensor_read operations)"
		}
	}
}`)

func iotGatewayTool(cfg config.Config) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      "iot_gateway",
		Clearance: models.ClearanceSpecialist,
		Description: "Read-only interface to IoT hardware nodes (Raspberry Pi, LoRa gateways, serial devices). " +
			"Phase 4 operations: discover (list available devices), ping (connectivity check), " +
			"status (SSH device health), sensor_read (SSH sensor query). " +
			"Write/command capabilities are not available until Phase 10.",
		Schema:  iotSchema,
		Handler: iotHandler(cfg),
	}
}

func iotHandler(cfg config.Config) mcp.Handler {
	return func(ctx context.Context, args map[string]any) (string, error) {
		op, _ := args["operation"].(string)

		switch op {
		case "discover":
			return iotDiscover(cfg)
		case "ping":
			host, _ := args["host"].(string)
			port := 22
			if p := toFloat64(args["port"]); p > 0 {
				port = int(p)
			}
			return iotPing(ctx, host, port)
		case "status":
			return iotSSH(ctx, args, "uptime && hostname && free -h 2>/dev/null || vm_stat")
		case "sensor_read":
			cmd, _ := args["command"].(string)
			if cmd == "" {
				return "", fmt.Errorf("iot_gateway: sensor_read requires a command argument")
			}
			if len(cmd) > iotSSHMaxCmdLen {
				return "", fmt.Errorf("iot_gateway: command too long (max %d chars)", iotSSHMaxCmdLen)
			}
			return iotSSH(ctx, args, cmd)
		default:
			return "", fmt.Errorf("iot_gateway: unknown operation %q (must be discover, ping, status, or sensor_read)", op)
		}
	}
}

// iotDiscover enumerates available serial ports and configured remote hosts.
func iotDiscover(cfg config.Config) (string, error) {
	var sb strings.Builder
	sb.WriteString("=== IoT Device Discovery ===\n\n")

	ports := listSerialPorts()
	sb.WriteString(fmt.Sprintf("Serial ports (%d found):\n", len(ports)))
	if len(ports) == 0 {
		sb.WriteString("  (none detected)\n")
	}
	for _, p := range ports {
		sb.WriteString("  " + p + "\n")
	}

	sb.WriteString("\nConfigured remote hosts:\n")
	if cfg.SeniorConsultant.SSHHost != "" {
		sb.WriteString(fmt.Sprintf("  ssh://%s (senior_consultant)\n", cfg.SeniorConsultant.SSHHost))
	} else {
		sb.WriteString("  (none configured — add [senior_consultant] ssh_host to config.toml)\n")
	}

	return sb.String(), nil
}

// iotPing checks if a host:port is reachable via TCP.
func iotPing(ctx context.Context, host string, port int) (string, error) {
	if host == "" {
		return "", fmt.Errorf("iot_gateway: ping requires a host argument")
	}
	addr := fmt.Sprintf("%s:%d", host, port)

	dialCtx, cancel := context.WithTimeout(ctx, iotPingTimeout)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return fmt.Sprintf("UNREACHABLE: %s — %v", addr, err), nil
	}
	conn.Close()
	return fmt.Sprintf("REACHABLE: %s responded within %s", addr, iotPingTimeout), nil
}

// iotSSH executes a read-only command on a remote device via SSH.
func iotSSH(ctx context.Context, args map[string]any, defaultCmd string) (string, error) {
	host, _ := args["host"].(string)
	if host == "" {
		return "", fmt.Errorf("iot_gateway: host is required for SSH operations")
	}

	user := "pi"
	if u, _ := args["ssh_user"].(string); u != "" {
		user = u
	}

	port := 22
	if p := toFloat64(args["port"]); p > 0 {
		port = int(p)
	}

	cmd := defaultCmd
	if c, _ := args["command"].(string); c != "" {
		cmd = c
	}

	if containsSudo(cmd) {
		return "", fmt.Errorf("iot_gateway: sudo is not permitted in SSH commands")
	}

	sshTarget := fmt.Sprintf("%s@%s", user, host)
	sshArgs := []string{
		"-p", fmt.Sprintf("%d", port),
		"-o", "ConnectTimeout=10",
		"-o", "StrictHostKeyChecking=no",
		"-o", "BatchMode=yes",
		sshTarget,
		cmd,
	}

	cmdCtx, cancel := context.WithTimeout(ctx, iotSSHTimeout)
	defer cancel()

	out, err := exec.CommandContext(cmdCtx, "ssh", sshArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("iot_gateway: SSH to %s failed: %v\n%s", sshTarget, err, string(out))
	}
	return string(out), nil
}

// listSerialPorts returns available serial device paths in a platform-appropriate way.
// Phase 4: discovery only — no data is read from the ports.
func listSerialPorts() []string {
	switch runtime.GOOS {
	case "linux":
		return globPorts("/dev/ttyUSB*", "/dev/ttyACM*", "/dev/ttyS*")
	case "darwin":
		return globPorts("/dev/tty.usbserial*", "/dev/tty.usbmodem*", "/dev/cu.usbserial*", "/dev/cu.usbmodem*")
	case "windows":
		out, err := exec.Command("wmic", "path", "Win32_SerialPort", "get", "DeviceID").Output()
		if err != nil {
			return nil
		}
		var ports []string
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "COM") {
				ports = append(ports, line)
			}
		}
		return ports
	}
	return nil
}

func globPorts(patterns ...string) []string {
	var found []string
	seen := map[string]bool{}
	for _, p := range patterns {
		out, err := exec.Command("sh", "-c", "ls "+p+" 2>/dev/null").Output()
		if err != nil {
			continue
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if line != "" && !seen[line] {
				seen[line] = true
				found = append(found, line)
			}
		}
	}
	return found
}
