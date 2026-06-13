package events

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestHubSubscribeReceivesPublishedEvents(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch, unsubscribe := hub.Subscribe(42)
	defer unsubscribe()

	event := RunEvent{RunID: 42, Kind: KindStepValidation, Status: "running", At: "2026-06-13T00:00:00Z"}
	hub.Publish(event)

	select {
	case got := <-ch:
		if got.RunID != event.RunID || got.Kind != event.Kind || got.Status != event.Status {
			t.Fatalf("unexpected event: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestHubIgnoresOtherRuns(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch, unsubscribe := hub.Subscribe(42)
	defer unsubscribe()

	hub.Publish(RunEvent{RunID: 7, Kind: KindStepAudit, Status: "running", At: "2026-06-13T00:00:00Z"})

	select {
	case got := <-ch:
		t.Fatalf("unexpected event for other run: %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestHubUnsubscribeClosesChannel(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch, unsubscribe := hub.Subscribe(42)
	unsubscribe()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for closed channel")
	}
}

func TestHubPublishDropsWhenSubscriberFull(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch, unsubscribe := hub.Subscribe(42)
	defer unsubscribe()

	for i := 0; i < 16; i++ {
		hub.Publish(RunEvent{RunID: 42, Kind: KindStepAgent, Status: "running", At: "2026-06-13T00:00:00Z"})
	}

	select {
	case <-ch:
	default:
		t.Fatal("expected at least one buffered event")
	}
}

func TestHubMultipleSubscribersReceiveSameEvent(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	ch1, unsubscribe1 := hub.Subscribe(42)
	defer unsubscribe1()
	ch2, unsubscribe2 := hub.Subscribe(42)
	defer unsubscribe2()

	event := RunEvent{RunID: 42, Kind: KindStepCommit, Status: "running", At: "2026-06-13T00:00:00Z"}
	hub.Publish(event)

	for i, ch := range []<-chan RunEvent{ch1, ch2} {
		select {
		case got := <-ch:
			if got.Kind != event.Kind || got.RunID != event.RunID {
				t.Fatalf("subscriber %d got unexpected event: %+v", i, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for subscriber %d", i)
		}
	}
}
