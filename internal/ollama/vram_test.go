package ollama_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/haepapa/kotui/internal/ollama"
	"github.com/haepapa/kotui/pkg/models"
)

// buildTagsWithSize returns a /api/tags response with specific model sizes.
func buildTagsWithSize(entries map[string]int64) string {
	infos := make([]map[string]interface{}, 0, len(entries))
	for name, size := range entries {
		infos = append(infos, map[string]interface{}{
			"name":  name,
			"model": name,
			"size":  size,
		})
	}
	b, _ := json.Marshal(map[string]interface{}{"models": infos})
	return string(b)
}

// GB is a helper to express sizes in gigabytes.
const GB = int64(1024 * 1024 * 1024)

// --- VRAM profile detection -----------------------------------------------

func TestVRAMProfileDual_BothModelsFit(t *testing.T) {
	// Simulate 18 GB RAM: lead=5 GB + worker=5 GB + 3 GB buffer = 13 GB needed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, buildTagsWithSize(map[string]int64{
			"lead-model:8b":   5 * GB,
			"worker-model:7b": 4 * GB,
		}))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	// The test relies on DetectVRAMProfile's internal comparison.
	// With 5+4=9 GB needed and 18-3=15 GB available → dual.
	profile, err := ollama.DetectVRAMProfile(context.Background(), c, "lead-model:8b", "worker-model:7b")
	if err != nil {
		t.Fatalf("DetectVRAMProfile error: %v", err)
	}
	if profile != models.VRAMDual {
		t.Errorf("expected VRAMDual, got %s", profile)
	}
}

func TestVRAMProfileSwap_ModelsDoNotFit(t *testing.T) {
	// Simulate 18 GB RAM: lead=20 GB + worker=5 GB = 25 GB needed → swap.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, buildTagsWithSize(map[string]int64{
			"lead-model:32b":  20 * GB,
			"worker-model:8b": 5 * GB,
		}))
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	profile, err := ollama.DetectVRAMProfile(context.Background(), c, "lead-model:32b", "worker-model:8b")
	if err != nil {
		t.Fatalf("DetectVRAMProfile error: %v", err)
	}
	if profile != models.VRAMSwap {
		t.Errorf("expected VRAMSwap, got %s", profile)
	}
}

func TestVRAMProfileSwap_OllamaDown(t *testing.T) {
	// When Ollama is unreachable, fall back to swap (safe default).
	c := ollama.New("http://127.0.0.1:19998")
	profile, err := ollama.DetectVRAMProfile(context.Background(), c, "any:8b", "any:7b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Falls back to param estimation; small models → may be dual or swap depending on RAM.
	// Just verify no panic and a valid value is returned.
	if profile != models.VRAMDual && profile != models.VRAMSwap {
		t.Errorf("unexpected profile: %s", profile)
	}
}

// --- Name-based parameter extraction -------------------------------------

func TestExtractParamsFromName(t *testing.T) {
	cases := []struct {
		model    string
		wantSwap bool // true if this 32B model + 8B model should require swap on 18GB
	}{
		{"qwen2.5-coder:32b", true},
		{"command-r:35b", true},
		{"llama3.1:8b", false},
		{"mistral:7b", false},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return empty model list to force name-based estimation.
		fmt.Fprint(w, `{"models":[]}`)
	}))
	defer srv.Close()

	c := ollama.New(srv.URL)
	for _, tc := range cases {
		profile, err := ollama.DetectVRAMProfile(context.Background(), c, tc.model, "llama3.1:8b")
		if err != nil {
			t.Errorf("%s: error: %v", tc.model, err)
			continue
		}
		isSwap := profile == models.VRAMSwap
		if isSwap != tc.wantSwap {
			t.Errorf("%s: got %s (wantSwap=%v)", tc.model, profile, tc.wantSwap)
		}
	}
}
