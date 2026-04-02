package memory_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/haepapa/kotui/internal/memory"
	"github.com/haepapa/kotui/internal/store"
)

// --- cosine similarity (exported via a test helper) ----------------------

// cosinePub is a thin wrapper so we can test the unexported cosine function
// without making it public. We test it indirectly via Recall behaviour and
// also directly here using the exported package-level test.
func TestCosineIdentical(t *testing.T) {
	// Two identical non-zero vectors must give similarity ≈ 1.0.
	entries := []store.JournalEmbedding{
		{ID: "1", AgentID: "a", ProjectID: "p", Content: "hello", Embedding: []float32{1, 0, 0}, IsFeedback: false},
	}
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	emb := &fixedEmbedder{vec: []float32{1, 0, 0}}
	mem := memory.New(db, emb, "test", nil)

	ctx := context.Background()
	if err := db.SaveJournalEmbedding(ctx, entries[0]); err != nil {
		t.Fatalf("save: %v", err)
	}

	results, err := mem.Recall(ctx, "a", "p", "query", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestCosineOrthogonal(t *testing.T) {
	// Orthogonal vectors → similarity 0.0 → should be filtered below threshold.
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	// Store an entry with vector [0,1,0]
	entry := store.JournalEmbedding{
		ID: "1", AgentID: "a", ProjectID: "p", Content: "orthogonal",
		Embedding: []float32{0, 1, 0},
	}
	ctx := context.Background()
	if err := db.SaveJournalEmbedding(ctx, entry); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Query with [1,0,0] — cosine similarity = 0 → below threshold
	emb := &fixedEmbedder{vec: []float32{1, 0, 0}}
	mem := memory.New(db, emb, "test", nil)

	results, err := mem.Recall(ctx, "a", "p", "query", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for orthogonal vectors, got %d", len(results))
	}
}

func TestCosineZeroVector(t *testing.T) {
	// Zero vector → similarity 0.0 → filtered out.
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	entry := store.JournalEmbedding{
		ID: "1", AgentID: "a", ProjectID: "p", Content: "zero",
		Embedding: []float32{0, 0, 0},
	}
	ctx := context.Background()
	if err := db.SaveJournalEmbedding(ctx, entry); err != nil {
		t.Fatalf("save: %v", err)
	}

	emb := &fixedEmbedder{vec: []float32{1, 0, 0}}
	mem := memory.New(db, emb, "test", nil)

	results, err := mem.Recall(ctx, "a", "p", "query", 5)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("expected 0 results for zero vector, got %d", len(results))
	}
}

// --- FormatRecall --------------------------------------------------------

func TestFormatRecallEmpty(t *testing.T) {
	out := memory.FormatRecall(nil)
	if out != "" {
		t.Fatalf("expected empty string for nil entries, got %q", out)
	}
	out = memory.FormatRecall([]store.JournalEmbedding{})
	if out != "" {
		t.Fatalf("expected empty string for empty entries, got %q", out)
	}
}

func TestFormatRecallContainsPastExperience(t *testing.T) {
	entries := []store.JournalEmbedding{
		{Content: "solved a hard problem", IsFeedback: false},
	}
	out := memory.FormatRecall(entries)
	if !strings.Contains(out, "Past Experience") {
		t.Fatalf("expected 'Past Experience' in output, got: %q", out)
	}
	if !strings.Contains(out, "solved a hard problem") {
		t.Fatalf("expected content in output, got: %q", out)
	}
}

func TestFormatRecallFeedbackLabel(t *testing.T) {
	entries := []store.JournalEmbedding{
		{Content: "don't use globals", IsFeedback: true},
		{Content: "normal journal", IsFeedback: false},
	}
	out := memory.FormatRecall(entries)
	if !strings.Contains(out, "BOSS FEEDBACK") {
		t.Fatalf("expected 'BOSS FEEDBACK' label in output, got: %q", out)
	}
	if !strings.Contains(out, "[Journal]") {
		t.Fatalf("expected '[Journal]' label in output, got: %q", out)
	}
}

// --- Recall with in-memory SQLite ----------------------------------------

func TestRecallTopK(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// Index 5 entries with decreasing similarity to query vector [1,0,0].
	// We vary the first component so cosine similarity differs.
	entries := []struct {
		id      string
		vec     []float32
		content string
		feedback bool
	}{
		{"1", []float32{0.9, 0.1, 0}, "entry high", false},
		{"2", []float32{0.8, 0.2, 0}, "entry mid-high", false},
		{"3", []float32{0.7, 0.3, 0}, "entry mid", false},
		{"4", []float32{0.6, 0.4, 0}, "entry mid-low", false},
		{"5", []float32{0.5, 0.5, 0}, "entry low", false},
	}
	for _, e := range entries {
		if err := db.SaveJournalEmbedding(ctx, store.JournalEmbedding{
			ID:         e.id,
			AgentID:    "worker",
			ProjectID:  "proj",
			Content:    e.content,
			Embedding:  e.vec,
			IsFeedback: e.feedback,
		}); err != nil {
			t.Fatalf("save %s: %v", e.id, err)
		}
	}

	emb := &fixedEmbedder{vec: []float32{1, 0, 0}}
	mem := memory.New(db, emb, "test", nil)

	results, err := mem.Recall(ctx, "worker", "proj", "query", 3)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// First result should be highest similarity (entry 1).
	if results[0].ID != "1" {
		t.Fatalf("expected entry '1' first, got %q", results[0].ID)
	}
}

func TestRecallFeedbackBoost(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	ctx := context.Background()

	// feedback entry has slightly lower raw similarity than the normal one,
	// but with 1.2× boost it should rank first.
	if err := db.SaveJournalEmbedding(ctx, store.JournalEmbedding{
		ID: "normal", AgentID: "a", ProjectID: "p",
		Content: "normal", Embedding: []float32{0.9, 0.1, 0}, IsFeedback: false,
	}); err != nil {
		t.Fatalf("save normal: %v", err)
	}
	if err := db.SaveJournalEmbedding(ctx, store.JournalEmbedding{
		ID: "feedback", AgentID: "a", ProjectID: "p",
		Content: "boss feedback", Embedding: []float32{0.85, 0.1, 0}, IsFeedback: true,
	}); err != nil {
		t.Fatalf("save feedback: %v", err)
	}

	emb := &fixedEmbedder{vec: []float32{1, 0, 0}}
	mem := memory.New(db, emb, "test", nil)

	results, err := mem.Recall(ctx, "a", "p", "query", 2)
	if err != nil {
		t.Fatalf("recall: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	// feedback gets boost so it should rank first.
	if results[0].ID != "feedback" {
		t.Fatalf("expected feedback entry first (boost), got %q", results[0].ID)
	}
}

// --- helpers -------------------------------------------------------------

// fixedEmbedder always returns the same vector, regardless of input text.
type fixedEmbedder struct {
	vec []float32
}

func (f *fixedEmbedder) Embed(_ context.Context, _, _ string) ([]float32, error) {
	out := make([]float32, len(f.vec))
	copy(out, f.vec)
	return out, nil
}
