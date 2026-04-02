package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/internal/orchestrator"
	"github.com/haepapa/kotui/pkg/models"
)

// --- Mock Inferrer --------------------------------------------------------

// mockInferrer is a controllable Inferrer for unit tests.
type mockInferrer struct {
	response string
	err      error
	calls    atomic.Int64
}

func (m *mockInferrer) Chat(_ context.Context, req ollama.ChatRequest) (*ollama.ChatResult, error) {
	m.calls.Add(1)
	if m.err != nil {
		return nil, m.err
	}
	return &ollama.ChatResult{Content: m.response}, nil
}

func (m *mockInferrer) ChatStream(_ context.Context, req ollama.ChatRequest) (<-chan ollama.StreamChunk, error) {
	ch := make(chan ollama.StreamChunk, 1)
	ch <- ollama.StreamChunk{Content: m.response, Done: true}
	close(ch)
	return ch, nil
}

func (m *mockInferrer) IsHealthy(_ context.Context) bool { return true }

// sequenceMock returns a different response for each successive call.
type sequenceMock struct {
	responses []string
	idx       atomic.Int64
}

func (s *sequenceMock) Chat(_ context.Context, req ollama.ChatRequest) (*ollama.ChatResult, error) {
	idx := int(s.idx.Add(1)) - 1
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	return &ollama.ChatResult{Content: s.responses[idx]}, nil
}

func (s *sequenceMock) ChatStream(_ context.Context, _ ollama.ChatRequest) (<-chan ollama.StreamChunk, error) {
	idx := int(s.idx.Add(1)) - 1
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	ch := make(chan ollama.StreamChunk, 1)
	ch <- ollama.StreamChunk{Content: s.responses[idx], Done: true}
	close(ch)
	return ch, nil
}

func (s *sequenceMock) IsHealthy(_ context.Context) bool { return true }

// --- Parse helpers ---------------------------------------------------------

func TestParseToolCall_Valid(t *testing.T) {
	text := `I need to read a file.
{"tool": "filesystem", "args": {"operation": "read", "path": "main.go"}}
After reading, I will proceed.`

	call := orchestrator.ExportedParseToolCall(text)
	if call == nil {
		t.Fatal("expected a tool call, got nil")
	}
	if call.ToolName != "filesystem" {
		t.Errorf("expected filesystem, got %q", call.ToolName)
	}
	if call.Args["operation"] != "read" {
		t.Errorf("expected operation=read, got %q", call.Args["operation"])
	}
}

func TestParseToolCall_NonePresent(t *testing.T) {
	text := "Here is my response. No tools needed."
	if call := orchestrator.ExportedParseToolCall(text); call != nil {
		t.Errorf("expected nil, got %+v", call)
	}
}

func TestParseToolCall_InvalidJSON_Skipped(t *testing.T) {
	text := `{ this is not valid json }
{"tool": "shell_executor", "args": {"command": "echo hi"}}`
	call := orchestrator.ExportedParseToolCall(text)
	if call == nil {
		t.Fatal("expected tool call from second line")
	}
	if call.ToolName != "shell_executor" {
		t.Errorf("expected shell_executor, got %q", call.ToolName)
	}
}

func TestParseToolCall_NoToolKey_Skipped(t *testing.T) {
	text := `{"foo": "bar", "baz": 1}`
	if call := orchestrator.ExportedParseToolCall(text); call != nil {
		t.Errorf("expected nil for JSON without 'tool' key, got %+v", call)
	}
}

// --- Escalation signal parsing --------------------------------------------

func TestParseEscalation_Detected(t *testing.T) {
	text := `I cannot handle this task.
{"escalation_needed": true, "reason": "requires deep maths", "capability_required": "theorem-prover"}
Please route this.`
	sig := orchestrator.ExportedParseEscalation(text)
	if sig == nil {
		t.Fatal("expected escalation signal, got nil")
	}
	if !strings.Contains(sig.Reason, "maths") {
		t.Errorf("unexpected reason: %q", sig.Reason)
	}
	if sig.CapabilityRequired != "theorem-prover" {
		t.Errorf("unexpected capability_required: %q", sig.CapabilityRequired)
	}
}

func TestParseEscalation_NotPresent(t *testing.T) {
	text := `{"tool": "filesystem", "args": {}}`
	if sig := orchestrator.ExportedParseEscalation(text); sig != nil {
		t.Errorf("expected nil for tool call, got %+v", sig)
	}
}

func TestParseEscalation_FalseFlag(t *testing.T) {
	text := `{"escalation_needed": false, "reason": "all good"}`
	if sig := orchestrator.ExportedParseEscalation(text); sig != nil {
		t.Errorf("expected nil when escalation_needed=false, got %+v", sig)
	}
}

// --- Task list parsing ----------------------------------------------------

func TestParseTaskList_Valid(t *testing.T) {
	text := `Here is my plan:
[{"id":"t1","title":"Write code","description":"Create main.go","assignee":"specialist"},{"id":"t2","title":"Run tests","description":"Execute go test","assignee":"specialist"}]
I will proceed in order.`
	tasks := orchestrator.ExportedParseTaskList(text)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ID != "t1" {
		t.Errorf("expected t1, got %q", tasks[0].ID)
	}
	if tasks[1].Assignee != "specialist" {
		t.Errorf("expected specialist, got %q", tasks[1].Assignee)
	}
}

func TestParseTaskList_None(t *testing.T) {
	text := "I'll just answer directly."
	if tasks := orchestrator.ExportedParseTaskList(text); len(tasks) != 0 {
		t.Errorf("expected nil, got %+v", tasks)
	}
}

// --- RunningAgent (agentic loop) ------------------------------------------

func TestRunningAgent_SimpleResponse(t *testing.T) {
	inf := &mockInferrer{response: "The answer is 42."}
	ra := orchestrator.NewRunningAgentForTest("ag1", "Test", "model", models.ClearanceLead, inf, nil)

	result, err := ra.Turn(context.Background(), "What is the answer?")
	if err != nil {
		t.Fatalf("Turn: %v", err)
	}
	if result != "The answer is 42." {
		t.Errorf("unexpected result: %q", result)
	}
}

func TestRunningAgent_EscalationSignalReturnsError(t *testing.T) {
	response := `{"escalation_needed": true, "reason": "too hard", "capability_required": "expert"}`
	inf := &mockInferrer{response: response}
	ra := orchestrator.NewRunningAgentForTest("ag1", "Test", "model", models.ClearanceLead, inf, nil)

	_, err := ra.Turn(context.Background(), "Solve this complex problem")
	if err == nil {
		t.Fatal("expected EscalationNeededError, got nil")
	}
	var escErr *orchestrator.EscalationNeededError
	if !errors.As(err, &escErr) {
		t.Fatalf("expected *EscalationNeededError, got %T: %v", err, err)
	}
	if escErr.Reason != "too hard" {
		t.Errorf("unexpected reason: %q", escErr.Reason)
	}
}

func TestRunningAgent_InferenceErrorPropagates(t *testing.T) {
	inf := &mockInferrer{err: errors.New("connection refused")}
	ra := orchestrator.NewRunningAgentForTest("ag1", "Test", "model", models.ClearanceLead, inf, nil)

	_, err := ra.Turn(context.Background(), "any")
	if err == nil {
		t.Fatal("expected error from failing inferrer")
	}
}

func TestRunningAgent_HistoryAccumulates(t *testing.T) {
	inf := &mockInferrer{response: "reply"}
	ra := orchestrator.NewRunningAgentForTest("ag1", "Test", "model", models.ClearanceLead, inf, nil)

	ra.Turn(context.Background(), "message 1")
	ra.Turn(context.Background(), "message 2")

	history := ra.History()
	// user1, assistant1, user2, assistant2
	if len(history) != 4 {
		t.Errorf("expected 4 history entries, got %d", len(history))
	}
}

func TestRunningAgent_ResetContextClearsHistory(t *testing.T) {
	inf := &mockInferrer{response: "ok"}
	ra := orchestrator.NewRunningAgentForTest("ag1", "Test", "model", models.ClearanceLead, inf, nil)

	ra.Turn(context.Background(), "before reset")
	ra.ResetContext("new system prompt")

	if len(ra.History()) != 0 {
		t.Errorf("history should be empty after ResetContext, got %d entries", len(ra.History()))
	}
	if ra.SystemPrompt() != "new system prompt" {
		t.Errorf("unexpected system prompt: %q", ra.SystemPrompt())
	}
}

// --- VRAM Coordinator -----------------------------------------------------

func TestVRAMCoordinator_DualModeDoesNotPark(t *testing.T) {
	inf := &mockInferrer{response: "ok"}
	vc := orchestrator.NewVRAMCoordinatorForTest(models.VRAMDual, inf, "test-model")

	if err := vc.AcquireWorkerSlot(context.Background()); err != nil {
		t.Fatalf("AcquireWorkerSlot: %v", err)
	}
	defer vc.ReleaseWorkerSlot(context.Background())

	if vc.IsParked() {
		t.Error("dual mode should not park the Lead")
	}
	if inf.calls.Load() > 0 {
		t.Errorf("dual mode should make 0 park calls, made %d", inf.calls.Load())
	}
}

func TestVRAMCoordinator_SwapModeParksLead(t *testing.T) {
	inf := &mockInferrer{response: "ok"}
	vc := orchestrator.NewVRAMCoordinatorForTest(models.VRAMSwap, inf, "test-model")

	if err := vc.AcquireWorkerSlot(context.Background()); err != nil {
		t.Fatalf("AcquireWorkerSlot: %v", err)
	}
	defer vc.ReleaseWorkerSlot(context.Background())

	if !vc.IsParked() {
		t.Error("swap mode should park the Lead")
	}
}

func TestVRAMCoordinator_ReleaseRestoresSlot(t *testing.T) {
	inf := &mockInferrer{response: "ok"}
	vc := orchestrator.NewVRAMCoordinatorForTest(models.VRAMDual, inf, "test-model")

	vc.AcquireWorkerSlot(context.Background())
	vc.ReleaseWorkerSlot(context.Background())

	// Should be acquirable again immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 100e6) // 100ms
	defer cancel()
	if err := vc.AcquireWorkerSlot(ctx); err != nil {
		t.Errorf("slot should be available after release: %v", err)
	}
	vc.ReleaseWorkerSlot(context.Background())
}

// --- Hiring workflow -------------------------------------------------------

func TestHiringState_Transitions(t *testing.T) {
	if orchestrator.HiringProposed != 0 {
		t.Error("HiringProposed should be 0")
	}
	if orchestrator.HiringInterview != 1 {
		t.Error("HiringInterview should be 1")
	}
	if orchestrator.HiringApproved != 2 {
		t.Error("HiringApproved should be 2")
	}
	if orchestrator.HiringRejected != 3 {
		t.Error("HiringRejected should be 3")
	}
}
// --- Strip tool call lines ------------------------------------------------

func TestStripToolCallLines(t *testing.T) {
	text := `Here is my answer.
{"tool": "filesystem", "args": {}}
This is the prose part.`
	stripped := orchestrator.ExportedStripToolCallLines(text)
	if strings.Contains(stripped, `"tool"`) {
		t.Errorf("stripped text should not contain tool call JSON: %q", stripped)
	}
	if !strings.Contains(stripped, "prose part") {
		t.Errorf("stripped text should preserve prose: %q", stripped)
	}
}
