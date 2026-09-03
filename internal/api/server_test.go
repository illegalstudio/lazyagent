package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
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
const testSalt = "lazyagent-api-v1-test"

// newTestServer spins up an httptest server with the same handler chain
// the production server uses (CORS + auth middleware + routes), so tests
// genuinely exercise the auth middleware.
func newTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv, err := New(":0", testProvider{}, testToken, testSalt, nil)
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

// fixedSessionProvider returns a fixed set of sessions, for tests that need
// GET /api/sessions to have known CWDs to filter against.
type fixedSessionProvider struct {
	sessions []*model.Session
}

func (p fixedSessionProvider) DiscoverSessions() ([]*model.Session, error) { return p.sessions, nil }
func (fixedSessionProvider) UseWatcher() bool                              { return false }
func (fixedSessionProvider) RefreshInterval() time.Duration                { return 0 }
func (fixedSessionProvider) WatchDirs() []string                           { return nil }

// newTestServerWithSessions is like newTestServer but seeds the manager
// with a fixed set of sessions via one synchronous Reload, so dir-filter
// tests have known CWDs to filter against.
func newTestServerWithSessions(t *testing.T, sessions []*model.Session) (*Server, *httptest.Server) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	srv, err := New(":0", fixedSessionProvider{sessions: sessions}, testToken, testSalt, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv.ln.Close()
	if err := srv.manager.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	ts := httptest.NewServer(srv.srv.Handler)
	return srv, ts
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

// TestGetSessionsNoDirParamUnchanged is the binding "byte-identical"
// requirement: an absent dir param and an explicit empty dir= must produce
// exactly the same response as GET /api/sessions always has, unfiltered.
func TestGetSessionsNoDirParamUnchanged(t *testing.T) {
	base := t.TempDir()
	now := time.Now()
	sessions := []*model.Session{
		{SessionID: "a", CWD: base, LastActivity: now},
		{SessionID: "b", CWD: "/somewhere/else", LastActivity: now},
	}
	_, ts := newTestServerWithSessions(t, sessions)
	defer ts.Close()

	respNoParam := authedGet(t, ts.URL+"/api/sessions")
	defer respNoParam.Body.Close()
	bodyNoParam, err := io.ReadAll(respNoParam.Body)
	if err != nil {
		t.Fatal(err)
	}

	respEmptyParam := authedGet(t, ts.URL+"/api/sessions?dir=")
	defer respEmptyParam.Body.Close()
	bodyEmptyParam, err := io.ReadAll(respEmptyParam.Body)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(bodyNoParam, bodyEmptyParam) {
		t.Fatalf("dir= (empty) must be byte-identical to no dir param at all\nno-param: %s\nempty:    %s", bodyNoParam, bodyEmptyParam)
	}

	var items []SessionItem
	if err := json.Unmarshal(bodyNoParam, &items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (both sessions, unfiltered)", len(items))
	}
}

// TestGetSessionsDirFilter covers exact match, subdirectory match, and
// false-prefix exclusion — the same matching semantics as `lazyagent
// sessions`.
func TestGetSessionsDirFilter(t *testing.T) {
	base := t.TempDir()
	sub := filepath.Join(base, "sub")
	now := time.Now()
	sessions := []*model.Session{
		{SessionID: "exact", CWD: base, LastActivity: now},
		{SessionID: "subdir", CWD: sub, LastActivity: now},
		{SessionID: "false-prefix", CWD: base + "extra", LastActivity: now},
		{SessionID: "unrelated", CWD: "/somewhere/else", LastActivity: now},
	}
	_, ts := newTestServerWithSessions(t, sessions)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/sessions?dir="+url.QueryEscape(base))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var items []SessionItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	got := make(map[string]bool, len(items))
	for _, it := range items {
		got[it.SessionID] = true
	}
	if len(items) != 2 || !got["exact"] || !got["subdir"] {
		t.Fatalf("dir filter returned %v (%d items), want exactly [exact subdir]", got, len(items))
	}
}

// TestGetSessionsDirFilterRejectsRelativePath: the API has no meaningful
// "current directory" for a remote client, so a non-absolute dir is a 400,
// matching the existing error convention (JSON body with an "error" key).
func TestGetSessionsDirFilterRejectsRelativePath(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/sessions?dir=relative/path")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Fatal("expected a non-empty \"error\" field in the 400 body")
	}
}

// TestGetSessionsDirFilterNonexistentDirMatchesCleanedPath is the API
// relaxation vs. the CLI: the API may serve remote clients, so a dir that
// doesn't exist on this machine's disk is not an error — it matches on the
// cleaned path alone (no symlink resolution possible, no existence check).
func TestGetSessionsDirFilterNonexistentDirMatchesCleanedPath(t *testing.T) {
	base := t.TempDir()
	nonexistent := filepath.Join(base, "does-not-exist")
	now := time.Now()
	sessions := []*model.Session{
		{SessionID: "target", CWD: nonexistent, LastActivity: now},
		{SessionID: "other", CWD: base, LastActivity: now},
	}
	_, ts := newTestServerWithSessions(t, sessions)
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/sessions?dir="+url.QueryEscape(nonexistent))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no existence requirement)", resp.StatusCode)
	}
	var items []SessionItem
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(items) != 1 || items[0].SessionID != "target" {
		t.Fatalf("got %d items (%v), want exactly [target]", len(items), items)
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

func TestGetSessionIncludesNormalAndYoloResumeCommands(t *testing.T) {
	now := time.Now()
	_, ts := newTestServerWithSessions(t, []*model.Session{{
		Agent: "claude", SessionID: "abc", CWD: t.TempDir(), LastActivity: now,
	}})
	defer ts.Close()

	resp := authedGet(t, ts.URL+"/api/sessions/abc")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var detail SessionFull
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if detail.ResumeCommand != "claude --resume abc" {
		t.Fatalf("resume command = %q", detail.ResumeCommand)
	}
	if detail.ResumeCommandYolo != "claude --dangerously-skip-permissions --resume abc" {
		t.Fatalf("YOLO resume command = %q", detail.ResumeCommandYolo)
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
	if v, ok := body["api_salt"]; ok && v != "" {
		t.Fatalf("api_salt should not be returned from /api/config, got %v", v)
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

func TestPlaygroundQuotesSalt(t *testing.T) {
	html := renderPlayground(`";alert(1)//`)
	if strings.Contains(html, `const KDF_SALT = "";alert(1)//";`) {
		t.Fatal("salt was interpolated into JavaScript without escaping")
	}
	if !strings.Contains(html, `const KDF_SALT = "\";alert(1)//";`) {
		t.Fatal("escaped salt literal not found in playground HTML")
	}
}

func TestAuthInfoIsPublic(t *testing.T) {
	_, ts := newTestServer(t)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/auth")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var info AuthInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if info.Salt != testSalt {
		t.Fatalf("salt = %q, want %q", info.Salt, testSalt)
	}
	if info.Iterations == 0 || info.KeyLength == 0 || info.Hash == "" || info.Encoding == "" {
		t.Fatalf("incomplete auth info: %+v", info)
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
