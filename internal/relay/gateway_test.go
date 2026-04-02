package relay_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/internal/relay"
	"github.com/haepapa/kotui/pkg/models"
)

// mockRelay captures sent messages for assertions.
type mockRelay struct {
	name     string
	messages []models.Message
	sendErr  error
}

func (m *mockRelay) Name() string { return m.name }
func (m *mockRelay) Send(_ context.Context, msg models.Message) error {
	m.messages = append(m.messages, msg)
	return m.sendErr
}

func newTestGateway(t *testing.T) (*relay.Gateway, *dispatcher.Dispatcher) {
	t.Helper()
	disp := dispatcher.New()
	gw := relay.New(disp, slog.Default())
	t.Cleanup(gw.Close)
	return gw, disp
}

func dispatch(disp *dispatcher.Dispatcher, content string) {
	disp.Dispatch(models.Message{
		Tier:    models.TierSummary,
		AgentID: "test-agent",
		Content: content,
	})
}

// TestGateway_ForwardsToRelay verifies that a registered relay receives messages.
func TestGateway_ForwardsToRelay(t *testing.T) {
	gw, disp := newTestGateway(t)

	r := &mockRelay{name: "test"}
	gw.Register(r)

	dispatch(disp, "hello from agent")

	// Give goroutines a moment (Dispatcher is synchronous so none needed, but be safe).
	time.Sleep(10 * time.Millisecond)

	if len(r.messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(r.messages))
	}
	if r.messages[0].Content != "hello from agent" {
		t.Errorf("unexpected content: %q", r.messages[0].Content)
	}
}

// TestGateway_MultipleRelays verifies all registered relays receive each message.
func TestGateway_MultipleRelays(t *testing.T) {
	gw, disp := newTestGateway(t)

	r1 := &mockRelay{name: "relay-1"}
	r2 := &mockRelay{name: "relay-2"}
	gw.Register(r1)
	gw.Register(r2)

	dispatch(disp, "broadcast")

	if len(r1.messages) != 1 {
		t.Errorf("relay-1: expected 1 message, got %d", len(r1.messages))
	}
	if len(r2.messages) != 1 {
		t.Errorf("relay-2: expected 1 message, got %d", len(r2.messages))
	}
}

// TestGateway_NoRelays verifies that zero-relay mode does not panic or error.
func TestGateway_NoRelays(t *testing.T) {
	_, disp := newTestGateway(t)
	// Dispatch without any relays — should not panic.
	dispatch(disp, "no one listening")
}

// TestGateway_SendErrorDoesNotCrash verifies that a relay that always errors
// does not prevent subsequent relays from receiving the message.
func TestGateway_SendErrorDoesNotCrash(t *testing.T) {
	gw, disp := newTestGateway(t)

	failing := &mockRelay{name: "failing", sendErr: errors.New("network down")}
	succeeding := &mockRelay{name: "succeeding"}
	gw.Register(failing)
	gw.Register(succeeding)

	dispatch(disp, "resilience test")

	if len(succeeding.messages) != 1 {
		t.Errorf("succeeding relay should still receive messages; got %d", len(succeeding.messages))
	}
}

// TestGateway_Close unsubscribes the gateway; subsequent dispatches are not forwarded.
func TestGateway_Close(t *testing.T) {
	disp := dispatcher.New()
	gw := relay.New(disp, slog.Default())

	r := &mockRelay{name: "after-close"}
	gw.Register(r)

	gw.Close()

	dispatch(disp, "should not arrive")

	if len(r.messages) != 0 {
		t.Errorf("expected 0 messages after Close, got %d", len(r.messages))
	}
}

// TestGateway_CloseIdempotent verifies calling Close twice does not panic.
func TestGateway_CloseIdempotent(t *testing.T) {
	_, disp := newTestGateway(t)
	gw := relay.New(disp, slog.Default())
	gw.Close()
	gw.Close() // must not panic
}

// TestGateway_RelayCount verifies the count accessor.
func TestGateway_RelayCount(t *testing.T) {
	gw, _ := newTestGateway(t)
	if gw.RelayCount() != 0 {
		t.Errorf("expected 0 relays initially, got %d", gw.RelayCount())
	}
	gw.Register(&mockRelay{name: "a"})
	gw.Register(&mockRelay{name: "b"})
	if gw.RelayCount() != 2 {
		t.Errorf("expected 2 relays, got %d", gw.RelayCount())
	}
}
