package events

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Notifier is the subset of *store.Store used to publish and listen for
// cross-replica events (implemented by *store.Store; faked in tests).
type Notifier interface {
	NotifyEvent(ctx context.Context, channel, payload string) error
	ListenEvents(ctx context.Context, channel string, onNotify func(payload string)) error
}

// Publisher emits events for other packages (engine, scoring) to call. It
// never broadcasts directly to the local Hub: publishing only NOTIFYs
// PostgreSQL, and the Listen loop (started once per pod, including on the
// publishing pod itself) is the single path that reaches WebSocket clients.
// This keeps delivery uniform across replicas instead of special-casing the
// origin pod.
type Publisher struct {
	notifier Notifier
}

// NewPublisher builds a Publisher backed by notifier.
func NewPublisher(notifier Notifier) *Publisher {
	return &Publisher{notifier: notifier}
}

// Publish serializes and NOTIFYs an event. Errors are logged, not returned:
// callers (e.g. the submit handler) must not fail a request because the
// real-time side channel had a hiccup.
func (p *Publisher) Publish(ctx context.Context, eventType string, payload any) {
	data, err := marshal(eventType, payload)
	if err != nil {
		slog.Error("marshal event failed", "type", eventType, "err", err)
		return
	}
	if err := p.notifier.NotifyEvent(ctx, Channel, string(data)); err != nil {
		slog.Error("publish event failed", "type", eventType, "err", err)
	}
}

// Listen runs the cross-replica bridge: it blocks, listening on Channel and
// rebroadcasting every payload to hub, until ctx is done. On a lost
// connection it retries with a fixed backoff instead of giving up — a
// permanently silent event feed is worse than a noisy log.
func Listen(ctx context.Context, notifier Notifier, hub *Hub) {
	const retryDelay = 2 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := notifier.ListenEvents(ctx, Channel, func(payload string) {
			hub.BroadcastRaw([]byte(payload))
		})
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			slog.Error("event listen loop failed, retrying", "err", fmt.Errorf("listen %s: %w", Channel, err))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryDelay):
		}
	}
}
