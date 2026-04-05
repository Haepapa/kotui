package orchestrator_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

// --- CogQueue -------------------------------------------------------------

func newTestQueue(t *testing.T) *orchestrator.CogQueue {
	t.Helper()
	q := orchestrator.NewCogQueueForTest(nil)
	q.Start(context.Background())
	return q
}

func TestCogQueue_SingleSubmitCompletes(t *testing.T) {
	q := newTestQueue(t)
	ran := false
	_, err := q.Submit(context.Background(), orchestrator.ExportedP1Lead, func(_ context.Context) error {
		ran = true
		return nil
	})
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !ran {
		t.Error("fn was not executed")
	}
}

func TestCogQueue_ErrorPropagates(t *testing.T) {
	q := newTestQueue(t)
	want := errors.New("boom")
	_, err := q.Submit(context.Background(), orchestrator.ExportedP1Lead, func(_ context.Context) error {
		return want
	})
	if !errors.Is(err, want) {
		t.Errorf("expected %v, got %v", want, err)
	}
}

func TestCogQueue_Serialisation(t *testing.T) {
	// Two concurrent submits must not overlap.
	q := newTestQueue(t)
	const n = 5
	var inFlight atomic.Int32
	var maxConcurrent atomic.Int32

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q.Submit(context.Background(), orchestrator.ExportedP1Lead, func(_ context.Context) error {
				cur := inFlight.Add(1)
				if cur > maxConcurrent.Load() {
					maxConcurrent.Store(cur)
				}
				time.Sleep(5 * time.Millisecond)
				inFlight.Add(-1)
				return nil
			})
		}()
	}
	wg.Wait()
	if got := maxConcurrent.Load(); got > 1 {
		t.Errorf("expected max concurrency 1, got %d", got)
	}
}

func TestCogQueue_PriorityOrdering(t *testing.T) {
	// Fill the queue with P3 items, then submit a P1 item.
	// The P1 item must run before the remaining P3 items.
	ctx := context.Background()

	// Use a blocking gate so we can control when items actually run.
	gate := make(chan struct{})
	var order []int
	var mu sync.Mutex

	q := orchestrator.NewCogQueueForTest(nil)
	q.Start(ctx)

	// Submit a P3 blocker first — it holds the gate open.
	var p3Started sync.WaitGroup
	p3Started.Add(1)
	go q.Submit(ctx, orchestrator.ExportedP3Background, func(_ context.Context) error {
		p3Started.Done() // signal that P3 is running
		<-gate           // block until released
		mu.Lock()
		order = append(order, 30)
		mu.Unlock()
		return nil
	})
	p3Started.Wait() // wait until the first P3 is actually running

	// Now queue: P3, P3, P1 — the P1 should run before the P3s.
	var submitted sync.WaitGroup
	submitted.Add(3)
	go func() {
		submitted.Done()
		q.Submit(ctx, orchestrator.ExportedP3Background, func(_ context.Context) error {
			mu.Lock(); order = append(order, 31); mu.Unlock()
			return nil
		})
	}()
	go func() {
		submitted.Done()
		q.Submit(ctx, orchestrator.ExportedP3Background, func(_ context.Context) error {
			mu.Lock(); order = append(order, 32); mu.Unlock()
			return nil
		})
	}()
	go func() {
		submitted.Done()
		q.Submit(ctx, orchestrator.ExportedP1Lead, func(_ context.Context) error {
			mu.Lock(); order = append(order, 1); mu.Unlock()
			return nil
		})
	}()
	submitted.Wait()
	// Give goroutines a moment to enqueue before releasing the gate.
	time.Sleep(20 * time.Millisecond)

	close(gate) // release the blocker

	// Wait for all 4 items to finish (1 blocker + 3 queued).
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(order) < 4 {
		t.Fatalf("expected 4 completed items, got %d: %v", len(order), order)
	}
	// The P1 item (value 1) must appear before both remaining P3 items (31, 32).
	p1Idx := -1
	for i, v := range order {
		if v == 1 {
			p1Idx = i
			break
		}
	}
	if p1Idx < 0 {
		t.Fatal("P1 item never ran")
	}
	for _, v := range []int{31, 32} {
		for i, o := range order {
			if o == v && i < p1Idx {
				t.Errorf("P3 item %d ran before P1 (P1 at index %d, P3 at index %d): order=%v", v, p1Idx, i, order)
			}
		}
	}
}

func TestCogQueue_ContextCancellationBeforeEnqueue(t *testing.T) {
	q := newTestQueue(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := q.Submit(ctx, orchestrator.ExportedP1Lead, func(_ context.Context) error {
		return nil
	})
	if err == nil {
		t.Error("expected context.Canceled, got nil")
	}
}

func TestCogQueue_StateReflectsWaiting(t *testing.T) {
	q := newTestQueue(t)
	gate := make(chan struct{})

	// Submit a blocking item to occupy the queue.
	go q.Submit(context.Background(), orchestrator.ExportedP1Lead, func(_ context.Context) error {
		<-gate
		return nil
	})
	// Give the dispatcher a moment to start running the fn.
	time.Sleep(10 * time.Millisecond)

	// Submit two more at different priorities without waiting.
	go q.Submit(context.Background(), orchestrator.ExportedP1Lead, func(_ context.Context) error { return nil })
	go q.Submit(context.Background(), orchestrator.ExportedP3Background, func(_ context.Context) error { return nil })
	time.Sleep(10 * time.Millisecond)

	state := q.State()
	if !state.Active {
		t.Error("expected Active=true while fn is running")
	}
	if state.P1 < 1 {
		t.Errorf("expected at least 1 waiting at P1, got %d", state.P1)
	}
	if state.P3 < 1 {
		t.Errorf("expected at least 1 waiting at P3, got %d", state.P3)
	}

	close(gate) // let it finish
	time.Sleep(50 * time.Millisecond)
	if q.State().Active {
		t.Error("expected Active=false after completion")
	}
}

// --- Confidence Signal parsing -------------------------------------------

func TestParseConfidenceSignal_Hit(t *testing.T) {
text := `Some prose before.
{"confidence_score": 0.85, "reason": "All files confirmed"}
Some prose after.`
sig := orchestrator.ExportedParseConfidenceSignal(text)
if sig == nil {
t.Fatal("expected signal, got nil")
}
if sig.ConfidenceScore != 0.85 {
t.Errorf("score = %v, want 0.85", sig.ConfidenceScore)
}
if sig.Reason != "All files confirmed" {
t.Errorf("reason = %q", sig.Reason)
}
}

func TestParseConfidenceSignal_Miss(t *testing.T) {
text := `{"tool": "file_manager", "args": {"op": "read", "path": "x"}}`
if sig := orchestrator.ExportedParseConfidenceSignal(text); sig != nil {
t.Errorf("expected nil for non-confidence JSON, got %+v", sig)
}
}

func TestParseConfidenceSignal_LowThreshold(t *testing.T) {
text := `{"confidence_score": 0.45, "reason": "path is ambiguous"}`
sig := orchestrator.ExportedParseConfidenceSignal(text)
if sig == nil {
t.Fatal("expected signal")
}
if sig.ConfidenceScore >= 0.7 {
t.Errorf("expected score < 0.7, got %v", sig.ConfidenceScore)
}
}

func TestParseConfidenceSignal_HighThreshold(t *testing.T) {
text := `{"confidence_score": 0.95, "reason": "clear instructions"}`
sig := orchestrator.ExportedParseConfidenceSignal(text)
if sig == nil {
t.Fatal("expected signal")
}
if sig.ConfidenceScore < 0.7 {
t.Errorf("expected score >= 0.7, got %v", sig.ConfidenceScore)
}
}

func TestParseConfidenceSignal_Empty(t *testing.T) {
if sig := orchestrator.ExportedParseConfidenceSignal(""); sig != nil {
t.Errorf("expected nil for empty string, got %+v", sig)
}
}

// --- LowConfidenceError via TurnStream -----------------------------------

func TestTurnStream_LowConfidenceReturnsError(t *testing.T) {
	inf := &mockInferrer{response: `{"confidence_score": 0.55, "reason": "path is unclear"}`}
	ra := orchestrator.NewRunningAgentForTest("lead", "Lead", "model", 2, inf, nil)
	_, err := ra.TurnStream(context.Background(), "do something risky", nil)
	if err == nil {
		t.Fatal("expected LowConfidenceError, got nil")
	}
	var lcErr *orchestrator.ExportedLowConfidenceError
	if !errors.As(err, &lcErr) {
		t.Fatalf("expected *LowConfidenceError, got %T: %v", err, err)
	}
	if lcErr.Score >= 0.7 {
		t.Errorf("score = %v, expected < 0.7", lcErr.Score)
	}
	if lcErr.Reason != "path is unclear" {
		t.Errorf("reason = %q", lcErr.Reason)
	}
}

func TestTurnStream_HighConfidenceProceedsNormally(t *testing.T) {
	inf := &mockInferrer{response: "{\"confidence_score\": 0.9, \"reason\": \"clear\"}\nHello, done."}
	ra := orchestrator.NewRunningAgentForTest("lead", "Lead", "model", 2, inf, nil)
	resp, err := ra.TurnStream(context.Background(), "say hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == "" {
		t.Error("expected non-empty response")
	}
}

// --- stripToolCallLines strips confidence signal lines -------------------

func TestStripToolCallLines_StripsConfidenceSignal(t *testing.T) {
text := "Here is my plan.\n{\"confidence_score\": 0.8, \"reason\": \"ok\"}\nProceed."
stripped := orchestrator.ExportedStripToolCallLines(text)
if strings.Contains(stripped, "confidence_score") {
t.Errorf("confidence signal not stripped: %q", stripped)
}
if !strings.Contains(stripped, "Here is my plan") {
t.Errorf("prose stripped incorrectly: %q", stripped)
}
}
