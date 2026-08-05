package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"bop/internal/events"
)

// RecordEvent persists an event. Fields (its event-specific key/value
// payload - size, checksum, snapshot id, error, ...) is stored as a JSON
// blob: SQLite has no native map type, and which keys are present varies
// by event Type (see events.Event's doc comment), so a fixed column
// layout doesn't fit.
func (s *Store) RecordEvent(ctx context.Context, e events.Event) error {
	fields, err := json.Marshal(e.Fields)
	if err != nil {
		return fmt.Errorf("metadata: marshal event fields: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO events (type, job_id, host, plugin, resource, fields, timestamp)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.Type, e.JobID, e.Host, e.Plugin, e.Resource, string(fields), e.Timestamp,
	)
	if err != nil {
		return fmt.Errorf("metadata: record event: %w", err)
	}
	return nil
}

// EventFilter narrows ListEventsPage's results. A zero-value JobID/Host
// means "no filter" on that field. Limit <= 0 means no limit, using
// SQLite's own `LIMIT -1` convention rather than a magic sentinel BOP
// invents itself.
type EventFilter struct {
	JobID string
	Host  string
	Limit int
}

// ListEvents returns every persisted event, most recent first - a
// convenience wrapper around ListEventsPage with no filter and no limit.
// Part of the storage layer's read/write symmetry (every other table here
// has both a record and a list method).
func (s *Store) ListEvents(ctx context.Context) ([]events.Event, error) {
	return s.ListEventsPage(ctx, EventFilter{})
}

// ListEventsPage returns events matching filter, most recent first. Backs
// GET /v1/events (internal/api) - the API layer is responsible for
// choosing a sane default/max Limit; this method applies whatever it's
// given as-is.
func (s *Store) ListEventsPage(ctx context.Context, filter EventFilter) ([]events.Event, error) {
	query := `SELECT type, job_id, host, plugin, resource, fields, timestamp FROM events WHERE 1=1`
	var args []interface{}
	if filter.JobID != "" {
		query += " AND job_id = ?"
		args = append(args, filter.JobID)
	}
	if filter.Host != "" {
		query += " AND host = ?"
		args = append(args, filter.Host)
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = -1
	}
	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("metadata: list events: %w", err)
	}
	defer rows.Close()

	var result []events.Event
	for rows.Next() {
		var e events.Event
		var fieldsJSON string
		if err := rows.Scan(&e.Type, &e.JobID, &e.Host, &e.Plugin, &e.Resource, &fieldsJSON, &e.Timestamp); err != nil {
			return nil, fmt.Errorf("metadata: scan event: %w", err)
		}
		if err := json.Unmarshal([]byte(fieldsJSON), &e.Fields); err != nil {
			return nil, fmt.Errorf("metadata: unmarshal event fields: %w", err)
		}
		result = append(result, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("metadata: list events: %w", err)
	}
	return result, nil
}

// PruneEventsOlderThan deletes every event recorded before cutoff and
// returns how many rows were removed. Called on controller startup and
// then periodically (see internal/cli's event pruner) so the events table
// doesn't grow unbounded across a long-running controller's uptime.
func (s *Store) PruneEventsOlderThan(ctx context.Context, cutoff time.Time) (int, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM events WHERE timestamp < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("metadata: prune events: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("metadata: prune events: %w", err)
	}
	return int(n), nil
}

// EventPublisher adapts Store to events.Publisher, persisting every event
// it receives - this is what actually closes the "no durable event store"
// gap, wired alongside LogPublisher/metrics.Publisher in the same
// events.Multi fan-out (see internal/cli/wiring.go).
type EventPublisher struct {
	Store  *Store
	Logger *slog.Logger // optional; defaults to slog.Default()
}

// Publish cannot return an error (see events.Publisher's doc comment:
// emitting an event must never be able to fail a backup job), so a write
// failure is logged, not propagated - the same "never fail the pipeline"
// contract every other Publisher implementation honors.
func (p *EventPublisher) Publish(ctx context.Context, e events.Event) {
	logger := p.Logger
	if logger == nil {
		logger = slog.Default()
	}
	if err := p.Store.RecordEvent(ctx, e); err != nil {
		logger.Error("metadata: failed to persist event", "event_type", e.Type, "error", err)
	}
}
