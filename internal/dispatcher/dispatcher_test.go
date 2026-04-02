package dispatcher_test

import (
	"testing"
	"time"

	"github.com/haepapa/kotui/internal/dispatcher"
	"github.com/haepapa/kotui/pkg/models"
)

func TestDispatchFanOut(t *testing.T) {
	d := dispatcher.New()

	received := make([]dispatcher.Envelope, 0, 2)
	var mu = make(chan dispatcher.Envelope, 10)

	unsub1 := d.Subscribe(func(e dispatcher.Envelope) { mu <- e })
	unsub2 := d.Subscribe(func(e dispatcher.Envelope) { mu <- e })
	defer unsub1()
	defer unsub2()

	msg := models.Message{ID: "test-1", Kind: models.KindMilestone, Content: "hello"}
	d.Dispatch(dispatcher.Envelope{Message: msg})

	for i := 0; i < 2; i++ {
		select {
		case e := <-mu:
			received = append(received, e)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for dispatch")
		}
	}

	if len(received) != 2 {
		t.Errorf("expected 2 deliveries, got %d", len(received))
	}
	for _, e := range received {
		if e.Message.ID != "test-1" {
			t.Errorf("unexpected message id: %s", e.Message.ID)
		}
	}
}

func TestUnsubscribeStopsDelivery(t *testing.T) {
	d := dispatcher.New()
	ch := make(chan dispatcher.Envelope, 5)

	unsub := d.Subscribe(func(e dispatcher.Envelope) { ch <- e })
	unsub() // immediately unsubscribe

	d.Dispatch(dispatcher.Envelope{Message: models.Message{ID: "should-not-arrive"}})

	select {
	case <-ch:
		t.Error("received message after unsubscribe")
	case <-time.After(50 * time.Millisecond):
		// correct: nothing received
	}
}
