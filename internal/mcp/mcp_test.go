package mcp_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haepapa/kotui/internal/mcp"
	"github.com/haepapa/kotui/pkg/models"
)

// --- helpers ---------------------------------------------------------------

func successHandler(output string) mcp.Handler {
	return func(_ context.Context, _ map[string]any) (string, error) {
		return output, nil
	}
}

func failHandler(msg string) mcp.Handler {
	return func(_ context.Context, _ map[string]any) (string, error) {
		return "", errors.New(msg)
	}
}

func countingHandler(calls *int, after int, output string) mcp.Handler {
	return func(_ context.Context, _ map[string]any) (string, error) {
		*calls++
		if *calls > after {
			return output, nil
		}
		return "", fmt.Errorf("transient failure %d", *calls)
	}
}

func simpleTool(name string, clearance models.Clearance, h mcp.Handler) mcp.ToolDef {
	return mcp.ToolDef{
		Name:        name,
		Description: "Test tool " + name,
		Schema:      []byte(`{"type":"object","properties":{},"required":[]}`),
		Clearance:   clearance,
		Handler:     h,
	}
}

func toolWithSchema(name string, clearance models.Clearance, schema string, h mcp.Handler) mcp.ToolDef {
	return mcp.ToolDef{
		Name:      name,
		Schema:    []byte(schema),
		Clearance: clearance,
		Handler:   h,
	}
}

// newCall constructs a ToolCall with the given name and empty args.
func newCall(toolName string) models.ToolCall {
	return models.ToolCall{ID: "test-call", ToolName: toolName, Args: map[string]any{}}
}

// --- Permission Gate -------------------------------------------------------

// IMMUTABLE LAW 1: Trial agents cannot call Specialist tools.
func TestPermissionGate_TrialBlockedFromSpecialist(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("specialist_tool", models.ClearanceSpecialist, successHandler("ok")))

	_, err := eng.Execute(context.Background(), models.ClearanceTrial, newCall("specialist_tool"))
	if err == nil {
		t.Fatal("expected permission error, got nil")
	}
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected *mcp.PermissionError, got %T: %v", err, err)
	}
	if pe.AgentClearance != models.ClearanceTrial {
		t.Errorf("wrong agent clearance in error: %v", pe.AgentClearance)
	}
}

// IMMUTABLE LAW 1b: Trial agents cannot call Lead tools.
func TestPermissionGate_TrialBlockedFromLead(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("lead_tool", models.ClearanceLead, successHandler("ok")))

	_, err := eng.Execute(context.Background(), models.ClearanceTrial, newCall("lead_tool"))
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError, got %T: %v", err, err)
	}
}

// IMMUTABLE LAW 2: Specialist agents cannot call Lead tools.
func TestPermissionGate_SpecialistBlockedFromLead(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("lead_only", models.ClearanceLead, successHandler("ok")))

	_, err := eng.Execute(context.Background(), models.ClearanceSpecialist, newCall("lead_only"))
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError, got %T: %v", err, err)
	}
}

// Specialist agents CAN call Specialist tools.
func TestPermissionGate_SpecialistAllowedForSpecialist(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("spec_tool", models.ClearanceSpecialist, successHandler("done")))

	result, err := eng.Execute(context.Background(), models.ClearanceSpecialist, newCall("spec_tool"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "done" {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

// Specialist agents CAN call Trial tools.
func TestPermissionGate_SpecialistAllowedForTrial(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("trial_tool", models.ClearanceTrial, successHandler("read")))

	_, err := eng.Execute(context.Background(), models.ClearanceSpecialist, newCall("trial_tool"))
	if err != nil {
		t.Fatalf("Specialist should be able to invoke a Trial tool: %v", err)
	}
}

// Lead agents CAN call all tools.
func TestPermissionGate_LeadAllowedForAll(t *testing.T) {
	eng := mcp.New("")
	for name, c := range map[string]models.Clearance{
		"tool_trial":      models.ClearanceTrial,
		"tool_specialist": models.ClearanceSpecialist,
		"tool_lead":       models.ClearanceLead,
	} {
		eng.Register(simpleTool(name, c, successHandler("ok")))
	}

	for _, name := range []string{"tool_trial", "tool_specialist", "tool_lead"} {
		_, err := eng.Execute(context.Background(), models.ClearanceLead, newCall(name))
		if err != nil {
			t.Errorf("Lead should be able to invoke %s: %v", name, err)
		}
	}
}

// Trial agents CAN call Trial tools.
func TestPermissionGate_TrialAllowedForTrial(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("reader", models.ClearanceTrial, successHandler("contents")))

	result, err := eng.Execute(context.Background(), models.ClearanceTrial, newCall("reader"))
	if err != nil {
		t.Fatalf("Trial should invoke Trial tool: %v", err)
	}
	if result.Output != "contents" {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

// --- Shell executor blocked for Trial (concrete hiring workflow scenario) --

// IMMUTABLE LAW — concrete hiring scenario:
// shell_executor is a Specialist-clearance tool.
// A Trial candidate must be blocked from calling it.
func TestPermissionGate_TrialBlockedFromShellExecutor(t *testing.T) {
	eng := mcp.New("")
	eng.Register(mcp.ToolDef{
		Name:      "shell_executor",
		Clearance: models.ClearanceSpecialist,
		Handler:   successHandler("$ echo hello"),
		Schema:    []byte(`{"type":"object","required":["command"],"properties":{"command":{"type":"string"}}}`),
	})

	call := models.ToolCall{ID: "c1", ToolName: "shell_executor", Args: map[string]any{"command": "rm -rf /"}}
	_, err := eng.Execute(context.Background(), models.ClearanceTrial, call)
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError for shell_executor from Trial, got: %v", err)
	}
}

// write_file also blocked for Trial.
func TestPermissionGate_TrialBlockedFromWriteFile(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("write_file", models.ClearanceSpecialist, successHandler("written")))

	_, err := eng.Execute(context.Background(), models.ClearanceTrial, newCall("write_file"))
	var pe *mcp.PermissionError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PermissionError for write_file from Trial: %v", err)
	}
}

// --- Retry & Escalation ----------------------------------------------------

// IMMUTABLE LAW 4: 3 consecutive failures → EscalationError.
func TestEscalation_AfterThreeFailures(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("flaky", models.ClearanceLead, failHandler("always fails")))

	result, err := eng.Execute(context.Background(), models.ClearanceLead, newCall("flaky"))
	if err == nil {
		t.Fatal("expected EscalationError, got nil")
	}
	var esc *mcp.EscalationError
	if !errors.As(err, &esc) {
		t.Fatalf("expected *mcp.EscalationError, got %T: %v", err, err)
	}
	if esc.Attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", esc.Attempts)
	}
	if result.IsError != true {
		t.Error("result.IsError should be true on escalation")
	}
}

// Retry: succeed on the 2nd attempt.
func TestRetry_SucceedOnSecondAttempt(t *testing.T) {
	eng := mcp.New("")
	calls := 0
	eng.Register(simpleTool("sometimes_flaky", models.ClearanceLead, countingHandler(&calls, 1, "ok")))

	result, err := eng.Execute(context.Background(), models.ClearanceLead, newCall("sometimes_flaky"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", result.Attempts)
	}
	if result.Output != "ok" {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

// Context cancellation stops execution immediately.
func TestExecute_ContextCancellation(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("slow", models.ClearanceLead, failHandler("nope")))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel

	_, err := eng.Execute(ctx, models.ClearanceLead, newCall("slow"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

// --- Schema validation -----------------------------------------------------

func TestSchemaValidation_RequiredFieldMissing(t *testing.T) {
	eng := mcp.New("")
	eng.Register(toolWithSchema("greeter", models.ClearanceLead,
		`{"type":"object","required":["name"],"properties":{"name":{"type":"string"}}}`,
		successHandler("hi"),
	))

	call := models.ToolCall{ID: "c1", ToolName: "greeter", Args: map[string]any{}}
	_, err := eng.Execute(context.Background(), models.ClearanceLead, call)
	if err == nil {
		t.Fatal("expected validation error for missing required field")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("error should mention the missing field 'name': %v", err)
	}
}

func TestSchemaValidation_WrongType(t *testing.T) {
	eng := mcp.New("")
	eng.Register(toolWithSchema("counter", models.ClearanceLead,
		`{"type":"object","required":["count"],"properties":{"count":{"type":"integer"}}}`,
		successHandler("done"),
	))

	call := models.ToolCall{ID: "c1", ToolName: "counter", Args: map[string]any{"count": "not-a-number"}}
	_, err := eng.Execute(context.Background(), models.ClearanceLead, call)
	if err == nil {
		t.Fatal("expected type validation error")
	}
}

func TestSchemaValidation_PassesWithCorrectArgs(t *testing.T) {
	eng := mcp.New("")
	eng.Register(toolWithSchema("writer", models.ClearanceSpecialist,
		`{"type":"object","required":["path","content"],"properties":{"path":{"type":"string"},"content":{"type":"string"}}}`,
		successHandler("written"),
	))

	call := models.ToolCall{
		ID:       "c1",
		ToolName: "writer",
		Args:     map[string]any{"path": "/tmp/x.txt", "content": "hello"},
	}
	result, err := eng.Execute(context.Background(), models.ClearanceSpecialist, call)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Output != "written" {
		t.Errorf("unexpected output: %q", result.Output)
	}
}

// --- Sandbox ---------------------------------------------------------------

// IMMUTABLE LAW 3: Path traversal must be blocked.
func TestSandbox_BlocksPathTraversal(t *testing.T) {
	root := t.TempDir()
	s := mcp.NewSandboxForTest(root)

	attackPaths := []string{
		"../../etc/passwd",
		root + "/../../etc/passwd",
		"/etc/passwd",
		root + "/../secret",
	}
	for _, p := range attackPaths {
		_, err := s.Resolve(p)
		if err == nil {
			t.Errorf("expected SandboxError for path %q, got nil", p)
		}
		var se *mcp.SandboxError
		if !errors.As(err, &se) {
			t.Errorf("expected *mcp.SandboxError for %q, got %T: %v", p, err, err)
		}
	}
}

func TestSandbox_AllowsValidPaths(t *testing.T) {
	root := t.TempDir()
	s := mcp.NewSandboxForTest(root)

	validPaths := []string{
		"file.go",
		"subdir/file.go",
		"a/b/c/d.txt",
		filepath.Join(root, "file.go"),
	}
	for _, p := range validPaths {
		got, err := s.Resolve(p)
		if err != nil {
			t.Errorf("unexpected error for valid path %q: %v", p, err)
		}
		if !strings.HasPrefix(got, root) {
			t.Errorf("resolved path %q is not inside root %q", got, root)
		}
	}
}

func TestSandbox_Disabled(t *testing.T) {
	s := mcp.NewSandboxForTest("") // disabled
	if !s.Disabled() {
		t.Error("expected sandbox to be disabled with empty root")
	}
	// Any path resolves cleanly.
	got, err := s.Resolve("some/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "some/path" {
		t.Errorf("unexpected resolved path: %q", got)
	}
}

// --- Registry --------------------------------------------------------------

func TestRegistry_DuplicateToolReturnsError(t *testing.T) {
	eng := mcp.New("")
	if err := eng.Register(simpleTool("dupe", models.ClearanceLead, successHandler("1"))); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := eng.Register(simpleTool("dupe", models.ClearanceLead, successHandler("2"))); err == nil {
		t.Fatal("expected error on duplicate registration")
	}
}

func TestRegistry_UnknownToolReturnsError(t *testing.T) {
	eng := mcp.New("")
	_, err := eng.Execute(context.Background(), models.ClearanceLead, newCall("ghost"))
	if err == nil {
		t.Fatal("expected error for unknown tool")
	}
}

func TestSystemPromptFragment_ContainsToolInfo(t *testing.T) {
	eng := mcp.New("")
	eng.Register(mcp.ToolDef{
		Name:        "read_file",
		Description: "Read a file from the workspace",
		Clearance:   models.ClearanceTrial,
		Schema:      []byte(`{"type":"object","required":["path"],"properties":{"path":{"type":"string"}}}`),
		Handler:     successHandler("contents"),
	})

	fragment := eng.SystemPromptFragment(models.ClearanceTrial)
	if !strings.Contains(fragment, "read_file") {
		t.Error("fragment should contain tool name 'read_file'")
	}
	if !strings.Contains(fragment, "Read a file") {
		t.Error("fragment should contain tool description")
	}
}

func TestSystemPromptFragment_HidesHighClearanceToolsFromTrial(t *testing.T) {
	eng := mcp.New("")
	eng.Register(simpleTool("read_only", models.ClearanceTrial, successHandler("ok")))
	eng.Register(simpleTool("shell_exec", models.ClearanceSpecialist, successHandler("ok")))

	fragment := eng.SystemPromptFragment(models.ClearanceTrial)
	if strings.Contains(fragment, "shell_exec") {
		t.Error("Trial agent should not see shell_exec in system prompt")
	}
	if !strings.Contains(fragment, "read_only") {
		t.Error("Trial agent should see read_only in system prompt")
	}
}

// --- Sudo guard ------------------------------------------------------------

func TestValidateSudo_BlocksBoolTrue(t *testing.T) {
	args := map[string]any{"sudo": true, "command": "echo hi"}
	if err := mcp.ValidateSudo(args); err == nil {
		t.Fatal("expected error for sudo:true")
	}
}

func TestValidateSudo_AllowsNoSudo(t *testing.T) {
	args := map[string]any{"command": "echo hi"}
	if err := mcp.ValidateSudo(args); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
