package metadata

import (
	"context"
	"testing"
	"time"

	"bop/internal/events"
)

func TestRecordAndListEvents(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	evts := []events.Event{
		{Type: events.TypeBackupStarted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: now},
		{
			Type: events.TypeArtifactCreated, JobID: "job-1", Host: "prod-db", Plugin: "postgres",
			Resource: "myapp", Fields: map[string]string{"checksum": "sha256:aaa", "size": "1024"},
			Timestamp: now.Add(time.Second),
		},
	}
	for _, e := range evts {
		if err := s.RecordEvent(ctx, e); err != nil {
			t.Fatalf("RecordEvent(%s): %v", e.Type, err)
		}
	}

	got, err := s.ListEvents(ctx)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListEvents returned %d events, want 2", len(got))
	}
	// Most recent first.
	if got[0].Type != events.TypeArtifactCreated || got[1].Type != events.TypeBackupStarted {
		t.Errorf("ListEvents order = [%s, %s], want [%s, %s]", got[0].Type, got[1].Type, events.TypeArtifactCreated, events.TypeBackupStarted)
	}
	if got[0].Fields["checksum"] != "sha256:aaa" || got[0].Fields["size"] != "1024" {
		t.Errorf("Fields = %+v, want checksum/size round-tripped", got[0].Fields)
	}
	if got[0].Resource != "myapp" {
		t.Errorf("Resource = %q, want myapp", got[0].Resource)
	}
	if !got[1].Timestamp.Equal(now) {
		t.Errorf("Timestamp = %v, want %v", got[1].Timestamp, now)
	}
}

func TestRecordEventWithNilFields(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	e := events.Event{Type: events.TypeRetentionApplied, Host: "prod-db", Timestamp: time.Now().UTC()}
	if err := s.RecordEvent(ctx, e); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	got, err := s.ListEvents(ctx)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if len(got[0].Fields) != 0 {
		t.Errorf("Fields = %+v, want empty", got[0].Fields)
	}
}

func TestListEventsEmpty(t *testing.T) {
	s := openTestStore(t)
	got, err := s.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListEvents returned %d events, want 0", len(got))
	}
}

func TestPruneEventsOlderThan(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	old := events.Event{Type: events.TypeBackupCompleted, Host: "prod-db", Timestamp: now.Add(-48 * time.Hour)}
	recent := events.Event{Type: events.TypeBackupCompleted, Host: "prod-db", Timestamp: now}
	for _, e := range []events.Event{old, recent} {
		if err := s.RecordEvent(ctx, e); err != nil {
			t.Fatalf("RecordEvent: %v", err)
		}
	}

	n, err := s.PruneEventsOlderThan(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneEventsOlderThan: %v", err)
	}
	if n != 1 {
		t.Fatalf("PruneEventsOlderThan removed %d rows, want 1", n)
	}

	got, err := s.ListEvents(ctx)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || !got[0].Timestamp.Equal(now) {
		t.Fatalf("ListEvents after prune = %+v, want just the recent event", got)
	}
}

func TestPruneEventsOlderThanNothingToDo(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	if err := s.RecordEvent(ctx, events.Event{Type: events.TypeBackupCompleted, Host: "prod-db", Timestamp: now}); err != nil {
		t.Fatalf("RecordEvent: %v", err)
	}

	n, err := s.PruneEventsOlderThan(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PruneEventsOlderThan: %v", err)
	}
	if n != 0 {
		t.Errorf("PruneEventsOlderThan removed %d rows, want 0", n)
	}
}

func TestEventPublisherPersistsEvent(t *testing.T) {
	s := openTestStore(t)
	p := &EventPublisher{Store: s}

	e := events.Event{Type: events.TypeBackupStarted, JobID: "job-1", Host: "prod-db", Plugin: "postgres", Timestamp: time.Now().UTC()}
	p.Publish(context.Background(), e)

	got, err := s.ListEvents(context.Background())
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || got[0].JobID != "job-1" {
		t.Fatalf("ListEvents = %+v, want the published event persisted", got)
	}
}

func TestEventPublisherSurvivesStoreFailure(t *testing.T) {
	s := openTestStore(t)
	s.Close() // force every subsequent write to fail

	p := &EventPublisher{Store: s}
	// Publish must not panic even though the underlying store is closed -
	// events.Publisher's contract is that emitting an event can never fail
	// a backup job, so a persistence failure has nowhere to go but a log.
	p.Publish(context.Background(), events.Event{Type: events.TypeBackupStarted, Host: "prod-db", Timestamp: time.Now().UTC()})
}
