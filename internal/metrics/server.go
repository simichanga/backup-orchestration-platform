package metrics

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Server serves Prometheus-format metrics over HTTP, per config.yaml's
// metrics.port/metrics.path.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

func handler(path string, reg *prometheus.Registry) http.Handler {
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	return mux
}

// NewServer binds addr immediately, rather than deferring the bind to
// Start: a port already in use is then a "bop controller" startup error,
// not a silent failure discovered later by whoever tries to scrape a dead
// endpoint.
func NewServer(addr, path string, reg *prometheus.Registry) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("metrics: listen on %s: %w", addr, err)
	}
	return &Server{
		httpServer: &http.Server{Handler: handler(path, reg)},
		listener:   ln,
	}, nil
}

// Addr is the actual bound address - useful when addr's port was 0.
func (s *Server) Addr() string {
	return s.listener.Addr().String()
}

// Start serves in the background and returns a channel that receives
// Serve's terminal error. A clean Shutdown makes Serve return
// http.ErrServerClosed - callers should not treat that specific error as a
// failure.
func (s *Server) Start() <-chan error {
	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpServer.Serve(s.listener)
	}()
	return errCh
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
