package orchestrator

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/haepapa/kotui/internal/ollama"
)

// CogPriority is the scheduling tier for a cognition request.
// Higher-priority requests always run before lower-priority waiting ones.
type CogPriority int

const (
	P0Emergency   CogPriority = iota // OOM recovery, heartbeat checks
	P1Lead                           // Boss DM, war-room channel
	P2Interactive                    // Candidate trials / hiring interviews
	P3Background                     // Background specialist tasks
)

// String returns a human-readable label for logging.
func (p CogPriority) String() string {
	switch p {
	case P0Emergency:
		return "P0"
	case P1Lead:
		return "P1"
	case P2Interactive:
		return "P2"
	case P3Background:
		return "P3"
	default:
		return "unknown"
	}
}

// CogFn is the unit of work dispatched by the queue.
// It receives the caller's context (possibly wrapped with a watchdog deadline).
type CogFn func(ctx context.Context) error

// cogRequest is a single queued inference request.
type cogRequest struct {
	priority CogPriority
	ctx      context.Context // caller's context; may be cancelled mid-queue
	fn       CogFn
	doneCh   chan error // buffered(1); receives fn's return value or ctx.Err()
}

// QueueState is a point-in-time snapshot of the queue, suitable for the
// kotui:queue_state frontend event.
type QueueState struct {
	P0     int  `json:"p0"`
	P1     int  `json:"p1"`
	P2     int  `json:"p2"`
	P3     int  `json:"p3"`
	Active bool `json:"active"` // true while a request is executing
}

// CogQueue routes all Ollama inference requests through a P0→P3 priority
// queue so that higher-priority calls (Boss DM, emergency) always run before
// lower-priority background work. Only one request executes at a time.
//
// P3 (Background) requests are additionally gated by the SystemMonitor: if
// the system is under CPU/RAM pressure the queue pauses P3 work until
// pressure relieves (up to p3MaxPressureWait before giving up).
//
// The queue is safe for concurrent use. Start must be called once before
// the first Submit.
type CogQueue struct {
	p0, p1, p2, p3 chan cogRequest

	// waiting[i] is the count of requests at priority i that are enqueued
	// and not yet executing. Decremented when execution starts (or the
	// request is skipped due to a cancelled context).
	waiting [4]atomic.Int32

	// active is true while a request's CogFn is running.
	active atomic.Bool

	// onState is called after every enqueue/dequeue and active state
	// change. May be nil. Used to emit kotui:queue_state events.
	onState func(QueueState)

	// sysmon is used to gate P3 requests under system pressure. May be nil
	// (disables pressure throttling).
	sysmon *ollama.SystemMonitor
}

// p3MaxPressureWait is the maximum time the queue will wait for pressure to
// relieve before dropping a P3 request with an error.
const p3MaxPressureWait = 60 * time.Second

// NewCogQueue creates a stopped CogQueue. Call Start to begin dispatching.
//
// onState, if non-nil, is called on every state change so callers can push
// a kotui:queue_state event to the frontend. It is called synchronously from
// the dispatcher goroutine; keep it non-blocking (e.g. fire-and-forget via a
// goroutine or a buffered channel).
//
// sysmon, if non-nil, gates P3 requests: they pause while IsUnderPressure()
// returns true. Pass nil to disable pressure throttling.
func NewCogQueue(onState func(QueueState), sysmon *ollama.SystemMonitor) *CogQueue {
	return &CogQueue{
		p0:      make(chan cogRequest, 4),
		p1:      make(chan cogRequest, 16),
		p2:      make(chan cogRequest, 8),
		p3:      make(chan cogRequest, 32),
		onState: onState,
		sysmon:  sysmon,
	}
}

// Start launches the background dispatcher goroutine.
// The dispatcher runs until ctx is cancelled.
func (q *CogQueue) Start(ctx context.Context) {
	go q.run(ctx)
}

// Submit enqueues fn at the given priority and blocks until fn completes or
// ctx is cancelled.
//
// Returns the 1-based queue position at submission time (within the priority
// tier) and fn's error. If ctx is cancelled before fn completes, ctx.Err()
// is returned as the error; fn may still execute in the background.
func (q *CogQueue) Submit(ctx context.Context, priority CogPriority, fn CogFn) (pos int32, err error) {
	req := cogRequest{
		priority: priority,
		ctx:      ctx,
		fn:       fn,
		doneCh:   make(chan error, 1),
	}

	// Increment before enqueue so State() is accurate immediately.
	pos = q.waiting[priority].Add(1)
	q.emitState()

	ch := q.chanFor(priority)

	// Enqueue — give up immediately if the caller already cancelled.
	select {
	case ch <- req:
	case <-ctx.Done():
		q.waiting[priority].Add(-1)
		q.emitState()
		return pos, ctx.Err()
	}

	// Wait for the dispatcher to complete fn.
	// If the caller cancels while waiting, we return the error but leave
	// the request in the channel; the dispatcher will skip it.
	select {
	case err = <-req.doneCh:
		return pos, err
	case <-ctx.Done():
		return pos, ctx.Err()
	}
}

// State returns a current snapshot of the queue depths and active status.
func (q *CogQueue) State() QueueState {
	return QueueState{
		P0:     int(q.waiting[P0Emergency].Load()),
		P1:     int(q.waiting[P1Lead].Load()),
		P2:     int(q.waiting[P2Interactive].Load()),
		P3:     int(q.waiting[P3Background].Load()),
		Active: q.active.Load(),
	}
}

// chanFor returns the channel for the given priority.
func (q *CogQueue) chanFor(p CogPriority) chan cogRequest {
	switch p {
	case P0Emergency:
		return q.p0
	case P1Lead:
		return q.p1
	case P2Interactive:
		return q.p2
	default:
		return q.p3
	}
}

// emitState fires onState with the current queue snapshot. Safe to call when
// onState is nil.
func (q *CogQueue) emitState() {
	if q.onState != nil {
		q.onState(q.State())
	}
}

// run is the dispatcher goroutine. It dequeues and executes one request at a
// time in strict priority order (P0 > P1 > P2 > P3).
// P3 requests are additionally held until system pressure relieves.
func (q *CogQueue) run(ctx context.Context) {
	for {
		req, ok := q.dequeue(ctx)
		if !ok {
			return // context cancelled — shut down cleanly
		}

		// Decrement the waiting counter now that we have taken ownership.
		q.waiting[req.priority].Add(-1)

		// If the caller already cancelled, skip execution and signal them.
		if req.ctx.Err() != nil {
			select {
			case req.doneCh <- req.ctx.Err():
			default:
			}
			q.emitState()
			continue
		}

		// P3 pressure throttle: wait for system pressure to relieve before
		// executing background tasks. Higher-priority items will have already
		// been dequeued first (dequeue preserves strict priority).
		if req.priority == P3Background && q.sysmon != nil && q.sysmon.IsUnderPressure() {
			if err := q.waitPressureRelief(ctx, req.ctx); err != nil {
				select {
				case req.doneCh <- err:
				default:
				}
				q.emitState()
				continue
			}
		}

		q.active.Store(true)
		q.emitState()

		execErr := req.fn(req.ctx)

		q.active.Store(false)
		q.emitState()

		// Deliver result. doneCh is buffered(1) so this never blocks even
		// if the caller already bailed via ctx cancellation.
		select {
		case req.doneCh <- execErr:
		default:
		}
	}
}

// waitPressureRelief blocks until the system pressure drops or the deadline
// (p3MaxPressureWait) is reached. Uses exponential backoff starting at 500ms.
// Returns an error if the deadline or a context is cancelled.
func (q *CogQueue) waitPressureRelief(qCtx, reqCtx context.Context) error {
	deadline := time.Now().Add(p3MaxPressureWait)
	backoff := 500 * time.Millisecond
	for q.sysmon.IsUnderPressure() {
		if time.Now().After(deadline) {
			return fmt.Errorf("cogqueue: P3 request dropped — system under pressure for >%s", p3MaxPressureWait)
		}
		select {
		case <-qCtx.Done():
			return qCtx.Err()
		case <-reqCtx.Done():
			return reqCtx.Err()
		case <-time.After(backoff):
		}
		if backoff < 8*time.Second {
			backoff *= 2
		}
	}
	return nil
}

// dequeue blocks until a request is available, returning the
// highest-priority pending request.
//
// The implementation uses nested non-blocking selects so higher-priority
// channels are always drained before falling back to lower ones. This gives
// true strict priority: if both P0 and P3 have waiting requests, P0 always
// wins.
//
// Returns (req, false) only when ctx is cancelled.
func (q *CogQueue) dequeue(ctx context.Context) (cogRequest, bool) {
	// Pass 1 — non-blocking, P0 only.
	select {
	case req := <-q.p0:
		return req, true
	default:
	}
	// Pass 2 — non-blocking, P0 or P1.
	select {
	case req := <-q.p0:
		return req, true
	case req := <-q.p1:
		return req, true
	default:
	}
	// Pass 3 — non-blocking, P0, P1, or P2.
	select {
	case req := <-q.p0:
		return req, true
	case req := <-q.p1:
		return req, true
	case req := <-q.p2:
		return req, true
	default:
	}
	// Pass 4 — blocking wait on all four priorities.
	select {
	case <-ctx.Done():
		return cogRequest{}, false
	case req := <-q.p0:
		return req, true
	case req := <-q.p1:
		return req, true
	case req := <-q.p2:
		return req, true
	case req := <-q.p3:
		return req, true
	}
}
