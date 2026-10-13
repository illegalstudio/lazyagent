package limits

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadGrokTokenFromBytes_Valid(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_auth.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got, err := readGrokTokenFromBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "test-jwt-token-payload" {
		t.Errorf("got %q, want %q", got, "test-jwt-token-payload")
	}
}

func TestReadGrokTokenFromBytes_NoEntries(t *testing.T) {
	_, err := readGrokTokenFromBytes([]byte(`{}`))
	if err == nil {
		t.Fatal("expected error for empty auth.json, got nil")
	}
}

func TestReadGrokTokenFromBytes_EmptyKey(t *testing.T) {
	data := []byte(`{"https://auth.x.ai::abc":{"key":""}}`)
	_, err := readGrokTokenFromBytes(data)
	if err == nil {
		t.Fatal("expected error for empty key, got nil")
	}
}

func TestReadGrokTokenFromBytes_Malformed(t *testing.T) {
	_, err := readGrokTokenFromBytes([]byte(`not json`))
	if err == nil {
		t.Fatal("expected parse error for malformed JSON, got nil")
	}
}

func TestReadGrokToken_EnvOverride(t *testing.T) {
	t.Setenv("GROK_OAUTH_TOKEN", "env-token")
	got, err := readGrokToken()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "env-token" {
		t.Errorf("got %q, want %q", got, "env-token")
	}
}

func TestParseGrokBillingFromBytes(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_billing.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	resp, err := parseGrokBilling(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Config == nil {
		t.Fatal("config is nil")
	}
	if resp.Config.MonthlyLimit.Val != 60000 {
		t.Errorf("monthlyLimit: got %d, want 60000", resp.Config.MonthlyLimit.Val)
	}
	if resp.Config.Used.Val != 8325 {
		t.Errorf("used: got %d, want 8325", resp.Config.Used.Val)
	}
	if resp.Config.BillingPeriodEnd.IsZero() {
		t.Error("billingPeriodEnd parsed as zero time")
	}
}

func TestGrokConfigToWindow(t *testing.T) {
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	cfg := &grokBillingConfig{
		MonthlyLimit:       grokCents{Val: 60000},
		Used:               grokCents{Val: 8325},
		OnDemandCap:        grokCents{Val: 0},
		BillingPeriodStart: start,
		BillingPeriodEnd:   end,
	}
	w := grokConfigToWindow(cfg)
	if w.Label != "monthly" {
		t.Errorf("label: got %q, want %q", w.Label, "monthly")
	}
	wantMinutes := int(end.Sub(start).Minutes()) // 31 * 24 * 60 = 44640
	if w.WindowMinutes != wantMinutes {
		t.Errorf("windowMinutes: got %d, want %d", w.WindowMinutes, wantMinutes)
	}
	wantPct := 100 * float64(8325) / float64(60000)
	if w.UsedPercent < wantPct-0.001 || w.UsedPercent > wantPct+0.001 {
		t.Errorf("usedPercent: got %.4f, want %.4f", w.UsedPercent, wantPct)
	}
	if !w.ResetsAt.Equal(end) {
		t.Errorf("resetsAt: got %v, want %v", w.ResetsAt, end)
	}
}

func TestParseGrokBillingCredits(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_billing_credits.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	resp, err := parseGrokBilling(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	cfg := resp.Config
	if cfg == nil {
		t.Fatal("config is nil")
	}
	if cfg.CurrentPeriod == nil {
		t.Fatal("currentPeriod is nil")
	}
	if cfg.CurrentPeriod.Type != "USAGE_PERIOD_TYPE_WEEKLY" {
		t.Errorf("period type: got %q", cfg.CurrentPeriod.Type)
	}
	if cfg.CreditUsagePercent != 5.0 {
		t.Errorf("creditUsagePercent: got %v, want 5.0", cfg.CreditUsagePercent)
	}
	if len(cfg.ProductUsage) != 1 || cfg.ProductUsage[0].Product != "GrokBuild" {
		t.Errorf("productUsage: got %+v", cfg.ProductUsage)
	}
	// The unified-billing response zeroes the legacy monthlyLimit — it must
	// not be treated as "no subscription".
	if cfg.MonthlyLimit.Val != 0 {
		t.Errorf("monthlyLimit: got %d, want 0", cfg.MonthlyLimit.Val)
	}
}

func TestGrokCreditsToWindow(t *testing.T) {
	start := time.Date(2026, 8, 20, 20, 12, 42, 0, time.UTC)
	end := time.Date(2026, 8, 27, 20, 12, 42, 0, time.UTC)
	cfg := &grokBillingConfig{
		CurrentPeriod:      &grokUsagePeriod{Type: "USAGE_PERIOD_TYPE_WEEKLY", Start: start, End: end},
		CreditUsagePercent: 5.0,
	}
	w := grokCreditsToWindow(cfg)
	if w.Label != "weekly" {
		t.Errorf("label: got %q, want %q", w.Label, "weekly")
	}
	if want := int(end.Sub(start).Minutes()); w.WindowMinutes != want {
		t.Errorf("windowMinutes: got %d, want %d", w.WindowMinutes, want)
	}
	if w.UsedPercent != 5.0 {
		t.Errorf("usedPercent: got %v, want 5.0", w.UsedPercent)
	}
	if !w.ResetsAt.Equal(end) {
		t.Errorf("resetsAt: got %v, want %v", w.ResetsAt, end)
	}
}

func TestGrokBillingToReport(t *testing.T) {
	standalone := false
	cases := []struct {
		name     string
		fixture  string
		flag     *bool
		provider string
		label    string
		used     float64
	}{
		{"legacy monthly", "grok_billing.json", nil, "Grok", "monthly", 13.875},
		{"unified credits", "grok_billing_credits.json", nil, "xAI", "weekly", 5},
		{"standalone credits", "grok_billing_credits.json", &standalone, "Grok", "weekly", 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", c.fixture))
			if err != nil {
				t.Fatal(err)
			}
			billing, err := parseGrokBilling(data)
			if err != nil {
				t.Fatal(err)
			}
			if c.flag != nil {
				billing.Config.IsUnifiedBillingUser = *c.flag
			}
			report, err := grokBillingToReport(billing.Config)
			if err != nil {
				t.Fatal(err)
			}
			if report.Provider != c.provider {
				t.Errorf("provider = %q, want %q", report.Provider, c.provider)
			}
			if len(report.Windows) != 1 || report.Windows[0].Label != c.label || report.Windows[0].UsedPercent != c.used {
				t.Errorf("windows = %+v, want one %s window at %v%%", report.Windows, c.label, c.used)
			}
			if report.Source == "" || report.Note == "" {
				t.Error("missing report provenance")
			}
		})
	}
}

func TestGrokPeriodLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"USAGE_PERIOD_TYPE_WEEKLY", "weekly"},
		{"USAGE_PERIOD_TYPE_MONTHLY", "monthly"},
		{"USAGE_PERIOD_TYPE_DAILY", "daily"},
		{"SOMETHING_ELSE", "period"},
		{"", "period"},
	}
	for _, c := range cases {
		if got := grokPeriodLabel(c.in); got != c.want {
			t.Errorf("grokPeriodLabel(%q): got %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGrokConfigToWindow_ZeroLimitYieldsZeroPercent(t *testing.T) {
	cfg := &grokBillingConfig{
		MonthlyLimit:       grokCents{Val: 0},
		Used:               grokCents{Val: 1234},
		BillingPeriodStart: time.Now(),
		BillingPeriodEnd:   time.Now().Add(24 * time.Hour),
	}
	w := grokConfigToWindow(cfg)
	if w.UsedPercent != 0 {
		t.Errorf("usedPercent with zero monthlyLimit: got %.4f, want 0", w.UsedPercent)
	}
}

func TestFormatCentsUSD(t *testing.T) {
	cases := []struct {
		cents int64
		want  string
	}{
		{0, "$0.00"},
		{1, "$0.01"},
		{99, "$0.99"},
		{100, "$1.00"},
		{8325, "$83.25"},
		{60000, "$600.00"},
		{123456, "$1234.56"},
	}
	for _, c := range cases {
		if got := formatCentsUSD(c.cents); got != c.want {
			t.Errorf("formatCentsUSD(%d): got %q, want %q", c.cents, got, c.want)
		}
	}
}
