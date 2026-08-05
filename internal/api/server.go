// Package api implements BOP's optional HTTP API (docs/02-architecture.md's
// "API" component). Read endpoints require a valid token; the one mutating
// endpoint (POST /v1/backups) requires a token in the separate write-scope
// set - see config.APIConfig's doc comment for why that's a distinct token
// list rather than a role on the same one. There is no anonymous access
// anywhere.
package api

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"bop/internal/controller"
	"bop/internal/queue"
)

// Server serves BOP's HTTP API, per config.yaml's api.* section.
type Server struct {
	httpServer *http.Server
	listener   net.Listener
}

// NewServer binds addr immediately, rather than deferring the bind to
// Start, matching metrics.NewServer: a port already in use is then a
// "bop controller" startup error, not a silently dead endpoint discovered
// later by whoever tries to call it. writeTokens may be nil/empty - see
// LoadWriteTokens - in which case POST /v1/backups is registered but
// unreachable by any token (a 401 for everyone, not a 404), since no
// write scope has been configured at all.
func NewServer(addr string, readTokens, writeTokens []string, ctl *controller.Controller, q queue.Queue) (*Server, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("api: listen on %s: %w", addr, err)
	}

	// A write token implicitly grants read access too - write is a
	// superset, not a separate track, so read-scope auth accepts either
	// token list.
	allTokens := make([]string, 0, len(readTokens)+len(writeTokens))
	allTokens = append(allTokens, readTokens...)
	allTokens = append(allTokens, writeTokens...)
	readHashes := hashTokens(allTokens)
	writeHashes := hashTokens(writeTokens)

	mux := http.NewServeMux()
	mux.Handle("GET /v1/hosts", authMiddleware(readHashes, listHostsHandler(ctl.Inventory)))
	mux.Handle("GET /v1/jobs", authMiddleware(readHashes, listJobsHandler(ctl.Metadata)))
	mux.Handle("GET /v1/jobs/{id}", authMiddleware(readHashes, getJobHandler(ctl.Metadata)))
	mux.Handle("GET /v1/snapshots", authMiddleware(readHashes, listSnapshotsHandler(ctl.Metadata)))
	mux.Handle("GET /v1/events", authMiddleware(readHashes, listEventsHandler(ctl.Metadata)))
	mux.Handle("POST /v1/backups", authMiddleware(writeHashes, triggerBackupHandler(ctl, q)))

	return &Server{
		httpServer: &http.Server{Handler: mux},
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
