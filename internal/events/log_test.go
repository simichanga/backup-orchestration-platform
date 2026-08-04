package events

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestLogPublisherWritesStructuredRecord(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	p := &LogPublisher{Logger: logger}

	p.Publish(context.Background(), Event{
		Type:     TypeArtifactCreated,
		JobID:    "job-1",
		Host:     "prod-db",
		Plugin:   "postgres",
		Resource: "myapp",
		Fields:   map[string]string{"size": "45"},
	})

	var record map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("unmarshal log record: %v (raw: %s)", err, buf.String())
	}

	for key, want := range map[string]string{
		"event_type": "ArtifactCreated",
		"job_id":     "job-1",
		"host":       "prod-db",
		"plugin":     "postgres",
		"resource":   "myapp",
		"size":       "45",
	} {
		got, _ := record[key].(string)
		if got != want {
			t.Errorf("record[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestLogPublisherDefaultsToSlogDefault(t *testing.T) {
	// Must not panic when Logger is unset.
	p := &LogPublisher{}
	p.Publish(context.Background(), Event{Type: TypeBackupStarted})
}

func TestLogPublisherOmitsEmptyResource(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	p := &LogPublisher{Logger: logger}

	p.Publish(context.Background(), Event{Type: TypeBackupStarted, JobID: "job-1"})

	if strings.Contains(buf.String(), "resource=") {
		t.Errorf("log record = %q, should not include a resource key for a job-level event", buf.String())
	}
}
