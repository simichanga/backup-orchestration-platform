package events

import (
	"context"
	"log/slog"
)

// Multi fans an event out to every subscriber, matching the documented
// "pushed to subscribers" model (a notification system, a metrics
// pipeline, an audit trail, alongside the log). Subscribers run
// synchronously, in order; a panicking subscriber is recovered so it can't
// take down the others or the caller. There is no per-subscriber timeout
// or concurrency yet - fine while the only subscriber is LogPublisher
// (fast, local), but revisit if a slow subscriber (e.g. a network-based
// notifier) is ever added.
type Multi struct {
	Subscribers []Publisher
	Logger      *slog.Logger // optional; defaults to slog.Default(), used only to report a panic
}

func (m *Multi) Publish(ctx context.Context, e Event) {
	for _, sub := range m.Subscribers {
		m.publishOne(ctx, sub, e)
	}
}

func (m *Multi) publishOne(ctx context.Context, sub Publisher, e Event) {
	defer func() {
		if r := recover(); r != nil {
			logger := m.Logger
			if logger == nil {
				logger = slog.Default()
			}
			logger.Error("event subscriber panicked", "panic", r, "event_type", e.Type)
		}
	}()
	sub.Publish(ctx, e)
}
