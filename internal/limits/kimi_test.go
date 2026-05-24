package limits

import (
	"context"
	"net/http"
	"net/http/httptest"
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
	got, err := readKimiToken()
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
	credPath := filepath.Join(root, "credentials", "kimi-code.json")
	if err := os.MkdirAll(filepath.Dir(credPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credPath, []byte(`{"access_token":"file-token"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readKimiToken()
	if err != nil {
		t.Fatalf("readKimiToken() error = %v", err)
	}
	if got != "file-token" {
		t.Fatalf("got %q, want file-token", got)
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
