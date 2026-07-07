package events

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

// fakeNotifier is an in-process stand-in for *store.Store: NotifyEvent
// stores the payload, and Listen (started by the test) replays it via
// onNotify — simulating what a real PostgreSQL LISTEN/NOTIFY round trip
// does, without a database.
type fakeNotifier struct {
	mu       sync.Mutex
	notified []string
	onNotify func(string)
}

func (f *fakeNotifier) NotifyEvent(_ context.Context, _ string, payload string) error {
	f.mu.Lock()
	f.notified = append(f.notified, payload)
	onNotify := f.onNotify
	f.mu.Unlock()
	if onNotify != nil {
		onNotify(payload)
	}
	return nil
}

func (f *fakeNotifier) ListenEvents(ctx context.Context, _ string, onNotify func(string)) error {
	f.mu.Lock()
	f.onNotify = onNotify
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func TestPublisherPublishNotifiesAndListenBroadcasts(t *testing.T) {
	notifier := &fakeNotifier{}
	hub := NewHub()
	client := hub.Register()
	defer hub.Unregister(client)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Listen(ctx, notifier, hub)

	// Give the Listen goroutine a moment to register its onNotify callback.
	deadline := time.After(2 * time.Second)
	for {
		notifier.mu.Lock()
		ready := notifier.onNotify != nil
		notifier.mu.Unlock()
		if ready {
			break
		}
		select {
		case <-deadline:
			t.Fatal("Listen never called ListenEvents")
		case <-time.After(time.Millisecond):
		}
	}

	pub := NewPublisher(notifier)
	pub.Publish(context.Background(), TypeChallengeSolved, map[string]string{"challenge": "web"})

	select {
	case msg := <-client.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("decode event: %v", err)
		}
		if ev.Type != TypeChallengeSolved {
			t.Errorf("type = %q, want %q", ev.Type, TypeChallengeSolved)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("client never received the broadcast event")
	}
}
