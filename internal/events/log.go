package events

import (
	"context"
	"log/slog"
)

// LogPublisher writes each event as a structured slog record - the
// "written to a log" half of the documented Event System, and the
// zero-config default every Controller falls back to.
type LogPublisher struct {
	Logger *slog.Logger // optional; defaults to slog.Default()
}

func (p *LogPublisher) Publish(_ context.Context, e Event) {
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}

	args := []any{
		"event_type", string(e.Type),
		"job_id", e.JobID,
		"host", e.Host,
		"plugin", e.Plugin,
	}
	if e.Resource != "" {
		args = append(args, "resource", e.Resource)
	}
	for k, v := range e.Fields {
		args = append(args, k, v)
	}

	logger.Info("event", args...)
}
