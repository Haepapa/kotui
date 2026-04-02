// Package dispatcher implements the central Go channel message bus.
//
// All inter-agent and system events flow through the Dispatcher. It fans out
// each message to registered subscribers (UI, relays, store).
//
// This is a stub for Phase 2. The full implementation lives here.
package dispatcher

import (
	"sync"

	"github.com/haepapa/kotui/pkg/models"
)

// Envelope wraps a message for routing through the bus.
type Envelope struct {
	Message models.Message
}

// Subscriber is a function that receives dispatched envelopes.
type Subscriber func(Envelope)

// Dispatcher is the central message bus.
type Dispatcher struct {
	mu          sync.RWMutex
	subscribers []Subscriber
}

// New creates a ready-to-use Dispatcher.
func New() *Dispatcher {
	return &Dispatcher{}
}

// Subscribe registers a subscriber. Returns an unsubscribe function.
func (d *Dispatcher) Subscribe(s Subscriber) func() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.subscribers = append(d.subscribers, s)
	idx := len(d.subscribers) - 1
	return func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.subscribers = append(d.subscribers[:idx], d.subscribers[idx+1:]...)
	}
}

// Dispatch sends an envelope to all registered subscribers.
func (d *Dispatcher) Dispatch(env Envelope) {
	d.mu.RLock()
	subs := make([]Subscriber, len(d.subscribers))
	copy(subs, d.subscribers)
	d.mu.RUnlock()
	for _, s := range subs {
		s(env)
	}
}
