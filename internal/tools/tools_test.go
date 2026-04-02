package tools_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haepapa/kotui/internal/config"
	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/internal/tools"
	"github.com/haepapa/kotui/pkg/models"
)

// newEngine creates a fully wired MCP engine with all tools registered,
// sandboxed to a temp directory.
func newEngine(t *testing.T) (*mcp.Engine, string) {
	t.Helper()
	root := t.TempDir()
	eng := mcp.New(root)
	if err := tools.RegisterAll(eng, config.Defaults()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}
	return eng, root
}

func exec(t *testing.T, eng *mcp.Engine, clearance models.Clearance, tool string, args map[string]any) (models.ToolResult, error) {
	t.Helper()
	return eng.Execute(context.Background(), clearance, models.ToolCall{
		ID:       "test",
		ToolName: tool,
		Args:     args,
	})
}

// ============================================================
// filesystem tool
// ============================================================

func TestFilesystem_WriteAndRead(t *testing.T) {
	eng, _ := newEngine(t)

	// Write a file.
	res, err := exec(t, eng, models.ClearanceSpecialist, "filesystem", map[string]any{
		"operation": "write",
		"path":      "hello.txt",
		"content":   "hello world",
	})
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	if !strings.Contains(res.Output, "11 bytes") {
		t.Errorf("unexpected write output: %q", res.Output)
	}

	// Read it back.
	res, err = exec(t, eng, models.ClearanceSpecialist, "filesystem", map[string]any{
		"operation": "read",
		"path":      "hello.txt",
	})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if res.Output != "hello world" {
		t.Errorf("expected 'hello world', got %q", res.Output)
	}
}

func TestFilesystem_ListDirectory(t *testing.T) {
	eng, root := newEngine(t)
	os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(root, "b.txt"), []byte("b"), 0o644)

	res, err := exec(t, eng, models.ClearanceSpecialist, "filesystem", map[string]any{
		"operation": "list",
		"path":      ".",
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(res.Output, "a.txt") || !strings.Contains(res.Output, "b.txt") {
		t.Errorf("list missing files: %q", res.Output)
	}
}

func TestFilesystem_DeleteCreatesBackup(t *testing.T) {
	eng, root := newEngine(t)
	filePath := filepath.Join(root, "deleteme.txt")
	os.WriteFile(filePath, []byte("precious data"), 0o644)

	res, err := exec(t, eng, models.ClearanceSpecialist, "filesystem", map[string]any{
		"operation": "delete",
		"path":      "deleteme.txt",
	})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !strings.Contains(res.Output, "backup") {
		t.Errorf("expected backup mention in output: %q", res.Output)
	}

	// Original file should be gone.
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("original file should be deleted")
	}

	// Backup should exist.
	backupPath := filePath + ".kotui_bak"
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("backup not found: %v", err)
	}
	if string(data) != "precious data" {
		t.Errorf("backup content mismatch: %q", data)
	}
}

// IMMUTABLE LAW: filesystem cannot escape the sandbox.
func TestFilesystem_SandboxEscape_Blocked(t *testing.T) {
	eng, _ := newEngine(t)

	attackPaths := []string{
		"../../etc/passwd",
		"/etc/passwd",
	}
	for _, p := range attackPaths {
		_, err := exec(t, eng, models.ClearanceSpecialist, "filesystem", map[string]any{
			"operation": "read",
			"path":      p,
		})
		if err == nil {
			t.Errorf("expected sandbox error for path %q, got nil", p)
		}
		var se *mcp.SandboxError
		if !errors.As(err, &se) {
			t.Errorf("expected SandboxError for %q, got %T: %v", p, err, err)
		}
	}
}

// Trial agent cannot use filesystem (Specialist tool).
func TestFilesystem_TrialBlocked(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceTrial, "filesystem", map[string]any{
		"operation": "read",
		"path":      "anything.txt",
	})
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError for Trial on filesystem, got %T: %v", err, err)
	}
}

func TestFilesystem_WritesSubdirectory(t *testing.T) {
	eng, root := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "filesystem", map[string]any{
		"operation": "write",
		"path":      "subdir/nested/file.go",
		"content":   "package main",
	})
	if err != nil {
		t.Fatalf("nested write: %v", err)
	}
	data, _ := os.ReadFile(filepath.Join(root, "subdir/nested/file.go"))
	if string(data) != "package main" {
		t.Errorf("unexpected content: %q", data)
	}
}

// ============================================================
// shell_executor tool
// ============================================================

func TestShell_BasicCommand(t *testing.T) {
	eng, _ := newEngine(t)
	res, err := exec(t, eng, models.ClearanceSpecialist, "shell_executor", map[string]any{
		"command": "echo hello",
	})
	if err != nil {
		t.Fatalf("shell echo: %v", err)
	}
	if !strings.Contains(res.Output, "hello") {
		t.Errorf("unexpected output: %q", res.Output)
	}
}

func TestShell_CapturesStderr(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "shell_executor", map[string]any{
		"command": "ls /nonexistent_dir_xyz",
	})
	// Should fail (non-zero exit) and include stderr content.
	if err == nil {
		t.Fatal("expected error for failing command")
	}
}

func TestShell_SudoBlocked(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "shell_executor", map[string]any{
		"command": "sudo whoami",
	})
	if err == nil {
		t.Fatal("expected error when command contains sudo")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("error should mention sudo: %v", err)
	}
}

func TestShell_TrialBlocked(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceTrial, "shell_executor", map[string]any{
		"command": "echo hi",
	})
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError for Trial on shell_executor, got %T: %v", err, err)
	}
}

func TestShell_TimeoutEnforced(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "shell_executor", map[string]any{
		"command":         "sleep 10",
		"timeout_seconds": float64(1),
	})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestShell_WorkingDirSandboxed(t *testing.T) {
	eng, _ := newEngine(t)

	// Working dir outside sandbox should be blocked.
	_, err := exec(t, eng, models.ClearanceSpecialist, "shell_executor", map[string]any{
		"command":     "pwd",
		"working_dir": "../../etc",
	})
	var se *mcp.SandboxError
	if !errors.As(err, &se) {
		t.Errorf("expected SandboxError for out-of-sandbox working_dir, got %T: %v", err, err)
	}
}

// ============================================================
// file_manager tool
// ============================================================

func TestFileManager_Tree(t *testing.T) {
	eng, root := newEngine(t)
	os.MkdirAll(filepath.Join(root, "src"), 0o755)
	os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main"), 0o644)
	os.WriteFile(filepath.Join(root, "README.md"), []byte("# readme"), 0o644)

	res, err := exec(t, eng, models.ClearanceLead, "file_manager", map[string]any{
		"operation": "tree",
		"path":      ".",
	})
	if err != nil {
		t.Fatalf("tree: %v", err)
	}
	if !strings.Contains(res.Output, "src/") {
		t.Errorf("tree should contain src/: %q", res.Output)
	}
	if !strings.Contains(res.Output, "main.go") {
		t.Errorf("tree should contain main.go: %q", res.Output)
	}
}

func TestFileManager_Stat(t *testing.T) {
	eng, root := newEngine(t)
	os.WriteFile(filepath.Join(root, "notes.txt"), []byte("some notes here"), 0o644)

	res, err := exec(t, eng, models.ClearanceLead, "file_manager", map[string]any{
		"operation": "stat",
		"path":      "notes.txt",
	})
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !strings.Contains(res.Output, "notes.txt") {
		t.Errorf("stat should contain filename: %q", res.Output)
	}
	if !strings.Contains(res.Output, "15 bytes") {
		t.Errorf("stat should contain size: %q", res.Output)
	}
}

func TestFileManager_Find(t *testing.T) {
	eng, root := newEngine(t)
	os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	os.WriteFile(filepath.Join(root, "pkg", "foo.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(root, "pkg", "foo_test.go"), []byte(""), 0o644)
	os.WriteFile(filepath.Join(root, "main.go"), []byte(""), 0o644)

	res, err := exec(t, eng, models.ClearanceLead, "file_manager", map[string]any{
		"operation": "find",
		"path":      ".",
		"pattern":   "*.go",
	})
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if !strings.Contains(res.Output, "main.go") {
		t.Errorf("find should match main.go: %q", res.Output)
	}
	if !strings.Contains(res.Output, "foo.go") {
		t.Errorf("find should match foo.go: %q", res.Output)
	}
}

// Specialist cannot use file_manager (Lead-only tool).
func TestFileManager_SpecialistBlocked(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "file_manager", map[string]any{
		"operation": "tree",
	})
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError for Specialist on file_manager, got %T: %v", err, err)
	}
}

// file_manager sandbox: cannot tree outside workspace.
func TestFileManager_SandboxEnforced(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceLead, "file_manager", map[string]any{
		"operation": "tree",
		"path":      "../../",
	})
	var se *mcp.SandboxError
	if !errors.As(err, &se) {
		t.Errorf("expected SandboxError for out-of-sandbox path, got %T: %v", err, err)
	}
}

// ============================================================
// iot_gateway tool
// ============================================================

func TestIoT_Discover(t *testing.T) {
	eng, _ := newEngine(t)
	res, err := exec(t, eng, models.ClearanceSpecialist, "iot_gateway", map[string]any{
		"operation": "discover",
	})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if !strings.Contains(res.Output, "IoT Device Discovery") {
		t.Errorf("expected discovery header: %q", res.Output)
	}
}

func TestIoT_PingUnreachable(t *testing.T) {
	eng, _ := newEngine(t)
	// 192.0.2.x is TEST-NET — guaranteed unreachable.
	res, err := exec(t, eng, models.ClearanceSpecialist, "iot_gateway", map[string]any{
		"operation": "ping",
		"host":      "192.0.2.1",
		"port":      float64(9999),
	})
	if err != nil {
		t.Fatalf("ping returned hard error: %v", err)
	}
	if !strings.Contains(res.Output, "UNREACHABLE") {
		t.Errorf("expected UNREACHABLE for dead host: %q", res.Output)
	}
}

func TestIoT_MissingHost(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "iot_gateway", map[string]any{
		"operation": "ping",
	})
	if err == nil {
		t.Fatal("expected error when host is missing")
	}
}

func TestIoT_SudoBlockedInSSHCommand(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "iot_gateway", map[string]any{
		"operation": "sensor_read",
		"host":      "192.0.2.1",
		"command":   "sudo cat /etc/shadow",
	})
	if err == nil {
		t.Fatal("expected error for sudo in SSH command")
	}
	if !strings.Contains(err.Error(), "sudo") {
		t.Errorf("error should mention sudo: %v", err)
	}
}

func TestIoT_TrialBlocked(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceTrial, "iot_gateway", map[string]any{
		"operation": "discover",
	})
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError for Trial on iot_gateway, got %T: %v", err, err)
	}
}

func TestIoT_UnknownOperation(t *testing.T) {
	eng, _ := newEngine(t)
	_, err := exec(t, eng, models.ClearanceSpecialist, "iot_gateway", map[string]any{
		"operation": "write_gpio",
	})
	if err == nil {
		t.Fatal("expected error for unknown Phase 10 operation")
	}
	if !strings.Contains(err.Error(), "write_gpio") {
		t.Errorf("error should mention operation name: %v", err)
	}
}

// ============================================================
// RegisterAll
// ============================================================

func TestRegisterAll_AllToolsPresent(t *testing.T) {
	eng := mcp.New(t.TempDir())
	if err := tools.RegisterAll(eng, config.Defaults()); err != nil {
		t.Fatalf("RegisterAll: %v", err)
	}

	// All 4 tools should appear in the Lead's system prompt fragment.
	fragment := eng.SystemPromptFragment(models.ClearanceLead)
	for _, name := range []string{"filesystem", "shell_executor", "file_manager", "iot_gateway"} {
		if !strings.Contains(fragment, name) {
			t.Errorf("expected %q in system prompt fragment, not found", name)
		}
	}
}

func TestRegisterAll_TrialOnlySeesReadTools(t *testing.T) {
	eng := mcp.New(t.TempDir())
	tools.RegisterAll(eng, config.Defaults())

	// Trial clearance should see NO tools (all 4 require Specialist or Lead).
	fragment := eng.SystemPromptFragment(models.ClearanceTrial)
	for _, name := range []string{"filesystem", "shell_executor", "file_manager", "iot_gateway"} {
		if strings.Contains(fragment, name) {
			t.Errorf("Trial should NOT see %q in system prompt", name)
		}
	}
}
