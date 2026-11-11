package limits

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

func TestClaudeUsageToReportMetersResetsAndCredits(t *testing.T) {
	var usage claudeUsageResponse
	if err := json.Unmarshal(loadTestdata(t, "claude_usage_at_wall.json"), &usage); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	now := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	r := claudeUsageToReport(&usage, now)

	if len(r.Windows) != 2 {
		t.Fatalf("windows = %+v, want weekly + Fable", r.Windows)
	}
	all, fable := r.Windows[0], r.Windows[1]
	if all.Label != "7-day" || all.Scope != "" || all.UsedPercent != 14 || all.WindowMinutes != 7*24*60 {
		t.Errorf("weekly_all window = %+v", all)
	}
	if fable.Label != "7-day" || fable.Scope != "Fable" || fable.UsedPercent != 18 {
		t.Errorf("Fable window = %+v", fable)
	}
	if fable.Detail != "9% of the weekly limit used, capped at 50%" {
		t.Errorf("Fable detail = %q", fable.Detail)
	}
	if fable.ResetsAt.IsZero() {
		t.Error("Fable window has no reset time")
	}

	// The used-up grant is dropped; the juniper_tide block is not shown
	// because the account is neither eligible nor in the experiment.
	if len(r.Resets) != 1 {
		t.Fatalf("resets = %+v, want one grant with resets left", r.Resets)
	}
	rs := r.Resets[0]
	if rs.Label != "New model launch: one Fable reset" || rs.Left != 1 || rs.Total != 1 || !rs.Usable || rs.Status != "" {
		t.Errorf("reset = %+v", rs)
	}
	if got := rs.ExpiresAt.UTC().Format(time.RFC3339); got != "2027-03-09T07:59:00Z" {
		t.Errorf("reset expiry = %s", got)
	}

	if len(r.Credits) != 1 {
		t.Fatalf("credits = %+v, want extra usage", r.Credits)
	}
	c := r.Credits[0]
	if c.Label != "Extra usage" || c.Enabled || c.Unit != "EUR" || c.Limit != nil {
		t.Errorf("extra usage credit = %+v", c)
	}
	if got := creditText(c); got != "disabled · set by your organization" {
		t.Errorf("credit text = %q", got)
	}
}

func TestClaudeUsageFallsBackToLegacyWindows(t *testing.T) {
	usage := claudeUsageResponse{
		FiveHour: &claudeWindow{Utilization: 21, ResetsAt: "2026-11-10T23:00:00Z"},
		SevenDay: &claudeWindow{Utilization: 30, ResetsAt: "2026-11-17T03:00:00Z"},
	}
	r := claudeUsageToReport(&usage, time.Now())
	if len(r.Windows) != 2 || r.Windows[0].Label != "5-hour" || r.Windows[1].Label != "7-day" {
		t.Fatalf("windows = %+v, want legacy 5-hour + 7-day", r.Windows)
	}
}

func TestClaudeSessionMeterMapsToFiveHour(t *testing.T) {
	usage := claudeUsageResponse{Limits: []claudeLimit{{Kind: "session", Group: "session", Percent: 40}}}
	r := claudeUsageToReport(&usage, time.Now())
	if len(r.Windows) != 1 || !isFiveHourWindow(r.Windows[0]) {
		t.Fatalf("windows = %+v, want a 5-hour window", r.Windows)
	}
}

func TestClaudeJuniperTideReset(t *testing.T) {
	usage := claudeUsageResponse{
		Limits:      []claudeLimit{{Kind: "weekly_all", Percent: 10}},
		JuniperTide: &claudeJuniperTide{Eligible: true, Available: true, ResetsPerWeek: 1},
	}
	r := claudeUsageToReport(&usage, time.Now())
	if len(r.Resets) != 1 || r.Resets[0].Label != "5-hour limit reset" || r.Resets[0].Left != 1 || !r.Resets[0].Usable {
		t.Fatalf("resets = %+v", r.Resets)
	}
}

func TestClaudeUserAgentClaimsRecentVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if got := claudeUserAgent(); got != "claude-cli/"+claudeMinCLIVersion+" (external, cli)" {
		t.Errorf("UA without ~/.claude.json = %q", got)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"lastReleaseNotesSeen":"2.2.5"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := claudeCLIVersion(); got != "2.2.5" {
		t.Errorf("version = %q, want installed 2.2.5", got)
	}
	if err := os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"lastReleaseNotesSeen":"2.0.1"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := claudeCLIVersion(); got != claudeMinCLIVersion {
		t.Errorf("version = %q, want the minimum for an older install", got)
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"2.1.322", "2.1.322", 0},
		{"2.1.400", "2.1.322", 1},
		{"2.1", "2.1.1", -1},
		{"bogus", "2.1.0", -1},
		{"3.0.0", "", 1},
	}
	for _, c := range cases {
		if got := compareVersions(c.a, c.b); got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestCodexUsageExtras(t *testing.T) {
	usage, err := parseCodexUsage(loadTestdata(t, "codex_usage_extras.json"))
	if err != nil {
		t.Fatal(err)
	}
	r := codexUsageToReport(usage)
	if len(r.Windows) != 3 {
		t.Fatalf("windows = %+v, want plan, Spark, code review", r.Windows)
	}
	if r.Windows[0].Scope != "" || r.Windows[0].UsedPercent != 100 {
		t.Errorf("plan window = %+v", r.Windows[0])
	}
	if r.Windows[1].Scope != "GPT-Spark" || r.Windows[1].Label != "5-hour" {
		t.Errorf("additional window = %+v", r.Windows[1])
	}
	if r.Windows[2].Scope != "Code review" {
		t.Errorf("code review window = %+v", r.Windows[2])
	}
	if len(r.Credits) != 1 {
		t.Fatalf("credits = %+v", r.Credits)
	}
	got := creditText(r.Credits[0])
	want := "28,315.67 credits · ~7,079-36,810 local messages · ~1,133-7,079 cloud messages"
	if got != want {
		t.Errorf("credit text = %q, want %q", got, want)
	}
}

func TestCodexCreditsAbsentWithoutBalance(t *testing.T) {
	if _, ok := codexCreditsToCredit(&codexCredits{HasCredits: false}, nil); ok {
		t.Error("an account without credits should produce no credit line")
	}
}

func TestCodexResetDetailsToResets(t *testing.T) {
	var d codexResetCreditDetails
	if err := json.Unmarshal(loadTestdata(t, "codex_reset_credits.json"), &d); err != nil {
		t.Fatal(err)
	}
	resets := codexResetDetailsToResets(&d)
	if len(resets) != 2 {
		t.Fatalf("resets = %+v, want the two available credits", resets)
	}
	if !resets[0].ExpiresAt.Before(resets[1].ExpiresAt) {
		t.Errorf("resets not sorted by expiry: %+v", resets)
	}
	if resets[0].Label != "Full reset" || resets[0].Left != 1 || !resets[0].Usable {
		t.Errorf("reset = %+v", resets[0])
	}
}

func TestFetchCodexReportReadsResetDetails(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/wham/usage":
			_, _ = w.Write(loadTestdata(t, "codex_usage_extras.json"))
		case "/wham/rate-limit-reset-credits":
			_, _ = w.Write(loadTestdata(t, "codex_reset_credits.json"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	t.Setenv("CODEX_USAGE_URL", srv.URL+"/wham/usage")
	t.Setenv("CODEX_OAUTH_TOKEN", "tok")

	r, err := fetchCodexReport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Resets) != 2 {
		t.Fatalf("resets = %+v", r.Resets)
	}
	for _, p := range paths {
		if !strings.HasPrefix(p, "GET ") || strings.Contains(p, "consume") {
			t.Errorf("unexpected request %q: lazyagent must only read", p)
		}
	}
}

func TestFetchCodexReportFallsBackToResetCount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wham/usage" {
			_, _ = w.Write(loadTestdata(t, "codex_usage_extras.json"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("CODEX_USAGE_URL", srv.URL+"/wham/usage")
	t.Setenv("CODEX_OAUTH_TOKEN", "tok")

	r, err := fetchCodexReport(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Resets) != 1 || r.Resets[0].Left != 2 {
		t.Fatalf("resets = %+v, want the count from the usage response", r.Resets)
	}
}

func TestGrokProductBreakdownAndPrepaid(t *testing.T) {
	cfg := &grokBillingConfig{
		CurrentPeriod:        &grokUsagePeriod{Type: "USAGE_PERIOD_TYPE_WEEKLY", Start: time.Now().Add(-24 * time.Hour), End: time.Now().Add(6 * 24 * time.Hour)},
		CreditUsagePercent:   5,
		ProductUsage:         []grokProductUsage{{Product: "GrokBuild", UsagePercent: 3}, {Product: "GrokBot", UsagePercent: 2}, {Product: "Idle", UsagePercent: 0}},
		IsUnifiedBillingUser: true,
		PrepaidBalance:       &grokCents{Val: 1250},
	}
	r, err := grokBillingToReport(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.Source, "(GrokBuild 3.0%, GrokBot 2.0%)") {
		t.Errorf("source = %q", r.Source)
	}
	if len(r.Credits) != 1 || creditText(r.Credits[0]) != "$12.50" {
		t.Errorf("credits = %+v", r.Credits)
	}
}

func TestCursorOnDemandCredit(t *testing.T) {
	var s cursorUsageSummary
	if err := json.Unmarshal(loadTestdata(t, "cursor_usage_summary.json"), &s); err != nil {
		t.Fatal(err)
	}
	reports, err := cursorSummaryToReports(&s)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports[0].Credits) != 0 {
		t.Errorf("Cursor Models should carry no credits: %+v", reports[0].Credits)
	}
	if len(reports[1].Credits) != 1 || creditText(reports[1].Credits[0]) != "$0.00 of $500.00 used" {
		t.Errorf("Cursor API credits = %+v", reports[1].Credits)
	}
}

func TestSummaryRowsSplitScopedWindows(t *testing.T) {
	now := time.Now()
	r := Report{
		Provider: "Claude Code",
		Windows: []Window{
			{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 14, ResetsAt: now.Add(time.Hour)},
			{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 18, ResetsAt: now.Add(time.Hour), Scope: "Fable"},
		},
	}
	v := BuildView([]Report{r}, now)
	if len(v.Summary) != 2 {
		t.Fatalf("summary = %+v, want Claude + Claude Fable", v.Summary)
	}
	if v.Summary[0].Provider != "Claude" || v.Summary[0].WeekGlobal.UsedPercent != 14 {
		t.Errorf("plan row = %+v", v.Summary[0])
	}
	if v.Summary[1].Provider != "Claude Fable" || v.Summary[1].WeekGlobal.UsedPercent != 18 {
		t.Errorf("scoped row = %+v", v.Summary[1])
	}
	if v.Reports[0].Windows[1].Title != "7-day window (Fable)" {
		t.Errorf("title = %q", v.Reports[0].Windows[1].Title)
	}

	var b strings.Builder
	renderSummaryTable(&b, []Report{r}, now)
	if !strings.Contains(b.String(), "Claude Fable") {
		t.Errorf("table missing scoped row:\n%s", b.String())
	}
}

func TestDetailedAndJSONIncludeExtras(t *testing.T) {
	now := time.Date(2026, 11, 10, 21, 0, 0, 0, time.UTC)
	r := Report{
		Provider: "Codex",
		Windows:  []Window{{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 50, ResetsAt: now.Add(48 * time.Hour)}},
		Resets:   []Reset{{Label: "Full reset", Left: 1, Usable: true, ExpiresAt: now.Add(20 * 24 * time.Hour)}},
		Credits:  []Credit{{Label: "Credits", Unit: "credits", Enabled: true, Balance: floatPtr(1234.5)}},
	}
	var b strings.Builder
	renderReport(&b, r, now)
	out := b.String()
	for _, want := range []string{"Resets", "Full reset", "1 left · expires in 20d 0h", "usable now", "Credits", "1,234.50 credits"} {
		if !strings.Contains(out, want) {
			t.Errorf("detailed output missing %q:\n%s", want, out)
		}
	}

	var buf bytes.Buffer
	if err := writeJSON(&buf, []agentReport{{agent: "codex", report: r}}, nil, now); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Reports []struct {
			Resets []struct {
				Label            string     `json:"label"`
				Left             int        `json:"left"`
				Usable           bool       `json:"usable"`
				ExpiresAt        *time.Time `json:"expires_at"`
				ExpiresInSeconds int64      `json:"expires_in_seconds"`
			} `json:"resets"`
			Credits []struct {
				Label   string   `json:"label"`
				Unit    string   `json:"unit"`
				Balance *float64 `json:"balance"`
				Used    *float64 `json:"used"`
				Text    string   `json:"text"`
			} `json:"credits"`
			Windows []struct {
				Scope  *string `json:"scope"`
				Detail *string `json:"detail"`
			} `json:"windows"`
		} `json:"reports"`
	}
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	rep := decoded.Reports[0]
	if len(rep.Resets) != 1 || rep.Resets[0].ExpiresAt == nil || rep.Resets[0].ExpiresInSeconds != 20*24*3600 {
		t.Errorf("resets json = %+v", rep.Resets)
	}
	if len(rep.Credits) != 1 || rep.Credits[0].Balance == nil || *rep.Credits[0].Balance != 1234.5 || rep.Credits[0].Used != nil {
		t.Errorf("credits json = %+v", rep.Credits)
	}
	if rep.Windows[0].Scope == nil || rep.Windows[0].Detail == nil {
		t.Errorf("window scope/detail keys must always be present: %s", buf.String())
	}
	if !strings.Contains(buf.String(), `"resets": [`) {
		t.Errorf("resets should encode as an array")
	}
}

func TestJSONEmptyExtrasEncodeAsArrays(t *testing.T) {
	var buf bytes.Buffer
	r := Report{Provider: "Kimi Code", Windows: []Window{{Label: "weekly", UsedPercent: 1}}}
	if err := writeJSON(&buf, []agentReport{{agent: "kimi", report: r}}, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"resets": []`) || !strings.Contains(buf.String(), `"credits": []`) {
		t.Errorf("empty extras should encode as []:\n%s", buf.String())
	}
}

func TestFormatAmount(t *testing.T) {
	cases := map[string]string{
		formatAmount(0, "EUR"):               "€0.00",
		formatAmount(1234567.891, "USD"):     "$1,234,567.89",
		formatAmount(28315.66928, "credits"): "28,315.67 credits",
		formatAmount(12, "chf"):              "12.00 CHF",
		formatAmount(-5, "USD"):              "-$5.00",
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("formatAmount = %q, want %q", got, want)
		}
	}
}
