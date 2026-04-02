// Package ollama provides a client for the local Ollama inference engine.
// It supports streaming chat, model management, embeddings, VRAM profiling,
// and a heartbeat monitor for liveness detection.
package ollama

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client communicates with a local Ollama instance over HTTP.
type Client struct {
	endpoint    string
	httpClient  *http.Client
	timeout     time.Duration
	maxRetries  int
	retryDelay  time.Duration // base delay between retries (default 2s)
}

// New creates a Client pointing at endpoint (e.g. "http://localhost:11434").
func New(endpoint string) *Client {
	return &Client{
		endpoint:   strings.TrimRight(endpoint, "/"),
		httpClient: &http.Client{},
		timeout:    90 * time.Second,
		maxRetries: 3,
		retryDelay: 2 * time.Second,
	}
}

// WithTimeout returns a copy of the client with a different per-request timeout.
func (c *Client) WithTimeout(d time.Duration) *Client {
	cp := *c
	cp.timeout = d
	return &cp
}

// WithRetryDelay returns a copy of the client with a different base retry backoff.
func (c *Client) WithRetryDelay(d time.Duration) *Client {
	cp := *c
	cp.retryDelay = d
	return &cp
}

// WithRetries returns a copy of the client with a different retry count.
func (c *Client) WithRetries(n int) *Client {
	cp := *c
	cp.maxRetries = n
	return &cp
}

// --- Chat -----------------------------------------------------------------

// Chat sends messages and accumulates the full streamed response.
// It retries up to maxRetries times on transient failures, then returns
// an EscalationError on exhaustion.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	req.Stream = true
	var (
		result   *ChatResult
		lastErr  error
		attempts int
	)
	for attempt := 1; attempt <= c.maxRetries; attempt++ {
		attempts = attempt
		r, err := c.chatOnce(ctx, req)
		if err == nil {
			r.Attempts = attempts
			return r, nil
		}
		if !isRetryable(err) || ctx.Err() != nil {
			return nil, err
		}
		lastErr = err
		if attempt < c.maxRetries {
			wait := time.Duration(attempt) * c.retryDelay
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	_ = result
	return nil, &EscalationError{Attempts: attempts, Cause: lastErr}
}

// ChatStream sends messages and returns a channel of StreamChunks.
// The caller must drain the channel until Done=true or the channel closes.
// Cancel ctx to abort the stream early.
func (c *Client) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamChunk, error) {
	req.Stream = true
	tctx, cancel := context.WithTimeout(ctx, c.timeout)

	ch := make(chan StreamChunk, 16)
	go func() {
		defer cancel()
		defer close(ch)
		if err := c.streamInto(tctx, req, ch); err != nil {
			ch <- StreamChunk{Done: true}
		}
	}()
	return ch, nil
}

// chatOnce performs a single streaming chat attempt, blocking until done.
func (c *Client) chatOnce(ctx context.Context, req ChatRequest) (*ChatResult, error) {
	tctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("ollama: marshal chat request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(tctx, http.MethodPost, c.url("/api/chat"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: chat HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var (
		sb        strings.Builder
		lastChunk ChatResponseChunk
	)
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ChatResponseChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return nil, fmt.Errorf("ollama: parse stream chunk: %w", err)
		}
		sb.WriteString(chunk.Message.Content)
		if chunk.Done {
			lastChunk = chunk
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("ollama: read stream: %w", err)
	}

	return &ChatResult{
		Content:       sb.String(),
		Model:         lastChunk.Model,
		TotalDuration: time.Duration(lastChunk.TotalDuration),
		EvalCount:     lastChunk.EvalCount,
	}, nil
}

// streamInto reads the NDJSON stream and sends StreamChunks to ch.
func (c *Client) streamInto(ctx context.Context, req ChatRequest, ch chan<- StreamChunk) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/api/chat"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama: chat HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var chunk ChatResponseChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			return err
		}
		ch <- StreamChunk{Content: chunk.Message.Content, Done: chunk.Done}
		if chunk.Done {
			return nil
		}
	}
	return scanner.Err()
}

// --- Model info -----------------------------------------------------------

// ListModels returns all locally-available models from GET /api/tags.
func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	tctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(tctx, http.MethodGet, c.url("/api/tags"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama: tags HTTP %d", resp.StatusCode)
	}
	var tags TagsResponse
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil, fmt.Errorf("ollama: decode tags: %w", err)
	}
	return tags.Models, nil
}

// IsHealthy returns true if Ollama responds to /api/tags within the timeout.
func (c *Client) IsHealthy(ctx context.Context) bool {
	tctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := c.ListModels(tctx)
	return err == nil
}

// Health returns a structured HealthStatus.
func (c *Client) Health(ctx context.Context) HealthStatus {
	models, err := c.ListModels(ctx)
	if err != nil {
		return HealthStatus{OK: false, Message: err.Error()}
	}
	names := make([]string, len(models))
	for i, m := range models {
		names[i] = m.Name
	}
	return HealthStatus{OK: true, Models: names}
}

// FindModel returns the ModelInfo for the named model, or nil if not found.
// Name matching is prefix-based (e.g. "llama3.1:8b" matches "llama3.1:8b-instruct").
func (c *Client) FindModel(ctx context.Context, name string) (*ModelInfo, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}
	for i, m := range models {
		if m.Name == name || strings.HasPrefix(m.Name, name) {
			return &models[i], nil
		}
	}
	return nil, nil
}

// --- Embeddings -----------------------------------------------------------

// Embed generates an embedding vector for the given text using the specified model.
func (c *Client) Embed(ctx context.Context, model, text string) ([]float32, error) {
	tctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	body, _ := json.Marshal(EmbeddingRequest{Model: model, Prompt: text})
	req, err := http.NewRequestWithContext(tctx, http.MethodPost, c.url("/api/embeddings"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("ollama: embed HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var er EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&er); err != nil {
		return nil, fmt.Errorf("ollama: decode embedding: %w", err)
	}
	return er.Embedding, nil
}

// --- Helpers --------------------------------------------------------------

func (c *Client) url(path string) string {
	return c.endpoint + path
}

// isRetryable returns true for errors that warrant a retry attempt.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	// Retry on network errors and 5xx server errors; not on 4xx client errors.
	return !strings.Contains(s, "HTTP 4")
}
