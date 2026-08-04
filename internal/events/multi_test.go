package events

import (
	"context"
	"testing"
)

type recordingSubscriber struct {
	received []Event
}

func (r *recordingSubscriber) Publish(_ context.Context, e Event) {
	r.received = append(r.received, e)
}

type panickingSubscriber struct{}

func (panickingSubscriber) Publish(context.Context, Event) {
	panic("subscriber exploded")
}

func TestMultiFansOutToAllSubscribers(t *testing.T) {
	a := &recordingSubscriber{}
	b := &recordingSubscriber{}
	m := &Multi{Subscribers: []Publisher{a, b}}

	e := Event{Type: TypeBackupStarted, JobID: "job-1"}
	m.Publish(context.Background(), e)

	if len(a.received) != 1 || a.received[0].JobID != "job-1" {
		t.Errorf("subscriber a received %+v, want one event for job-1", a.received)
	}
	if len(b.received) != 1 || b.received[0].JobID != "job-1" {
		t.Errorf("subscriber b received %+v, want one event for job-1", b.received)
	}
}

func TestMultiRecoversPanickingSubscriberAndContinues(t *testing.T) {
	after := &recordingSubscriber{}
	m := &Multi{Subscribers: []Publisher{panickingSubscriber{}, after}}

	// Must not panic itself, and the subscriber after the panicking one
	// must still receive the event.
	m.Publish(context.Background(), Event{Type: TypeBackupStarted, JobID: "job-1"})

	if len(after.received) != 1 {
		t.Errorf("subscriber after the panicking one received %d events, want 1", len(after.received))
	}
}
