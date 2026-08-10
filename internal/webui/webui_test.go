package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerServesIndexAtRoot(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /: status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
		t.Fatalf("GET /: body does not look like the built index.html: %s", rec.Body.String())
	}
}

func TestHandlerFallsBackToIndexForClientRoutes(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	// /jobs/some-job-id is owned by the frontend's own router, not a real
	// file in dist/ - a hard refresh or shared link on a deep route must
	// still resolve to the app shell, not a 404.
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/some-job-id", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /jobs/some-job-id: status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "<div id=\"root\">") {
		t.Fatalf("GET /jobs/some-job-id: body does not look like the built index.html: %s", rec.Body.String())
	}
}

func TestHandlerServesRealAssetsAsIs(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/favicon.svg", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /favicon.svg: status = %d, want 200", rec.Code)
	}
	ct := rec.Header().Get("Content-Type")
	if !strings.Contains(ct, "svg") {
		t.Fatalf("GET /favicon.svg: Content-Type = %q, want an svg type", ct)
	}
}
