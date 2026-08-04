package metrics

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"bop/internal/events"
)

func TestServerServesRegisteredMetricsOnConfiguredPath(t *testing.T) {
	reg := prometheus.NewRegistry()
	p := New(reg)
	p.Publish(context.Background(), events.Event{Type: events.TypeRetentionApplied, Host: "prod-db"})

	s, err := NewServer("127.0.0.1:0", "/metrics", reg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	errCh := s.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	})

	resp, err := http.Get("http://" + s.Addr() + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if !strings.Contains(string(body), `bop_retention_applied_total{host="prod-db"} 1`) {
		t.Errorf("metrics output missing expected series, got:\n%s", body)
	}

	select {
	case err := <-errCh:
		t.Fatalf("server exited early with %v", err)
	default:
	}
}

func TestServerReturns404OffConfiguredPath(t *testing.T) {
	reg := prometheus.NewRegistry()
	New(reg)

	s, err := NewServer("127.0.0.1:0", "/metrics", reg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	s.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.Shutdown(ctx)
	})

	resp, err := http.Get("http://" + s.Addr() + "/other")
	if err != nil {
		t.Fatalf("GET /other: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (only the configured path should be served)", resp.StatusCode)
	}
}

func TestNewServerFailsFastOnPortInUse(t *testing.T) {
	reg := prometheus.NewRegistry()
	s1, err := NewServer("127.0.0.1:0", "/metrics", reg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s1.Shutdown(ctx)
	}()

	_, err = NewServer(s1.Addr(), "/metrics", reg)
	if err == nil {
		t.Fatal("NewServer: expected an error binding an already-bound address, got nil")
	}
}

func TestServerShutdownStopsAcceptingConnections(t *testing.T) {
	reg := prometheus.NewRegistry()
	s, err := NewServer("127.0.0.1:0", "/metrics", reg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	errCh := s.Start()
	addr := s.Addr()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			t.Errorf("Serve returned %v, want http.ErrServerClosed", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Start's error channel never received a value after Shutdown")
	}

	if _, err := http.Get("http://" + addr + "/metrics"); err == nil {
		t.Errorf("GET after Shutdown succeeded, want a connection error")
	}
}
