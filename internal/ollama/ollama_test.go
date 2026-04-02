package ollama_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/haepapa/kotui/internal/ollama"
)

// --- helpers --------------------------------------------------------------

// buildStreamResponse assembles NDJSON lines that Ollama's /api/chat returns.
func buildStreamResponse(model string, tokens []string) string {
	var sb strings.Builder
	for _, tok := range tokens {
		chunk := map[string]interface{}{
			"model":      model,
			"created_at": time.Now().Format(time.RFC3339Nano),
			"message":    map[string]string{"role": "assistant", "content": tok},
			"done":       false,
		}
		b, _ := json.Marshal(chunk)
		sb.Write(b)
		sb.WriteByte('\n')
	}
	// final done chunk
	done := map[string]interface{}{
		"model":          model,
		"created_at":     time.Now().Format(time.RFC3339Nano),
		"message":        map[string]string{"role": "assistant", "content": ""},
		"done":           true,
		"done_reason":    "stop",
		"total_duration": 1_000_000_000,
		"eval_count":     len(tokens),
	}
	b, _ := json.Marshal(done)
	sb.Write(b)
	sb.WriteByte('\n')
	return sb.String()
}

func tagsResponse(models ...string) string {
	infos := make([]map[string]interface{}, len(models))
	for i, m := range models {
		infos[i] = map[string]interface{}{
			"name":  m,
			"model": m,
			"size":  int64(5 * 1024 * 1024 * 1024),
		}
	}
	b, _ := json.Marshal(map[string]interface{}{"models": infos})
	return string(b)
}

// --- Chat streaming -------------------------------------------------------

func TestChatStreamingAccumulatesContent(t *testing.T) {
	tokens := []string{"Hello", ", ", "world", "!"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprint(w, buildStreamResponse("llama3.1:8b", tokens))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	result, err := c.Chat(context.Background(), ollama.ChatRequest{
		Model:    "llama3.1:8b",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}
	want := "Hello, world!"
	if result.Content != want {
		t.Errorf("Content = %q, want %q", result.Content, want)
	}
	if result.EvalCount != len(tokens) {
		t.Errorf("EvalCount = %d, want %d", result.EvalCount, len(tokens))
	}
	if result.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", result.Attempts)
	}
}

func TestChatStreamChannel(t *testing.T) {
	tokens := []string{"one", " two", " three"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, buildStreamResponse("test-model", tokens))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	ch, err := c.ChatStream(context.Background(), ollama.ChatRequest{
		Model:    "test-model",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "go"}},
	})
	if err != nil {
		t.Fatalf("ChatStream() error: %v", err)
	}

	var got strings.Builder
	for chunk := range ch {
		got.WriteString(chunk.Content)
		if chunk.Done {
			break
		}
	}
	if got.String() != "one two three" {
		t.Errorf("stream content = %q, want %q", got.String(), "one two three")
	}
}

// --- Retry ----------------------------------------------------------------

func TestChatRetriesOnTransientFailure(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n < 3 {
			// First two calls fail with 503
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		fmt.Fprint(w, buildStreamResponse("m", []string{"ok"}))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL).WithTimeout(5 * time.Second).WithRetryDelay(10 * time.Millisecond)
	result, err := c.Chat(context.Background(), ollama.ChatRequest{
		Model:    "m",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "test"}},
	})
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if result.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", result.Attempts)
	}
}

func TestChatEscalationAfterMaxRetries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL).WithTimeout(2 * time.Second).WithRetries(3).WithRetryDelay(10 * time.Millisecond)
	_, err := c.Chat(context.Background(), ollama.ChatRequest{
		Model:    "m",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "fail"}},
	})
	if err == nil {
		t.Fatal("expected EscalationError, got nil")
	}
	var esc *ollama.EscalationError
	if !errorAs(err, &esc) {
		t.Errorf("expected EscalationError, got %T: %v", err, err)
	}
	if esc != nil && esc.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", esc.Attempts)
	}
}

// errorAs is a local helper to avoid importing errors package in test.
func errorAs(err error, target **ollama.EscalationError) bool {
	for err != nil {
		if e, ok := err.(*ollama.EscalationError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		if u, ok := err.(unwrapper); ok {
			err = u.Unwrap()
		} else {
			break
		}
	}
	return false
}

func TestChatNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL).WithTimeout(2 * time.Second)
	_, err := c.Chat(context.Background(), ollama.ChatRequest{
		Model:    "m",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected error on 400")
	}
	if calls.Load() != 1 {
		t.Errorf("expected exactly 1 call for 400 (no retry), got %d", calls.Load())
	}
}

// --- Timeout --------------------------------------------------------------

func TestChatTimeoutReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Stall until the client disconnects or a short safety window expires.
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}))
	// Force-close any lingering connections before shutting down the test server.
	t.Cleanup(func() {
		srv.CloseClientConnections()
		srv.Close()
	})

	c := ollama.New(srv.URL).WithTimeout(50 * time.Millisecond).WithRetries(1)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := c.Chat(ctx, ollama.ChatRequest{
		Model:    "m",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "x"}},
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

// --- Health / ListModels --------------------------------------------------

func TestListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, tagsResponse("llama3.1:8b", "qwen2.5-coder:32b"))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	models, err := c.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestIsHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, tagsResponse())
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	if !c.IsHealthy(context.Background()) {
		t.Error("expected IsHealthy=true for responding server")
	}
}

func TestIsHealthyFalseWhenDown(t *testing.T) {
	// Point at a server that refuses connections.
	c := ollama.New("http://127.0.0.1:19999") // nothing listening here
	if c.IsHealthy(context.Background()) {
		t.Error("expected IsHealthy=false for unreachable server")
	}
}

func TestFindModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, tagsResponse("llama3.1:8b", "qwen2.5-coder:32b"))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	m, err := c.FindModel(context.Background(), "llama3.1:8b")
	if err != nil {
		t.Fatal(err)
	}
	if m == nil {
		t.Fatal("expected to find llama3.1:8b")
	}
	missing, err := c.FindModel(context.Background(), "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Error("expected nil for nonexistent model")
	}
}

// --- Embeddings -----------------------------------------------------------

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			http.NotFound(w, r)
			return
		}
		b, _ := json.Marshal(map[string]interface{}{
			"embedding": []float32{0.1, 0.2, 0.3},
		})
		w.Write(b)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	vec, err := c.Embed(context.Background(), "nomic-embed-text", "hello world")
	if err != nil {
		t.Fatalf("Embed() error: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("expected 3 floats, got %d", len(vec))
	}
}

// --- KeepAlive JSON -------------------------------------------------------

func TestKeepAliveForeverMarshal(t *testing.T) {
	req := ollama.ChatRequest{
		Model:     "test",
		Messages:  []ollama.ChatMessage{},
		Stream:    true,
		KeepAlive: ollama.Forever(),
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"keep_alive":-1`) {
		t.Errorf("expected keep_alive:-1 in JSON, got: %s", string(b))
	}
}

func TestKeepAliveReleaseMarshal(t *testing.T) {
	req := ollama.ChatRequest{
		Model:     "test",
		Messages:  []ollama.ChatMessage{},
		KeepAlive: ollama.Release(),
	}
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"keep_alive":0`) {
		t.Errorf("expected keep_alive:0 in JSON, got: %s", string(b))
	}
}

// --- Heartbeat monitor ----------------------------------------------------

func TestHeartbeatDetectsFailure(t *testing.T) {
	var healthy atomic.Bool
	healthy.Store(true)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if healthy.Load() {
			fmt.Fprint(w, tagsResponse())
		} else {
			// Simulate OOM / unresponsive
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()

	emergencies := make(chan string, 5)
	c := ollama.New(srv.URL).WithTimeout(200 * time.Millisecond)
	mon := ollama.NewMonitor(c, 50*time.Millisecond, func(msg string) {
		emergencies <- msg
	})
	mon.Start()
	defer mon.Stop()

	// Initially healthy — no emergency.
	time.Sleep(120 * time.Millisecond)
	select {
	case msg := <-emergencies:
		t.Errorf("unexpected emergency while healthy: %s", msg)
	default:
	}

	// Now simulate failure.
	healthy.Store(false)
	select {
	case msg := <-emergencies:
		if msg == "" {
			t.Error("expected non-empty emergency message")
		}
	case <-time.After(500 * time.Millisecond):
		t.Error("timed out waiting for emergency event")
	}
}

func TestHeartbeatStop(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, tagsResponse())
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	mon := ollama.NewMonitor(c, 20*time.Millisecond, nil)
	mon.Start()
	mon.Stop() // should not hang
}

// --- Chat request body validation -----------------------------------------

func TestChatSendsCorrectBody(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedBody, _ = io.ReadAll(r.Body)
		fmt.Fprint(w, buildStreamResponse("m", []string{"hi"}))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	c.Chat(context.Background(), ollama.ChatRequest{
		Model:    "m",
		Messages: []ollama.ChatMessage{{Role: "user", Content: "hello"}},
	})

	var body map[string]interface{}
	if err := json.Unmarshal(receivedBody, &body); err != nil {
		t.Fatalf("invalid JSON body sent to Ollama: %v", err)
	}
	if body["stream"] != true {
		t.Error("expected stream=true in request body")
	}
	if body["model"] != "m" {
		t.Errorf("expected model=m, got %v", body["model"])
	}
}
