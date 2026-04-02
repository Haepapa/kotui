// Package relay — shared HTTP webhook server for Slack and WhatsApp.
//
// Both relay adapters register their webhook paths on a single HTTP mux so
// only one port needs to be opened/forwarded. The server shuts down gracefully
// on Stop().
package relay

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// WebhookServer runs a single net/http server that hosts multiple relay adapters.
type WebhookServer struct {
	port   int
	mux    *http.ServeMux
	server *http.Server
	log    *slog.Logger
}

// NewWebhookServer creates a WebhookServer on the given port.
func NewWebhookServer(port int, log *slog.Logger) *WebhookServer {
	if log == nil {
		log = slog.Default()
	}
	mux := http.NewServeMux()
	srv := &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return &WebhookServer{port: port, mux: mux, server: srv, log: log}
}

// webhookHandler is implemented by relay adapters that serve inbound webhooks.
type webhookHandler interface {
	WebhookPath() string
	HandleWebhook(w http.ResponseWriter, r *http.Request)
}

// Register adds a relay's webhook endpoint to the mux.
func (ws *WebhookServer) Register(h webhookHandler) {
	ws.mux.HandleFunc(h.WebhookPath(), h.HandleWebhook)
	ws.log.Info("webhook registered", "path", h.WebhookPath())
}

// Start begins listening on the configured port. Returns an error if the port
// is unavailable. Non-blocking — the server runs in a background goroutine.
func (ws *WebhookServer) Start() error {
	addr := fmt.Sprintf(":%d", ws.port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("webhook server: cannot bind to %s: %w", addr, err)
	}
	ws.server.Addr = addr
	ws.log.Info("webhook server listening", "addr", addr)
	go func() {
		if err := ws.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			ws.log.Error("webhook server error", "err", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the HTTP server with a 10-second timeout.
func (ws *WebhookServer) Stop() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ws.server.Shutdown(ctx); err != nil {
		ws.log.Warn("webhook server shutdown error", "err", err)
	}
	ws.log.Info("webhook server stopped")
}
