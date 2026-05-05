package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// testProvider is a no-op provider for API tests.
type testProvider struct{}

func (testProvider) DiscoverSessions() ([]*model.Session, error) { return nil, nil }
func (testProvider) UseWatcher() bool                            { return false }
func (testProvider) RefreshInterval() time.Duration              { return 0 }
func (testProvider) WatchDirs() []string                         { return nil }

// testToken is the bearer token used by the test helper. Tests authenticate
// by sending Authorization: Bearer <testToken>. The token value itself is
// arbitrary — we don't exercise the PBKDF2 derivation here, only the
// middleware enforcement.
const testToken = "test-token"

// newTestServer spins up an httptest server with the same handler chain
// the production server uses (CORS + auth middleware + routes), so tests
// genuinely exercise the auth middleware.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv, err := New(":0", testProvider{}, testToken)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.ln.Close()
	ts := httptest.NewServer(srv.srv.Handler)
	return srv, ts
}

// authedGet performs a GET with the test bearer token attached.
func authedGet(t *testing.T, url string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestGetSessions(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/sessions")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var items []SessionItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func TestGetSessionNotFound(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/sessions/nonexistent")
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetStats(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/stats")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var stats StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if stats.WindowMinutes == 0 {
		t.Fatal("window_minutes should not be 0")
	}
}

func TestGetConfig(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/config")
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	// /api/config must not echo the passphrase even to an authenticated caller.
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if v, ok := body["api_passphrase"]; ok && v != "" {
		t.Fatalf("api_passphrase should not be returned, got %v", v)
	}
}

func TestPlaygroundIsPublic(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	// No Authorization header — must still succeed.
	resp, err := http.Get(ts.URL + "/api")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content-type = %q, want text/html", ct)
	}
}

func TestEndpointsRejectMissingToken(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	for _, path := range []string{"/api/sessions", "/api/stats", "/api/config", "/api/events"} {
		resp, err := http.Get(ts.URL + path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", path, resp.StatusCode)
		}
	}
}

func TestEndpointsRejectWrongToken(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	req, _ := http.NewRequest("GET", ts.URL+"/api/sessions", nil)
	req.Header.Set("Authorization", "Bearer not-the-right-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestSSE(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// SSE clients (EventSource) cannot send custom headers — verify the
	// query-string fallback works.
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events?token="+testToken, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	gotEvent := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: update") {
			gotEvent = true
		}
		if strings.HasPrefix(line, "data: ") && gotEvent {
			var payload SSEPayload
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("decode SSE data: %v", err)
			}
			break
		}
	}
	if !gotEvent {
		t.Fatal("never received SSE update event")
	}

	srv.notifySSE()

	gotSecond := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: update") {
			gotSecond = true
		}
		if strings.HasPrefix(line, "data: ") && gotSecond {
			var payload SSEPayload
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &payload); err != nil {
				t.Fatalf("decode second SSE data: %v", err)
			}
			break
		}
	}
	if !gotSecond {
		t.Fatal("never received second SSE update event after notifySSE")
	}
}
