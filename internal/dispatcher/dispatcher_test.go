package dispatcher_test

import (
	"testing"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/pkg/models"
)

func TestDispatchFanOut(t *testing.T) {
	d := dispatcher.New()
	ch := make(chan models.Message, 10)

	// Two subscribers with no tier filter → both receive every message.
	unsub1 := d.Subscribe("", func(m models.Message) { ch <- m })
	unsub2 := d.Subscribe("", func(m models.Message) { ch <- m })
	defer unsub1()
	defer unsub2()

	msg := models.Message{ID: "test-1", Kind: models.KindMilestone, Content: "hello", Tier: models.TierSummary}
	d.Dispatch(msg)

	var received []models.Message
	for i := 0; i < 2; i++ {
		select {
		case m := <-ch:
			received = append(received, m)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for dispatch")
		}
	}

	if len(received) != 2 {
		t.Errorf("expected 2 deliveries, got %d", len(received))
	}
	for _, m := range received {
		if m.ID != "test-1" {
			t.Errorf("unexpected message id: %s", m.ID)
		}
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	d := dispatcher.New()
	ch := make(chan models.Message, 5)

	unsub := d.Subscribe("", func(m models.Message) { ch <- m })
	unsub() // immediately unsubscribe

	d.Dispatch(models.Message{ID: "should-not-arrive", Tier: models.TierRaw})

	select {
	case <-ch:
		t.Error("received message after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// correct
	}
}

// TestTierFiltering ensures a summary-only subscriber never receives raw messages.
func TestTierFiltering(t *testing.T) {
	d := dispatcher.New()
	ch := make(chan models.Message, 5)

	d.Subscribe(models.TierSummary, func(m models.Message) { ch <- m })

	// Dispatch a raw message — should not arrive.
	d.Dispatch(models.Message{ID: "raw-1", Tier: models.TierRaw})

	// Dispatch a summary message — should arrive.
	d.Dispatch(models.Message{ID: "sum-1", Tier: models.TierSummary})

	select {
	case m := <-ch:
		if m.ID != "sum-1" {
			t.Errorf("expected sum-1, got %s", m.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for summary message")
	}

	// Ensure nothing else arrives.
	select {
	case m := <-ch:
		t.Errorf("unexpected message received: %s", m.ID)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestProjectIDTagging verifies that Dispatch stamps the active project on untagged messages.
func TestProjectIDTagging(t *testing.T) {
	d := dispatcher.New()
	d.SetProject("proj-abc")

	ch := make(chan models.Message, 2)
	d.Subscribe("", func(m models.Message) { ch <- m })

	// No project set on the message — should be tagged automatically.
	d.Dispatch(models.Message{ID: "m1", Tier: models.TierSummary})

	// Explicit project ID should NOT be overwritten.
	d.Dispatch(models.Message{ID: "m2", Tier: models.TierSummary, ProjectID: "other-proj"})

	m1 := <-ch
	m2 := <-ch

	if m1.ProjectID != "proj-abc" {
		t.Errorf("expected project tagged as proj-abc, got %q", m1.ProjectID)
	}
	if m2.ProjectID != "other-proj" {
		t.Errorf("expected project preserved as other-proj, got %q", m2.ProjectID)
	}
}

// TestDispatchSummaryHelper verifies the convenience wrapper sets the tier.
func TestDispatchSummaryHelper(t *testing.T) {
	d := dispatcher.New()
	ch := make(chan models.Message, 1)
	d.Subscribe(models.TierSummary, func(m models.Message) { ch <- m })

	d.DispatchSummary(models.Message{ID: "s1"})

	select {
	case m := <-ch:
		if m.Tier != models.TierSummary {
			t.Errorf("expected summary tier, got %q", m.Tier)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
