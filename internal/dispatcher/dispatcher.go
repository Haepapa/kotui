// Package dispatcher implements the central Go channel message bus.
//
// All inter-agent and system events flow through the Dispatcher. It fans out
// each message to registered subscribers (UI, relays, store persister) and
// manages the active project context so every message is correctly scoped.
//
// Design: the Dispatcher has NO direct database dependency. Persistence is
// handled by a StorePersister that registers itself as a subscriber. This
// keeps the Dispatcher testable without a database.
package dispatcher

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/haepapa/kotui/pkg/models"
)

// newID generates a random UUID-like identifier (same format as store.NewID).
func newID() string {
	b := make([]byte, 16)
	rand.Read(b) //nolint:errcheck
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// Handler is a function that receives a dispatched message.
type Handler func(models.Message)

type subscriber struct {
	tier    models.LogTier // empty = receive all tiers
	handler Handler
}

// Dispatcher is the central message bus for the War Room.
type Dispatcher struct {
	mu        sync.RWMutex
	subs      []subscriber
	projectID atomic.Value // stores string
}

// New creates a ready-to-use Dispatcher with no active project.
func New() *Dispatcher {
	d := &Dispatcher{}
	d.projectID.Store("")
	return d
}

// Subscribe registers a handler for messages matching the given tier.
// Pass an empty LogTier to receive messages of every tier.
// Returns an unsubscribe function — call it to deregister the handler.
func (d *Dispatcher) Subscribe(tier models.LogTier, h Handler) func() {
	d.mu.Lock()
	defer d.mu.Unlock()
	sub := subscriber{tier: tier, handler: h}
	d.subs = append(d.subs, sub)
	idx := len(d.subs) - 1
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		if idx < len(d.subs) {
			d.subs = append(d.subs[:idx], d.subs[idx+1:]...)
		}
	}
}

// Dispatch tags the message with the active project ID and ensures it has a
// stable ID and timestamp before fanning it out to all matching subscribers.
// Stamping here means both the StorePersister (DB) and the event bridge
// (frontend Wails event) see the same ID, preventing key collisions in the
// Svelte {#each} loop that caused channel messages to vanish.
func (d *Dispatcher) Dispatch(msg models.Message) {
	if msg.ProjectID == "" {
		msg.ProjectID = d.ProjectID()
	}
	if msg.ID == "" {
		msg.ID = newID()
	}
	if msg.CreatedAt.IsZero() {
		msg.CreatedAt = time.Now()
	}

	d.mu.RLock()
	subs := make([]subscriber, len(d.subs))
	copy(subs, d.subs)
	d.mu.RUnlock()

	for _, s := range subs {
		if s.tier == "" || s.tier == msg.Tier {
			s.handler(msg)
		}
	}
}

// SetProject sets the active project ID. All subsequent dispatches without an
// explicit ProjectID will be tagged with this value.
func (d *Dispatcher) SetProject(id string) {
	d.projectID.Store(id)
}

// ProjectID returns the currently active project ID.
func (d *Dispatcher) ProjectID() string {
	v := d.projectID.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// DispatchSummary is a convenience method that dispatches a summary-tier message.
func (d *Dispatcher) DispatchSummary(msg models.Message) {
	msg.Tier = models.TierSummary
	d.Dispatch(msg)
}

// DispatchRaw is a convenience method that dispatches a raw-tier message.
func (d *Dispatcher) DispatchRaw(msg models.Message) {
	msg.Tier = models.TierRaw
	d.Dispatch(msg)
}
