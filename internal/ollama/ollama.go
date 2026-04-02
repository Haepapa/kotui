// Package ollama provides a client for the local Ollama inference engine.
// This is a stub for Phase 1. The full implementation lives here.
package ollama

// Client is a stub for the Ollama HTTP client.
// Full implementation in Phase 1.
type Client struct {
	endpoint string
}

// New creates a new Ollama client pointing at the given endpoint.
func New(endpoint string) *Client {
	return &Client{endpoint: endpoint}
}
