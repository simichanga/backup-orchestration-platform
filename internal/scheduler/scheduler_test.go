package scheduler

import (
	"context"
	"testing"
	"time"

	"bop/internal/core"
	"bop/internal/events"
	"bop/internal/inventory"
	"bop/internal/metadata"
	"bop/internal/queue"
)

func testInventory(schedule string) *inventory.Inventory {
	return &inventory.Inventory{
		Servers: map[string]inventory.Server{
			"prod-db": {
				Host:     "192.168.1.100",
				Schedule: schedule,
				Plugins: map[string]*inventory.PluginConfig{
					"postgres":   nil,
					"filesystem": nil,
				},
				Retention: core.Policy{Daily: 7},
			},
		},
	}
}

func openTestStore(t *testing.T) *metadata.Store {
	t.Helper()
	s, err := metadata.Open(":memory:")
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// recordingPublisher records every event's type in emission order.
type recordingPublisher struct {
	types []events.Type
}

func (r *recordingPublisher) Publish(_ context.Context, e events.Event) {
	r.types = append(r.types, e.Type)
}

// panickingPublisher is an events.Publisher test double that always panics,
// to prove a broken subscriber can never crash the scheduler's goroutine.
type panickingPublisher struct{}

func (panickingPublisher) Publish(context.Context, events.Event) { panic("subscriber exploded") }

func TestNewRejectsInvalidSchedule(t *testing.T) {
	inv := testInventory("not-a-cron-expr")
	md := openTestStore(t)
	q := queue.NewMemory(10)

	_, err := New(inv, md, q, nil, "", nil)
	if err == nil {
		t.Fatal("New: expected an error for an invalid schedule, got nil")
	}
}

func TestNewSkipsServersWithNoSchedule(t *testing.T) {
	inv := testInventory("")
	md := openTestStore(t)
	q := queue.NewMemory(10)

	s, err := New(inv, md, q, nil, "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(s.cron.Entries()) != 0 {
		t.Errorf("cron entries = %d, want 0 (server has no schedule, so it stays manual-only)", len(s.cron.Entries()))
	}
}

func TestNewRejectsInvalidCronLocation(t *testing.T) {
	inv := testInventory("")
	md := openTestStore(t)
	q := queue.NewMemory(10)

	_, err := New(inv, md, q, nil, "Not/A_Real_Zone", nil)
	if err == nil {
		t.Fatal("New: expected an error for an invalid scheduler.cron_location, got nil")
	}
}

func TestDispatchCreatesOneJobPerPluginAndEnqueues(t *testing.T) {
	inv := testInventory("0 3 * * *")
	md := openTestStore(t)
	q := queue.NewMemory(10)
	pub := &recordingPublisher{}

	s, err := New(inv, md, q, pub, "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.dispatch("prod-db", inv.Servers["prod-db"])

	if q.Len() != 2 {
		t.Fatalf("queue length = %d, want 2 (one job per plugin on the server)", q.Len())
	}

	queued, err := md.ListJobsByStatus(context.Background(), core.JobStatusQueued)
	if err != nil {
		t.Fatalf("ListJobsByStatus: %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("queued jobs = %d, want 2", len(queued))
	}
	plugins := map[string]bool{}
	for _, j := range queued {
		if j.Host != "prod-db" {
			t.Errorf("job.Host = %q, want prod-db", j.Host)
		}
		if j.Policy.Daily != 7 {
			t.Errorf("job.Policy.Daily = %d, want 7 (from the server's inventory retention)", j.Policy.Daily)
		}
		plugins[j.Plugin] = true
	}
	if !plugins["postgres"] || !plugins["filesystem"] {
		t.Errorf("dispatched plugins = %v, want both postgres and filesystem", plugins)
	}

	if len(pub.types) != 2 {
		t.Fatalf("published %d events, want 2 (one BackupRequested per plugin)", len(pub.types))
	}
	for _, ty := range pub.types {
		if ty != events.TypeBackupRequested {
			t.Errorf("event type = %s, want BackupRequested", ty)
		}
	}
}

func TestDispatchLeavesJobQueuedWhenEnqueueFails(t *testing.T) {
	inv := testInventory("0 3 * * *")
	md := openTestStore(t)
	q := queue.NewMemory(0) // zero capacity: Enqueue always fails with ErrQueueFull

	s, err := New(inv, md, q, nil, "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.dispatch("prod-db", inv.Servers["prod-db"])

	queued, err := md.ListJobsByStatus(context.Background(), core.JobStatusQueued)
	if err != nil {
		t.Fatalf("ListJobsByStatus: %v", err)
	}
	if len(queued) != 2 {
		t.Fatalf("queued jobs = %d, want 2 (persisted even though Enqueue failed - recoverable via startup reconciliation)", len(queued))
	}
}

func TestDispatchSurvivesPanickingEventPublisher(t *testing.T) {
	inv := testInventory("0 3 * * *")
	md := openTestStore(t)
	q := queue.NewMemory(10)

	s, err := New(inv, md, q, panickingPublisher{}, "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	s.dispatch("prod-db", inv.Servers["prod-db"]) // must not panic

	if q.Len() != 2 {
		t.Errorf("queue length = %d, want 2 (dispatch must complete despite a panicking publisher)", q.Len())
	}
}

// TestStartFiresScheduledDispatch is a real end-to-end test of the cron
// timer itself, not just dispatch's logic: it proves AddFunc's closure
// actually fires on schedule and reaches the queue, not just that dispatch
// works when called directly.
func TestStartFiresScheduledDispatch(t *testing.T) {
	inv := testInventory("@every 30ms")
	md := openTestStore(t)
	q := queue.NewMemory(10)

	s, err := New(inv, md, q, nil, "", nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.Start()
	defer s.Stop()

	deadline := time.After(2 * time.Second)
	for q.Len() < 2 {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for scheduled dispatch to fire; queue length = %d", q.Len())
		case <-time.After(10 * time.Millisecond):
		}
	}
}
