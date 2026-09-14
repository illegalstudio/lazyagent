package limits

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReadKimiTokenFromBytes(t *testing.T) {
	got, err := readKimiTokenFromBytes([]byte(`{"access_token":"test-token","refresh_token":"ignored"}`))
	if err != nil {
		t.Fatalf("readKimiTokenFromBytes() error = %v", err)
	}
	if got != "test-token" {
		t.Fatalf("got %q, want test-token", got)
	}
}

func TestReadKimiTokenFromBytes_Empty(t *testing.T) {
	_, err := readKimiTokenFromBytes([]byte(`{"access_token":""}`))
	if err == nil {
		t.Fatal("expected error for empty access token")
	}
}

func TestReadKimiToken_EnvOverride(t *testing.T) {
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "env-token")
	got, err := readKimiToken(context.Background())
	if err != nil {
		t.Fatalf("readKimiToken() error = %v", err)
	}
	if got != "env-token" {
		t.Fatalf("got %q, want env-token", got)
	}
}

func TestReadKimiToken_File(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", root)
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "")
	writeKimiSlot(t, root, "kimi-code.json", `{"access_token":"file-token"}`)

	got, err := readKimiToken(context.Background())
	if err != nil {
		t.Fatalf("readKimiToken() error = %v", err)
	}
	if got != "file-token" {
		t.Fatalf("got %q, want file-token", got)
	}
}

// A global (.ai) login stores its token in a scoped slot named by config.toml,
// not in the legacy kimi-code.json — the case that left Linux installs without
// a Kimi row.
func TestReadKimiToken_ScopedSlot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", root)
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "")
	writeKimiConfig(t, root, `https://api.kimi.ai/coding/v1`, "oauth/kimi-code-env-0e4f99c69cc27850", "https://auth.kimi.ai")
	writeKimiSlot(t, root, "kimi-code-env-0e4f99c69cc27850.json", `{"access_token":"scoped-token"}`)

	got, err := readKimiToken(context.Background())
	if err != nil {
		t.Fatalf("readKimiToken() error = %v", err)
	}
	if got != "scoped-token" {
		t.Fatalf("got %q, want scoped-token", got)
	}
}

func TestKimiTokenExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	cases := []struct {
		name  string
		creds kimiCredentials
		want  bool
	}{
		{"no expiry", kimiCredentials{}, false},
		{"fresh", kimiCredentials{ExpiresAt: now.Add(10 * time.Minute).Unix()}, false},
		{"within skew", kimiCredentials{ExpiresAt: now.Add(30 * time.Second).Unix()}, true},
		{"expired", kimiCredentials{ExpiresAt: now.Add(-time.Minute).Unix()}, true},
	}
	for _, tc := range cases {
		if got := kimiTokenExpired(tc.creds, now); got != tc.want {
			t.Errorf("%s: kimiTokenExpired() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// An expired slot is refreshed through Kimi's own grant, and the rotated
// tokens are written back so the CLI does not keep an invalidated refresh token.
func TestReadKimiToken_RefreshesExpiredSlot(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", root)
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "")

	var gotForm url.Values
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/oauth/token" {
			t.Errorf("path = %q, want /api/oauth/token", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm() error = %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-access","refresh_token":"new-refresh","expires_in":900,"scope":"kimi-code","token_type":"Bearer"}`))
	}))
	defer auth.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", auth.URL)

	slot := writeKimiSlot(t, root, "kimi-code.json", `{"access_token":"stale","refresh_token":"old-refresh","expires_at":1,"device_id":"keep-me"}`)

	got, err := readKimiToken(context.Background())
	if err != nil {
		t.Fatalf("readKimiToken() error = %v", err)
	}
	if got != "new-access" {
		t.Fatalf("got %q, want new-access", got)
	}
	if gotForm.Get("grant_type") != "refresh_token" || gotForm.Get("refresh_token") != "old-refresh" {
		t.Fatalf("refresh form = %v", gotForm)
	}
	if gotForm.Get("client_id") != kimiOAuthClientID {
		t.Fatalf("client_id = %q, want %q", gotForm.Get("client_id"), kimiOAuthClientID)
	}

	data, err := os.ReadFile(slot)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatalf("stored credentials are not JSON: %v", err)
	}
	if stored["access_token"] != "new-access" || stored["refresh_token"] != "new-refresh" {
		t.Fatalf("stored = %v, want rotated tokens", stored)
	}
	if stored["device_id"] != "keep-me" {
		t.Fatalf("stored dropped unknown fields: %v", stored)
	}
	if expiresAt, _ := stored["expires_at"].(float64); int64(expiresAt) <= time.Now().Unix() {
		t.Fatalf("expires_at = %v, want a future timestamp", stored["expires_at"])
	}
	if info, err := os.Stat(slot); err != nil {
		t.Fatal(err)
	} else if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("slot mode = %v, want 0600", perm)
	}
}

// A refresh that fails must not hide the usage call's own 401 guidance.
func TestReadKimiToken_RefreshFailureKeepsStaleToken(t *testing.T) {
	root := t.TempDir()
	t.Setenv("KIMI_SHARE_DIR", root)
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "")
	auth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer auth.Close()
	t.Setenv("KIMI_CODE_OAUTH_HOST", auth.URL)
	writeKimiSlot(t, root, "kimi-code.json", `{"access_token":"stale","refresh_token":"old","expires_at":1}`)

	got, err := readKimiToken(context.Background())
	if err != nil {
		t.Fatalf("readKimiToken() error = %v", err)
	}
	if got != "stale" {
		t.Fatalf("got %q, want the stale token", got)
	}
}

func writeKimiSlot(t *testing.T, root, name, body string) string {
	t.Helper()
	dir := filepath.Join(root, "credentials")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeKimiConfig(t *testing.T, root, baseURL, oauthKey, oauthHost string) {
	t.Helper()
	config := fmt.Sprintf(`[providers."managed:kimi-code"]
type = "kimi"
base_url = %q

[providers."managed:kimi-code".oauth]
storage = "file"
key = %q
oauth_host = %q
`, baseURL, oauthKey, oauthHost)
	if err := os.WriteFile(filepath.Join(root, "config.toml"), []byte(config), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestKimiUsageToReport(t *testing.T) {
	data := []byte(`{
		"usage":{"limit":"100","remaining":"80","resetTime":"2026-05-28T12:15:14Z"},
		"limits":[{"window":{"duration":300,"timeUnit":"TIME_UNIT_MINUTE"},"detail":{"limit":"50","used":"5","resetTime":"2026-05-24T18:15:14Z"}}],
		"totalQuota":{"limit":"100","remaining":"79"},
		"parallel":{"limit":"20"}
	}`)
	resp, err := parseKimiUsage(data)
	if err != nil {
		t.Fatalf("parseKimiUsage() error = %v", err)
	}
	report := kimiUsageToReport(resp)
	if report.Provider != "Kimi Code" {
		t.Fatalf("Provider = %q, want Kimi Code", report.Provider)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(report.Windows))
	}
	weekly := report.Windows[0]
	if weekly.Label != "weekly" || weekly.WindowMinutes != 7*24*60 {
		t.Fatalf("weekly window = %+v", weekly)
	}
	if weekly.UsedPercent != 20 {
		t.Fatalf("weekly UsedPercent = %.2f, want 20", weekly.UsedPercent)
	}
	fiveHour := report.Windows[1]
	if fiveHour.Label != "5-hour" || fiveHour.WindowMinutes != 300 {
		t.Fatalf("5-hour window = %+v", fiveHour)
	}
	if fiveHour.UsedPercent != 10 {
		t.Fatalf("5-hour UsedPercent = %.2f, want 10", fiveHour.UsedPercent)
	}
	wantReset := time.Date(2026, 5, 24, 18, 15, 14, 0, time.UTC)
	if !fiveHour.ResetsAt.Equal(wantReset) {
		t.Fatalf("fiveHour ResetsAt = %v, want %v", fiveHour.ResetsAt, wantReset)
	}
	if !strings.Contains(report.Source, "20 of 100 weekly quota used") {
		t.Fatalf("Source missing weekly quota: %q", report.Source)
	}
	if !strings.Contains(report.Source, "21 of 100 total quota used") {
		t.Fatalf("Source missing total quota: %q", report.Source)
	}
	if !strings.Contains(report.Source, "parallel limit: 20") {
		t.Fatalf("Source missing parallel limit: %q", report.Source)
	}
}

func TestFetchKimiReport(t *testing.T) {
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "env-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/usages" {
			t.Fatalf("path = %q, want /usages", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer env-token" {
			t.Fatalf("Authorization = %q, want bearer env-token", got)
		}
		if got := r.Header.Get("User-Agent"); !strings.Contains(got, "lazyagent/") {
			t.Fatalf("User-Agent = %q, want lazyagent", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"usage":{"limit":"100","remaining":"100","resetTime":"2026-05-28T12:15:14Z"}}`))
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_BASE_URL", server.URL)

	report, err := fetchKimiReport(context.Background())
	if err != nil {
		t.Fatalf("fetchKimiReport() error = %v", err)
	}
	if report.Provider != "Kimi Code" || len(report.Windows) != 1 {
		t.Fatalf("report = %+v, want one Kimi Code window", report)
	}
}

func TestFetchKimiReportUnauthorized(t *testing.T) {
	t.Setenv("KIMI_CODE_OAUTH_TOKEN", "env-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	t.Setenv("KIMI_CODE_BASE_URL", server.URL)

	_, err := fetchKimiReport(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("fetchKimiReport() error = %v, want 401", err)
	}
}
