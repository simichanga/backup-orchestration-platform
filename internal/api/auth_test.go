package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}

func TestAuthMiddlewareAcceptsValidToken(t *testing.T) {
	h := authMiddleware(hashTokens([]string{"good-token"}), okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	req.Header.Set("Authorization", "Bearer good-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareAcceptsAnyOfMultipleTokens(t *testing.T) {
	h := authMiddleware(hashTokens([]string{"token-a", "token-b"}), okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	req.Header.Set("Authorization", "Bearer token-b")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

func TestAuthMiddlewareRejectsMissingHeader(t *testing.T) {
	h := authMiddleware(hashTokens([]string{"good-token"}), okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareRejectsWrongToken(t *testing.T) {
	h := authMiddleware(hashTokens([]string{"good-token"}), okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestAuthMiddlewareRejectsMalformedHeader(t *testing.T) {
	h := authMiddleware(hashTokens([]string{"good-token"}), okHandler())

	req := httptest.NewRequest(http.MethodGet, "/v1/hosts", nil)
	req.Header.Set("Authorization", "good-token") // missing "Bearer " prefix
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
