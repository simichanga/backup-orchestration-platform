package metadata

import (
	"context"
	"testing"
	"time"

	"bop/internal/events"
)

// TestPruneEventsSubSecondBoundary guards against a specific
// modernc.org/sqlite gotcha: the events.timestamp column is DATETIME,
// stored as text with trailing zeros trimmed from the fractional-second
// part (750ms stores as ".75", not ".750") - a variable-width text
// representation. Naive byte-wise (memcmp) comparison of variable-width
// text would stop matching chronological order at sub-second boundaries
// (".75" sorts after ".754" because 'Z' > '4'). Verified directly (see
// scratchpad investigation, not kept in-repo) that modernc's driver does
// NOT fall into this trap - WHERE/ORDER BY on this column compare
// chronologically correctly even in the worst case (a whole-second
// timestamp with its fraction trimmed to nothing, compared against one
// half a millisecond later). This test is the permanent regression guard
// for that finding, in case a future driver/version change reintroduces
// the naive-comparison behavior.
func TestPruneEventsSubSecondBoundary(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	base := time.Date(2026, 1, 1, 0, 0, 25, 750000000, time.UTC)   // .75 once trimmed
	newer := time.Date(2026, 1, 1, 0, 0, 25, 754000000, time.UTC)  // .754
	cutoff := time.Date(2026, 1, 1, 0, 0, 25, 752000000, time.UTC) // .752, strictly between

	if err := s.RecordEvent(ctx, events.Event{Type: events.TypeBackupStarted, Host: "h", Timestamp: base}); err != nil {
		t.Fatalf("RecordEvent(base): %v", err)
	}
	if err := s.RecordEvent(ctx, events.Event{Type: events.TypeBackupCompleted, Host: "h", Timestamp: newer}); err != nil {
		t.Fatalf("RecordEvent(newer): %v", err)
	}

	n, err := s.PruneEventsOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneEventsOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("PruneEventsOlderThan removed %d rows, want exactly 1 (base) - sub-second ordering regression", n)
	}

	got, err := s.ListEvents(ctx)
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(got) != 1 || !got[0].Timestamp.Equal(newer) {
		t.Errorf("ListEvents after prune = %+v, want just newer (%v)", got, newer)
	}
}

// TestPruneEventsWholeSecondVsFractional is the worst case for the same
// gotcha: a timestamp with NO fractional part at all (trimmed to zero
// digits, e.g. ".0" -> "") compared against one only half a millisecond
// later that DOES have a fraction. Naive memcmp would rank the
// no-fraction (chronologically earlier) timestamp last, since 'Z' (end of
// string) sorts after '.' (start of a fraction).
func TestPruneEventsWholeSecondVsFractional(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	earlier := time.Date(2026, 1, 1, 0, 0, 25, 0, time.UTC)       // whole second, no fraction
	later := time.Date(2026, 1, 1, 0, 0, 25, 1_000_000, time.UTC) // +1ms
	cutoff := time.Date(2026, 1, 1, 0, 0, 25, 500_000, time.UTC)  // +0.5ms, strictly between

	if err := s.RecordEvent(ctx, events.Event{Type: events.TypeBackupStarted, Host: "h", Timestamp: earlier}); err != nil {
		t.Fatalf("RecordEvent(earlier): %v", err)
	}
	if err := s.RecordEvent(ctx, events.Event{Type: events.TypeBackupCompleted, Host: "h", Timestamp: later}); err != nil {
		t.Fatalf("RecordEvent(later): %v", err)
	}

	n, err := s.PruneEventsOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatalf("PruneEventsOlderThan: %v", err)
	}
	if n != 1 {
		t.Errorf("PruneEventsOlderThan removed %d rows, want exactly 1 (earlier) - sub-second ordering regression", n)
	}
}
