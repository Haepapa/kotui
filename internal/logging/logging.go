// Package logging provides a tiered structured logger built on slog.
//
// Two tiers are defined:
//   - Summary: high-level milestones routed to the Group Chat (Boss-visible).
//   - Raw:     detailed tool calls and LLM reasoning routed to the Engine Room (Dev Mode).
//
// Subscribers register via Subscribe() to receive log records filtered by tier.
package logging

import (
	"context"
	"log/slog"
	"os"
	"sync"

	"github.com/haepapa/kotui/pkg/models"
)

// Record is a log record delivered to subscribers.
type Record struct {
	Tier    models.LogTier
	Level   slog.Level
	Message string
	Attrs   []slog.Attr
}

// Handler is a function that receives a log Record.
type Handler func(Record)

var (
	mu          sync.RWMutex
	subscribers []subscriber
)

type subscriber struct {
	tier    models.LogTier // empty string = all tiers
	handler Handler
}

// Subscribe registers a handler to receive log records. Pass an empty tier to
// receive records from all tiers. Returns an unsubscribe function.
func Subscribe(tier models.LogTier, h Handler) func() {
	mu.Lock()
	defer mu.Unlock()
	sub := subscriber{tier: tier, handler: h}
	subscribers = append(subscribers, sub)
	return func() {
		mu.Lock()
		defer mu.Unlock()
		for i, s := range subscribers {
			if &s == &sub {
				subscribers = append(subscribers[:i], subscribers[i+1:]...)
				return
			}
		}
	}
}

func dispatch(r Record) {
	mu.RLock()
	defer mu.RUnlock()
	for _, sub := range subscribers {
		if sub.tier == "" || sub.tier == r.Tier {
			sub.handler(r)
		}
	}
}

// tieredHandler implements slog.Handler and fans records out to subscribers.
type tieredHandler struct {
	tier    models.LogTier
	console *slog.Logger // fallback to stderr
}

func (h *tieredHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }

func (h *tieredHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, a)
		return true
	})
	dispatch(Record{
		Tier:    h.tier,
		Level:   r.Level,
		Message: r.Message,
		Attrs:   attrs,
	})
	return nil
}

func (h *tieredHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h
}

func (h *tieredHandler) WithGroup(name string) slog.Handler {
	return h
}

var (
	// Summary is the logger for Boss-visible milestone events.
	Summary *slog.Logger
	// Raw is the logger for detailed Engine Room events.
	Raw *slog.Logger
	// Console is a plain stderr logger used during early startup before the UI is ready.
	Console *slog.Logger
)

func init() {
	Summary = slog.New(&tieredHandler{tier: models.TierSummary})
	Raw = slog.New(&tieredHandler{tier: models.TierRaw})
	Console = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}
