package limits

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestScanRolloutForLimitsReadsLatestUsableBlock(t *testing.T) {
	path := writeCodexRollout(t, `
{"timestamp":"2026-05-25T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"primary":{"used_percent":1.0,"window_minutes":300,"resets_at":1779700000},"secondary":{"used_percent":2.0,"window_minutes":10080,"resets_at":1780100000}}}}
{"timestamp":"2026-05-25T10:01:00.000Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1200}},"rate_limits":{"primary":{"used_percent":17.0,"window_minutes":300,"resets_at":1779710000},"secondary":{"used_percent":9.0,"window_minutes":10080,"resets_at":1780110000}}}}
`)

	got, err := scanRolloutForLimits(path)
	if err != nil {
		t.Fatalf("scanRolloutForLimits() error = %v", err)
	}
	if got == nil {
		t.Fatal("scanRolloutForLimits() returned nil")
	}
	if got.Primary == nil || got.Primary.UsedPercent != 17 || got.Primary.WindowMinutes != 300 || got.Primary.ResetsAt != 1779710000 {
		t.Fatalf("primary = %#v, want latest usable primary window", got.Primary)
	}
	if got.Secondary == nil || got.Secondary.UsedPercent != 9 || got.Secondary.WindowMinutes != 10080 || got.Secondary.ResetsAt != 1780110000 {
		t.Fatalf("secondary = %#v, want latest usable secondary window", got.Secondary)
	}
}

func TestScanRolloutForLimitsIgnoresMalformedZeroWindow(t *testing.T) {
	path := writeCodexRollout(t, `
{"timestamp":"2026-05-25T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{"used_percent":11.0,"window_minutes":300,"resets_at":1779710000},"secondary":{"used_percent":7.0,"window_minutes":10080,"resets_at":1780110000}}}}
{"timestamp":"2026-05-25T10:01:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"primary":{},"secondary":{}}}}
`)

	got, err := scanRolloutForLimits(path)
	if err != nil {
		t.Fatalf("scanRolloutForLimits() error = %v", err)
	}
	if got == nil {
		t.Fatal("scanRolloutForLimits() returned nil")
	}
	if got.Primary == nil || got.Primary.UsedPercent != 11 {
		t.Fatalf("primary = %#v, want previous usable primary window", got.Primary)
	}
	if got.Secondary == nil || got.Secondary.UsedPercent != 7 {
		t.Fatalf("secondary = %#v, want previous usable secondary window", got.Secondary)
	}
}

func TestScanRolloutForLimitsSkipsZeroModelSpecificLimit(t *testing.T) {
	path := writeCodexRollout(t, `
{"timestamp":"2026-05-25T10:00:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":13.0,"window_minutes":300,"resets_at":1779710000},"secondary":{"used_percent":8.0,"window_minutes":10080,"resets_at":1780110000}}}}
{"timestamp":"2026-05-25T10:01:00.000Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex_bengalfox","limit_name":"GPT-5.3-Codex-Spark","primary":{"used_percent":0.0,"window_minutes":300,"resets_at":1779720000},"secondary":{"used_percent":0.0,"window_minutes":10080,"resets_at":1780120000}}}}
`)

	got, err := scanRolloutForLimits(path)
	if err != nil {
		t.Fatalf("scanRolloutForLimits() error = %v", err)
	}
	if got != nil {
		t.Fatalf("scanRolloutForLimits() = %#v, want nil so fetch can fall back to the next rollout", got)
	}
}

func TestCodexRateLimitsUsableAllowsZeroUsage(t *testing.T) {
	limits := &codexRateLimits{
		LimitID: "codex",
		Primary: &codexLimitWindow{
			UsedPercent:   0,
			WindowMinutes: 300,
			ResetsAt:      1779710000,
		},
	}
	if !codexRateLimitsUsable(limits) {
		t.Fatal("zero usage with a real window should be usable")
	}
}

func TestFetchCodexReportFallsBackPastZeroModelSpecificRollout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	newest := filepath.Join(home, ".codex", "sessions", "2026", "05", "22", "rollout-newest.jsonl")
	writeFile(t, newest, `
{"timestamp":"2026-05-22T16:39:14.300Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":16.0,"window_minutes":300,"resets_at":1779469440},"secondary":{"used_percent":22.0,"window_minutes":10080,"resets_at":1779820803}}}}
{"timestamp":"2026-05-25T13:42:41.225Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex_bengalfox","limit_name":"GPT-5.3-Codex-Spark","primary":{"used_percent":0.0,"window_minutes":300,"resets_at":1779734549},"secondary":{"used_percent":0.0,"window_minutes":10080,"resets_at":1780321349}}}}
`)

	fallback := filepath.Join(home, ".codex", "sessions", "2026", "05", "25", "rollout-fallback.jsonl")
	writeFile(t, fallback, `
{"timestamp":"2026-05-25T13:35:48.478Z","type":"event_msg","payload":{"type":"token_count","rate_limits":{"limit_id":"codex","primary":{"used_percent":2.0,"window_minutes":300,"resets_at":1779731904},"secondary":{"used_percent":13.0,"window_minutes":10080,"resets_at":1780172911}}}}
`)

	base := time.Unix(1779710000, 0)
	if err := os.Chtimes(fallback, base, base); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newest, base.Add(time.Minute), base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	report, err := fetchCodexReport()
	if err != nil {
		t.Fatalf("fetchCodexReport() error = %v", err)
	}
	if !strings.Contains(report.Source, fallback) {
		t.Fatalf("Source = %q, want fallback rollout %q", report.Source, fallback)
	}
	if len(report.Windows) != 2 {
		t.Fatalf("got %d windows, want 2", len(report.Windows))
	}
	if report.Windows[0].UsedPercent != 2 || report.Windows[1].UsedPercent != 13 {
		t.Fatalf("used percents = (%.1f, %.1f), want (2.0, 13.0)", report.Windows[0].UsedPercent, report.Windows[1].UsedPercent)
	}
}

func writeCodexRollout(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	writeFile(t, path, content)
	return path
}

func writeFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
