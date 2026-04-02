// Package relay implements the Relay Gateway — the bridge between the internal
// Dispatcher message bus and external communication channels (stdout logging in
// headless mode; Telegram/Slack/WhatsApp added in Phase 12).
//
// Architecture:
//
//	Gateway subscribes to the Dispatcher for summary-tier events.
//	Each registered Relay receives a copy of every forwarded message.
//	Relay.Send errors are logged but never crash the gateway — outbound
//	failures must not interrupt agent operation.
//
// Usage:
//
//	gw := relay.New(disp, logger)
//	gw.Register(myTelegramRelay)   // Phase 12
//	defer gw.Close()
package relay

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/pkg/models"
)

// Relay is the interface that all outbound relay adapters must satisfy.
// Phase 12 will add Telegram, Slack, and WhatsApp implementations.
type Relay interface {
	// Name returns a human-readable identifier for logging.
	Name() string
	// Send forwards a message to the external channel.
	// ctx may be cancelled; implementations must respect it.
	Send(ctx context.Context, msg models.Message) error
}

// Gateway subscribes to the Dispatcher and fans messages out to registered Relays.
type Gateway struct {
	log        *slog.Logger
	mu         sync.RWMutex
	relays     []Relay
	unsub      func() // Dispatcher unsubscribe function
	sendTimeout time.Duration
}

// New creates a Gateway and immediately subscribes to summary-tier events on disp.
// The Gateway is ready to forward messages as soon as New returns.
func New(disp *dispatcher.Dispatcher, log *slog.Logger) *Gateway {
	if log == nil {
		log = slog.Default()
	}
	gw := &Gateway{
		log:         log,
		sendTimeout: 10 * time.Second,
	}
	// Subscribe to summary-tier events (these are the human-readable events
	// appropriate for external channels). Pass "" to receive all tiers in
	// headless mode so nothing is silently dropped.
	gw.unsub = disp.Subscribe(models.TierSummary, gw.handle)
	return gw
}

// Register adds a Relay to the gateway's fan-out list.
// Safe to call concurrently and after New.
func (gw *Gateway) Register(r Relay) {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	gw.relays = append(gw.relays, r)
	gw.log.Info("relay registered", "name", r.Name())
}

// Close unsubscribes the gateway from the Dispatcher. Subsequent messages are
// not forwarded. Idempotent.
func (gw *Gateway) Close() {
	if gw.unsub != nil {
		gw.unsub()
		gw.unsub = nil
	}
}

// handle is the internal Dispatcher callback. It logs the event and fans it
// out to all registered relays.
func (gw *Gateway) handle(msg models.Message) {
	gw.log.Info("relay event",
		"project", msg.ProjectID,
		"from", msg.AgentID,
		"tier", msg.Tier,
		"content", truncate(msg.Content, 200),
	)

	gw.mu.RLock()
	relays := make([]Relay, len(gw.relays))
	copy(relays, gw.relays)
	gw.mu.RUnlock()

	if len(relays) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), gw.sendTimeout)
	defer cancel()

	for _, r := range relays {
		if err := r.Send(ctx, msg); err != nil {
			gw.log.Warn("relay send failed",
				"relay", r.Name(),
				"err", err,
			)
			// Never propagate: outbound failures must not interrupt agents.
		}
	}
}

// RelayCount returns the number of currently registered relays. Useful for tests.
func (gw *Gateway) RelayCount() int {
	gw.mu.RLock()
	defer gw.mu.RUnlock()
	return len(gw.relays)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "…"
}
