package apiauth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func newHandler(token string, exempt map[string]bool) http.Handler {
	mw := Middleware(token, exempt)
	return mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
}

func TestMiddlewareRejectsMissingHeader(t *testing.T) {
	h := newHandler("expected", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/sessions", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
	if rr.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing WWW-Authenticate header")
	}
}

func TestMiddlewareRejectsWrongToken(t *testing.T) {
	h := newHandler("expected", nil)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}

func TestMiddlewareAcceptsCorrectToken(t *testing.T) {
	h := newHandler("expected", nil)
	req := httptest.NewRequest("GET", "/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer expected")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
}

func TestMiddlewareAcceptsTokenFromQuery(t *testing.T) {
	h := newHandler("expected", nil)
	req := httptest.NewRequest("GET", "/api/events?token=expected", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (token from query)", rr.Code)
	}
}

func TestMiddlewareSkipsOptions(t *testing.T) {
	h := newHandler("expected", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("OPTIONS", "/api/sessions", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (OPTIONS preflight bypass)", rr.Code)
	}
}

func TestMiddlewareSkipsExemptPath(t *testing.T) {
	h := newHandler("expected", map[string]bool{"/api": true})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (exempt path)", rr.Code)
	}
}

func TestMiddlewareNonExemptStillProtected(t *testing.T) {
	h := newHandler("expected", map[string]bool{"/api": true})
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/api/sessions", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}
