package tools

import (
	"context"
	"fmt"
	"sync"
	"time"
)

const sudoApprovalTimeout = 10 * time.Minute

// SudoGate serialises sudo-command approvals between the shell executor (which
// blocks waiting for Boss approval) and the warroom service (which resolves the
// decision when the Boss clicks Approve/Reject in the UI).
type SudoGate struct {
	mu        sync.Mutex
	pending   map[string]chan bool
	onRequest func(id, cmd string) // called when a new sudo request arrives
}

// NewSudoGate creates a gate without a request callback. Call SetOnRequest
// after the warroom service and DB are ready to wire the notification logic.
func NewSudoGate() *SudoGate {
	return &SudoGate{pending: make(map[string]chan bool)}
}

// SetOnRequest registers a callback that is called (in a new goroutine) when
// a sudo request arrives. Use it to persist the approval and emit the frontend
// event.
func (g *SudoGate) SetOnRequest(fn func(id, cmd string)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onRequest = fn
}

// Request blocks until the Boss approves or rejects the command (or the context
// / internal timeout expires). Returns true if approved.
// id must be a unique, stable string (used as the approval DB record ID).
func (g *SudoGate) Request(ctx context.Context, id, cmd string) (bool, error) {
	ch := make(chan bool, 1)
	g.mu.Lock()
	g.pending[id] = ch
	cb := g.onRequest
	g.mu.Unlock()

	defer func() {
		g.mu.Lock()
		delete(g.pending, id)
		g.mu.Unlock()
	}()

	// Notify callers asynchronously so we can start blocking immediately.
	if cb != nil {
		go cb(id, cmd)
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, sudoApprovalTimeout)
	defer cancel()

	select {
	case approved := <-ch:
		return approved, nil
	case <-timeoutCtx.Done():
		return false, fmt.Errorf("sudo approval for command timed out after %s — request cancelled", sudoApprovalTimeout)
	}
}

// Resolve unblocks a pending Request with the Boss's decision.
// Returns true if a matching pending request was found and resolved.
func (g *SudoGate) Resolve(id string, approved bool) bool {
	g.mu.Lock()
	ch, ok := g.pending[id]
	if ok {
		delete(g.pending, id)
	}
	g.mu.Unlock()
	if !ok {
		return false
	}
	ch <- approved
	return true
}
