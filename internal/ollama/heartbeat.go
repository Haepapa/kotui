package ollama

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// EmergencyFunc is called by the Monitor when Ollama becomes unresponsive.
type EmergencyFunc func(msg string)

// Monitor polls Ollama's health endpoint on a fixed interval and fires an
// EmergencyFunc when the engine becomes unresponsive.
type Monitor struct {
	client    *Client
	interval  time.Duration
	onEmerg   EmergencyFunc
	mu        sync.Mutex
	running   bool
	cancel    context.CancelFunc
	done      chan struct{}
}

// NewMonitor creates a Monitor for the given client.
// interval is how often to poll (default 10s if zero).
// onEmergency is called when Ollama is unreachable; may be nil.
func NewMonitor(c *Client, interval time.Duration, onEmergency EmergencyFunc) *Monitor {
	if interval <= 0 {
		interval = 10 * time.Second
	}
	if onEmergency == nil {
		onEmergency = func(msg string) {
			slog.Error("ollama heartbeat: emergency", "msg", msg)
		}
	}
	return &Monitor{
		client:   c,
		interval: interval,
		onEmerg:  onEmergency,
	}
}

// Start begins the polling goroutine. Safe to call multiple times (idempotent).
func (m *Monitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.done = make(chan struct{})
	m.running = true
	go m.loop(ctx)
}

// Stop halts the polling goroutine and waits for it to exit.
func (m *Monitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	cancel := m.cancel
	done := m.done
	m.running = false
	m.mu.Unlock()

	cancel()
	<-done
}

func (m *Monitor) loop(ctx context.Context) {
	defer close(m.done)
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	healthy := true
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ok := m.client.IsHealthy(ctx)
			if !ok && healthy {
				healthy = false
				m.onEmerg("Ollama is not responding — check that `ollama serve` is running and that the engine has not OOM'd")
			} else if ok && !healthy {
				healthy = true
				slog.Info("ollama heartbeat: engine recovered")
			}
		}
	}
}
