package limits

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// codexUsageSample mirrors the real GET /backend-api/wham/usage payload.
const codexUsageSample = `{
  "plan_type": "pro",
  "rate_limit": {
    "allowed": true,
    "primary_window":   {"used_percent": 24, "limit_window_seconds": 18000,  "reset_after_seconds": 2383,   "reset_at": 1780055984},
    "secondary_window": {"used_percent": 93, "limit_window_seconds": 604800, "reset_after_seconds": 119311, "reset_at": 1780172911}
  },
  "additional_rate_limits": [
    {"limit_name": "GPT-5.3-Codex-Spark", "metered_feature": "codex_bengalfox",
     "rate_limit": {"primary_window": {"used_percent": 0, "limit_window_seconds": 18000, "reset_at": 1780071601}}}
  ]
}`

func TestReadCodexAuthFromBytes(t *testing.T) {
	token, acct, err := readCodexAuthFromBytes([]byte(`{"auth_mode":"chatgpt","tokens":{"access_token":"abc123","account_id":"acc-1"}}`))
	if err != nil {
		t.Fatalf("readCodexAuthFromBytes() error = %v", err)
	}
	if token != "abc123" || acct != "acc-1" {
		t.Fatalf("got (%q, %q), want (abc123, acc-1)", token, acct)
	}

	if _, _, err := readCodexAuthFromBytes([]byte(`{"tokens":{"access_token":""}}`)); err != errAgentNotInstalled {
		t.Fatalf("empty token: err = %v, want errAgentNotInstalled", err)
	}

	if _, _, err := readCodexAuthFromBytes([]byte(`not json`)); err == nil {
		t.Fatal("malformed json should error")
	}
}

func TestCodexUsageToReport(t *testing.T) {
	usage, err := parseCodexUsage([]byte(codexUsageSample))
	if err != nil {
		t.Fatalf("parseCodexUsage() error = %v", err)
	}
	report := codexUsageToReport(usage)

	if report.Provider != "Codex" {
		t.Fatalf("provider = %q, want Codex", report.Provider)
	}
	if !strings.Contains(report.Source, "pro") {
		t.Fatalf("source = %q, want it to mention the plan", report.Source)
	}
	// The two plan-wide windows, then the Spark model's own 5-hour window,
	// scoped by its limit name so it gets its own summary row.
	if len(report.Windows) != 3 {
		t.Fatalf("got %d windows, want 3", len(report.Windows))
	}
	if spark := report.Windows[2]; spark.Scope != "GPT-5.3-Codex-Spark" || spark.Label != "5-hour" || spark.UsedPercent != 0 {
		t.Fatalf("additional = %+v, want scoped GPT-5.3-Codex-Spark 5-hour window", spark)
	}

	p := report.Windows[0]
	if p.Label != "5-hour" || p.WindowMinutes != 300 || p.UsedPercent != 24 || p.ResetsAt.Unix() != 1780055984 {
		t.Fatalf("primary = %+v, want 5-hour/300m/24%%/reset 1780055984", p)
	}
	s := report.Windows[1]
	if s.Label != "7-day" || s.WindowMinutes != 10080 || s.UsedPercent != 93 || s.ResetsAt.Unix() != 1780172911 {
		t.Fatalf("secondary = %+v, want 7-day/10080m/93%%/reset 1780172911", s)
	}
}

func TestCodexUsageToReportSkipsWindowsWithoutLength(t *testing.T) {
	// A window with no limit_window_seconds is unusable and must be dropped.
	usage, err := parseCodexUsage([]byte(`{"rate_limit":{"primary_window":{"used_percent":5,"limit_window_seconds":18000,"reset_at":1780055984},"secondary_window":{"used_percent":0}}}`))
	if err != nil {
		t.Fatal(err)
	}
	report := codexUsageToReport(usage)
	if len(report.Windows) != 1 || report.Windows[0].Label != "5-hour" {
		t.Fatalf("windows = %+v, want only the 5-hour window", report.Windows)
	}
}

func TestCodexWindowLabel(t *testing.T) {
	cases := map[int]string{
		300:         "5-hour",
		7 * 24 * 60: "7-day",
		3 * 24 * 60: "3-day",
		2 * 60:      "2-hour",
		45:          "45-minute",
		0:           "window",
	}
	for minutes, want := range cases {
		if got := codexWindowLabel(minutes); got != want {
			t.Errorf("codexWindowLabel(%d) = %q, want %q", minutes, got, want)
		}
	}
}

func TestFetchCodexReportLive(t *testing.T) {
	var gotAuth, gotAccount string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccount = r.Header.Get("chatgpt-account-id")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(codexUsageSample))
	}))
	defer srv.Close()

	t.Setenv("CODEX_USAGE_URL", srv.URL)
	t.Setenv("CODEX_OAUTH_TOKEN", "tok-xyz")
	t.Setenv("CODEX_ACCOUNT_ID", "acc-9")

	report, err := fetchCodexReport(context.Background())
	if err != nil {
		t.Fatalf("fetchCodexReport() error = %v", err)
	}
	if gotAuth != "Bearer tok-xyz" {
		t.Fatalf("Authorization header = %q, want Bearer tok-xyz", gotAuth)
	}
	if gotAccount != "acc-9" {
		t.Fatalf("chatgpt-account-id header = %q, want acc-9", gotAccount)
	}
	if len(report.Windows) != 3 || report.Windows[0].UsedPercent != 24 || report.Windows[1].UsedPercent != 93 {
		t.Fatalf("windows = %+v, want live 24%%/93%%", report.Windows)
	}
}

func TestFetchCodexReportUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"expired"}`))
	}))
	defer srv.Close()

	t.Setenv("CODEX_USAGE_URL", srv.URL)
	t.Setenv("CODEX_OAUTH_TOKEN", "tok-expired")

	_, err := fetchCodexReport(context.Background())
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err = %v, want a 401 error mentioning expiry", err)
	}
}

func TestFetchCodexReportNotInstalled(t *testing.T) {
	// No env token and a HOME without ~/.codex/auth.json => errAgentNotInstalled.
	t.Setenv("CODEX_OAUTH_TOKEN", "")
	t.Setenv("HOME", t.TempDir())

	_, err := fetchCodexReport(context.Background())
	if err == nil {
		t.Fatal("expected an error when no auth is present")
	}
	if err.Error() != errAgentNotInstalled.Error() {
		t.Fatalf("err = %v, want errAgentNotInstalled", err)
	}
}
