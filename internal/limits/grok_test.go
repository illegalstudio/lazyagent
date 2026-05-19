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
