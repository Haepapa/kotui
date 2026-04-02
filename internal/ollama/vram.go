package ollama

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/haepapa/kotui/pkg/models"
)

const (
	// safetyBufferBytes is reserved for OS + other processes.
	safetyBufferBytes = 3 * 1024 * 1024 * 1024 // 3 GB
)

// paramSizePattern matches e.g. "8b", "32b", "7b", "70b" in a model tag.
var paramSizePattern = regexp.MustCompile(`(?i)(?:^|[^0-9])(\d+)b(?:[^0-9]|$)`)

// DetectVRAMProfile determines whether the host can load the Lead and one Worker
// model simultaneously (dual) or must swap them (park Lead, load Worker, reload Lead).
//
// It queries Ollama for the on-disk sizes of the configured models. If a model
// is not yet pulled, it falls back to a parameter-count estimate.
func DetectVRAMProfile(ctx context.Context, c *Client, leadModel, workerModel string) (models.VRAMProfile, error) {
	totalRAM, err := totalSystemMemory()
	if err != nil {
		// If we can't determine RAM, assume swap to be safe.
		return models.VRAMSwap, nil
	}

	available := int64(totalRAM) - safetyBufferBytes
	if available <= 0 {
		return models.VRAMSwap, nil
	}

	modelList, _ := c.ListModels(ctx) // ignore error — fall back to estimates

	leadSize := modelSizeBytes(leadModel, modelList)
	workerSize := modelSizeBytes(workerModel, modelList)

	if leadSize+workerSize <= available {
		return models.VRAMDual, nil
	}
	return models.VRAMSwap, nil
}

// modelSizeBytes returns the on-disk size of a model from the Ollama listing,
// falling back to a parameter-count based estimate if not found.
func modelSizeBytes(name string, available []ModelInfo) int64 {
	if available != nil {
		for _, m := range available {
			if m.Name == name || strings.HasPrefix(m.Name, strings.Split(name, ":")[0]) {
				if m.Size > 0 {
					return m.Size
				}
			}
		}
	}
	return estimateFromName(name)
}

// estimateFromName estimates VRAM footprint in bytes from the model name.
// Assumes Q4_K_M quantization as a baseline.
func estimateFromName(name string) int64 {
	params := extractParams(name)
	switch {
	case params <= 0:
		return 5 * 1024 * 1024 * 1024 // unknown: assume 5 GB
	case params <= 3:
		return 2 * 1024 * 1024 * 1024
	case params <= 8:
		return 5 * 1024 * 1024 * 1024
	case params <= 14:
		return 9 * 1024 * 1024 * 1024
	case params <= 20:
		return 13 * 1024 * 1024 * 1024
	case params <= 34:
		return 20 * 1024 * 1024 * 1024
	case params <= 72:
		return 40 * 1024 * 1024 * 1024
	default:
		return 80 * 1024 * 1024 * 1024
	}
}

// extractParams parses the parameter count (in billions) from a model name.
// e.g. "qwen2.5-coder:32b" → 32, "llama3.1:8b-instruct" → 8.
func extractParams(name string) int64 {
	lower := strings.ToLower(name)
	matches := paramSizePattern.FindAllStringSubmatch(lower, -1)
	for _, m := range matches {
		if len(m) >= 2 {
			if n, err := strconv.ParseInt(m[1], 10, 64); err == nil && n > 0 && n < 1000 {
				return n
			}
		}
	}
	return 0
}
