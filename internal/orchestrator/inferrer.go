package orchestrator

import (
	"context"

	"github.com/haepapa/kotui/internal/ollama"
)

// Inferrer is the interface the Orchestrator uses for model inference.
// *ollama.Client implements this; tests inject a mock.
type Inferrer interface {
	Chat(ctx context.Context, req ollama.ChatRequest) (*ollama.ChatResult, error)
	ChatStream(ctx context.Context, req ollama.ChatRequest) (<-chan ollama.StreamChunk, error)
	IsHealthy(ctx context.Context) bool
}

// ClientAdapter wraps *ollama.Client to implement Inferrer.
// Using a named adapter keeps the interface minimal — we only expose what
// the Orchestrator actually needs.
type ClientAdapter struct {
	c *ollama.Client
}

// NewClientAdapter wraps an *ollama.Client.
func NewClientAdapter(c *ollama.Client) Inferrer {
	return &ClientAdapter{c: c}
}

func (a *ClientAdapter) Chat(ctx context.Context, req ollama.ChatRequest) (*ollama.ChatResult, error) {
	return a.c.Chat(ctx, req)
}

func (a *ClientAdapter) ChatStream(ctx context.Context, req ollama.ChatRequest) (<-chan ollama.StreamChunk, error) {
	return a.c.ChatStream(ctx, req)
}

func (a *ClientAdapter) IsHealthy(ctx context.Context) bool {
	return a.c.IsHealthy(ctx)
}
