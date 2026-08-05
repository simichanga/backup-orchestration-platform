// Package api implements BOP's optional read-only HTTP API
// (docs/02-architecture.md's "API" component, v1 scope: REST only, no
// mutating endpoints yet - see config.APIConfig's doc comment for why).
// Every request requires a bearer token; there is no anonymous access.
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"bop/internal/inventory"
	"bop/internal/metadata"
)

// Server serves BOP's read-only HTTP API, per config.yaml's api.* section.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

// NewServer binds addr immediately, rather than deferring the bind to
// Start, matching metrics.NewServer: a port already in use is then a
// "bop controller" startup error, not a silently dead endpoint discovered
// later by whoever tries to call it.
func NewServer(addr string, tokens []string, inv *inventory.Inventory, md *metadata.Store) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("api: listen on %s: %w", addr, err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/hosts", listHostsHandler(inv))
	mux.HandleFunc("GET /v1/jobs", listJobsHandler(md))
	mux.HandleFunc("GET /v1/jobs/{id}", getJobHandler(md))
	mux.HandleFunc("GET /v1/snapshots", listSnapshotsHandler(md))

	return &Server{
		httpServer: &http.Server{Handler: authMiddleware(hashTokens(tokens), mux)},
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
