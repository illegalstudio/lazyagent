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
)

func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv, err := New(":0", false)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.ln.Close()
	ts := httptest.NewServer(srv.mux)
	return srv, ts
}

func TestGetSessions(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/sessions")
	if err != nil {
		t.Fatal(err)
	}
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

	resp, err := http.Get(ts.URL + "/api/sessions/nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}

func TestGetStats(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
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

	resp, err := http.Get(ts.URL + "/api/config")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestPlayground(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

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

func TestSSE(t *testing.T) {
	srv, ts := newTestServer(t)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	// Read initial snapshot: expect "event: update" line.
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
			// Got valid initial frame, success.
			break
		}
	}
	if !gotEvent {
		t.Fatal("never received SSE update event")
	}

	// Trigger a notification and verify a second frame arrives.
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
