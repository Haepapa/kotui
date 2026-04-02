// Package ollama provides a client for the local Ollama inference engine.
// It supports streaming chat, model management, embeddings, VRAM profiling,
// and a heartbeat monitor for liveness detection.
package ollama

import (
	"encoding/json"
	"fmt"
	"time"
)

// --- Keep-alive -----------------------------------------------------------

// KeepAlive controls how long Ollama retains a model in VRAM after a request.
// Use Forever() for the Lead agent and Release() for Worker agents in swap mode.
type KeepAlive struct {
	forever bool
	d       time.Duration
}

// Forever keeps the model loaded in VRAM indefinitely (Lead agent).
func Forever() *KeepAlive { return &KeepAlive{forever: true} }

// Release unloads the model from VRAM immediately after the response (Workers).
func Release() *KeepAlive { return &KeepAlive{d: 0} }

// ForDuration keeps the model loaded for the specified duration.
func ForDuration(d time.Duration) *KeepAlive { return &KeepAlive{d: d} }

func (k *KeepAlive) MarshalJSON() ([]byte, error) {
	if k == nil {
		return []byte("null"), nil
	}
	if k.forever {
		return []byte(`-1`), nil // Ollama special value: hold indefinitely
	}
	if k.d == 0 {
		return []byte(`0`), nil // Ollama special value: release now
	}
	return json.Marshal(fmt.Sprintf("%ds", int(k.d.Seconds())))
}

// --- Chat -----------------------------------------------------------------

// ChatMessage is a single turn in a conversation.
type ChatMessage struct {
	Role    string `json:"role"`    // "system" | "user" | "assistant" | "tool"
	Content string `json:"content"`
}

// ChatRequest is the payload sent to POST /api/chat.
type ChatRequest struct {
	Model     string        `json:"model"`
	Messages  []ChatMessage `json:"messages"`
	Stream    bool          `json:"stream"`
	KeepAlive *KeepAlive    `json:"keep_alive,omitempty"`
	Options   *ModelOptions `json:"options,omitempty"`
}

// ModelOptions are sampler parameters forwarded to the inference engine.
type ModelOptions struct {
	Temperature float64 `json:"temperature,omitempty"`
	NumCtx      int     `json:"num_ctx,omitempty"`
	NumPredict  int     `json:"num_predict,omitempty"`
	TopP        float64 `json:"top_p,omitempty"`
}

// ChatResponseChunk is one line in the NDJSON stream from /api/chat.
type ChatResponseChunk struct {
	Model           string      `json:"model"`
	CreatedAt       time.Time   `json:"created_at"`
	Message         ChatMessage `json:"message"`
	Done            bool        `json:"done"`
	DoneReason      string      `json:"done_reason,omitempty"`
	TotalDuration   int64       `json:"total_duration,omitempty"`
	LoadDuration    int64       `json:"load_duration,omitempty"`
	EvalCount       int         `json:"eval_count,omitempty"`
	PromptEvalCount int         `json:"prompt_eval_count,omitempty"`
}

// StreamChunk is delivered on the channel returned by ChatStream.
type StreamChunk struct {
	Content string
	Done    bool
}

// ChatResult is the fully-accumulated result of a single chat turn.
type ChatResult struct {
	Content       string
	Model         string
	TotalDuration time.Duration
	EvalCount     int
	Attempts      int // 1 = success on first try, >1 = retried
}

// --- Model info -----------------------------------------------------------

// ModelInfo describes a model that is locally available in Ollama.
type ModelInfo struct {
	Name       string       `json:"name"`
	Model      string       `json:"model"`
	ModifiedAt time.Time    `json:"modified_at"`
	Size       int64        `json:"size"` // bytes on disk ≈ VRAM footprint
	Digest     string       `json:"digest"`
	Details    ModelDetails `json:"details"`
}

// ModelDetails carries structured metadata about a model.
type ModelDetails struct {
	ParameterSize     string `json:"parameter_size"`
	QuantizationLevel string `json:"quantization_level"`
	Family            string `json:"family"`
}

// TagsResponse is the JSON body of GET /api/tags.
type TagsResponse struct {
	Models []ModelInfo `json:"models"`
}

// HealthStatus summarises the current state of the Ollama engine.
type HealthStatus struct {
	OK      bool
	Models  []string
	Message string
}

// --- Embeddings -----------------------------------------------------------

// EmbeddingRequest is the payload sent to POST /api/embeddings.
type EmbeddingRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

// EmbeddingResponse is returned from POST /api/embeddings.
type EmbeddingResponse struct {
	Embedding []float32 `json:"embedding"`
}

// --- Errors ---------------------------------------------------------------

// EscalationError is returned when all retry attempts are exhausted.
// The orchestrator must pause the task and notify the Boss.
type EscalationError struct {
	Attempts int
	Cause    error
}

func (e *EscalationError) Error() string {
	return fmt.Sprintf("ollama: escalation after %d attempts: %v", e.Attempts, e.Cause)
}

func (e *EscalationError) Unwrap() error { return e.Cause }
