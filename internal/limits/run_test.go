package limits

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/cursor"
)

func TestSingleWrapsOneReport(t *testing.T) {
	got, err := single(Report{Provider: "Grok"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Provider != "Grok" {
		t.Fatalf("got %+v, want a one-element slice holding the Grok report", got)
	}
}

func TestSinglePropagatesErrorAndDropsTheReport(t *testing.T) {
	sentinel := errors.New("boom")
	got, err := single(Report{Provider: "Grok"}, sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want the sentinel", err)
	}
	if got != nil {
		t.Fatalf("got %+v, want nil reports alongside an error", got)
	}
}

func TestFetchReportsRejectsUnknownAgent(t *testing.T) {
	_, err := fetchReports(context.Background(), "nope")
	if err == nil || !strings.Contains(err.Error(), "unsupported agent") {
		t.Fatalf("err = %v, want an unsupported-agent error", err)
	}
}

func TestCollectGrokCursorFallback(t *testing.T) {
	grokData, err := os.ReadFile(filepath.Join("testdata", "grok_billing_credits.json"))
	if err != nil {
		t.Fatal(err)
	}
	billing, err := parseGrokBilling(grokData)
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name          string
		agents        []string
		grokStatus    int
		grokBody      string
		noGrok        bool
		noCursor      bool
		standalone    bool
		cursorFails   bool
		rpcFails      bool
		wantReports   int
		wantErrAgents string
		wantCalls     [3]int // Grok billing, Cursor summary, Cursor current period.
		wantExit      int
	}{
		{name: "explicit Grok recovers", agents: []string{"grok"}, wantReports: 1, wantCalls: [3]int{1, 1, 1}},
		{name: "aggregate reuses Cursor", agents: []string{"grok", "cursor"}, wantReports: 1, wantCalls: [3]int{1, 1, 1}},
		{name: "expired Grok token recovers", agents: []string{"grok"}, grokStatus: 401, wantReports: 1, wantCalls: [3]int{1, 1, 1}},
		{name: "malformed Grok response recovers", agents: []string{"grok"}, grokStatus: 200, grokBody: "invalid JSON", wantReports: 1, wantCalls: [3]int{1, 1, 1}},
		{name: "standalone Cursor cannot replace Grok", agents: []string{"grok"}, standalone: true, wantErrAgents: "grok", wantCalls: [3]int{1, 1, 0}, wantExit: 1},
		{name: "aggregate preserves standalone pools", agents: []string{"grok", "cursor"}, standalone: true, wantReports: 2, wantErrAgents: "grok", wantCalls: [3]int{1, 1, 0}, wantExit: 1},
		{name: "missing Cursor preserves Grok error", agents: []string{"grok"}, noCursor: true, wantErrAgents: "grok", wantCalls: [3]int{1, 0, 0}, wantExit: 1},
		{name: "failed Cursor preserves Grok error", agents: []string{"grok"}, cursorFails: true, wantErrAgents: "grok", wantCalls: [3]int{1, 1, 0}, wantExit: 1},
		{name: "failed current period preserves Grok error", agents: []string{"grok"}, rpcFails: true, wantErrAgents: "grok", wantCalls: [3]int{1, 1, 1}, wantExit: 1},
		{name: "aggregate preserves both failures", agents: []string{"grok", "cursor"}, rpcFails: true, wantErrAgents: "grok,cursor", wantCalls: [3]int{1, 1, 1}, wantExit: 1},
		{name: "healthy Grok needs no fallback", agents: []string{"grok"}, grokStatus: 200, wantReports: 1, wantCalls: [3]int{1, 0, 0}},
		{name: "healthy shared pool stays deduplicated", agents: []string{"grok", "cursor"}, grokStatus: 200, wantReports: 1, wantCalls: [3]int{1, 1, 1}},
		{name: "missing Grok does not trigger fallback", agents: []string{"grok"}, noGrok: true, wantErrAgents: "grok", wantExit: 1},
		{name: "Cursor still works without Grok", agents: []string{"grok", "cursor"}, noGrok: true, wantReports: 1, wantErrAgents: "grok", wantCalls: [3]int{0, 1, 1}},
		{name: "unrelated error is preserved", agents: []string{"codex", "grok", "cursor"}, wantReports: 1, wantErrAgents: "codex", wantCalls: [3]int{1, 1, 1}, wantExit: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			setupLimitsAuth(t, !c.noGrok, !c.noCursor)
			calls := [3]int{}
			originalTransport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = originalTransport })
			http.DefaultTransport = cursorTestTransport(func(req *http.Request) (*http.Response, error) {
				status := http.StatusOK
				var body []byte
				switch req.URL.String() {
				case grokBillingURL:
					calls[0]++
					status = c.grokStatus
					if status == 0 {
						status = http.StatusInternalServerError
					}
					body = grokData
					if status != http.StatusOK {
						body = []byte(`{"error":"Failed to serialize billing response"}`)
					} else if c.grokBody != "" {
						body = []byte(c.grokBody)
					}
				case cursorUsageSummaryURL:
					calls[1]++
					fixture := "cursor_usage_summary_unified.json"
					if c.standalone {
						fixture = "cursor_usage_summary.json"
					}
					body, _ = json.Marshal(loadCursorSummary(t, fixture))
					if c.cursorFails {
						status = http.StatusUnauthorized
					}
				case cursorCurrentPeriodUsageURL:
					calls[2]++
					usage := loadCursorCurrentPeriod(t)
					usage.BillingCycleStart = billing.Config.CurrentPeriod.Start.UnixMilli()
					usage.BillingCycleEnd = billing.Config.CurrentPeriod.End.UnixMilli()
					body, _ = json.Marshal(usage)
					if c.rpcFails {
						status = http.StatusServiceUnavailable
					}
				case "https://chatgpt.com/backend-api/wham/usage":
					status = http.StatusServiceUnavailable
				default:
					return nil, fmt.Errorf("unexpected network call: %s", req.URL)
				}
				return &http.Response{
					StatusCode: status,
					Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
					Body:       io.NopCloser(strings.NewReader(string(body))),
					Header:     make(http.Header),
				}, nil
			})

			results, errs := collect(context.Background(), c.agents)
			if calls != c.wantCalls || len(results) != c.wantReports {
				t.Fatalf("calls = %v, reports = %d; want %v, %d", calls, len(results), c.wantCalls, c.wantReports)
			}
			var errAgents []string
			for _, ae := range errs {
				errAgents = append(errAgents, ae.agent)
				if ae.agent == "grok" {
					if c.noGrok {
						if ae.kind != errKindNotInstalled {
							t.Fatalf("error kind = %q, want not_installed", ae.kind)
						}
					} else if !strings.Contains(ae.message(), "Failed to serialize billing response") {
						t.Fatalf("lost original Grok failure: %v", ae.err)
					}
				}
			}
			if got := strings.Join(errAgents, ","); got != c.wantErrAgents {
				t.Fatalf("error agents = %q, want %q", got, c.wantErrAgents)
			}
			if code := exitCodeFor(results, errs, len(c.agents), len(c.agents) == 1); code != c.wantExit {
				t.Fatalf("exit code = %d, want %d", code, c.wantExit)
			}
			if len(results) == 1 {
				wire := buildJSON(results, errs, time.Now())
				report := wire.Reports[0]
				if report.Provider != "xAI" {
					t.Fatalf("provider = %q, want xAI", report.Provider)
				}
				if c.grokStatus == http.StatusOK && c.grokBody == "" {
					if report.Agent != "grok" {
						t.Fatalf("healthy Grok report attributed to %q", report.Agent)
					}
				} else if report.Agent != "cursor" || !strings.Contains(report.Source, "Cursor") || !strings.Contains(report.Note, "GetCurrentPeriodUsage") || report.Windows[0].UsedPercent != *loadCursorCurrentPeriod(t).PlanUsage.TotalPercentUsed {
					t.Fatalf("fallback lost Cursor usage or provenance: %+v", report)
				}
			}
		})
	}
}

func setupLimitsAuth(t *testing.T, withGrok, withCursor bool) {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_CONFIG_HOME", root)
	t.Setenv("APPDATA", root)
	t.Setenv("GROK_OAUTH_TOKEN", "")
	t.Setenv("CODEX_OAUTH_TOKEN", "test-codex-token")
	t.Setenv("CODEX_USAGE_URL", "")
	if withGrok {
		t.Setenv("GROK_OAUTH_TOKEN", "test-grok-token")
	}
	if !withCursor {
		return
	}
	dir := cursor.StateDBDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.vscdb"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO ItemTable (key, value) VALUES (?, ?)", "cursorAuth/accessToken", makeJWT("auth0|fallback-test")); err != nil {
		t.Fatal(err)
	}
}

func TestSubscriptionReportsPresentation(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "grok_billing_credits.json"))
	if err != nil {
		t.Fatal(err)
	}
	billing, err := parseGrokBilling(data)
	if err != nil {
		t.Fatal(err)
	}
	grok, err := grokBillingToReport(billing.Config)
	if err != nil {
		t.Fatal(err)
	}
	now := billing.Config.CurrentPeriod.Start.Add(time.Hour)
	for _, c := range []struct {
		name           string
		fixture        string
		withGrok       bool
		separatePeriod bool
		count          int
	}{
		{"both clients, same shared pool", "cursor_usage_summary_unified.json", true, false, 1},
		{"only Cursor installed", "cursor_usage_summary_unified.json", false, false, 1},
		{"distinct shared periods", "cursor_usage_summary_unified.json", true, true, 2},
		{"separate standalone Cursor", "cursor_usage_summary.json", true, false, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			var results []agentReport
			var errs []agentError
			if c.withGrok {
				results = appendAgentReports(results, "grok", []Report{grok})
			} else {
				errs = append(errs, agentError{agent: "grok", kind: errKindNotInstalled, err: errAgentNotInstalled})
			}
			summary := loadCursorSummary(t, c.fixture)
			var cursor []Report
			if cursorUnifiedMembership(summary.MembershipType) {
				usage := loadCursorCurrentPeriod(t)
				if !c.separatePeriod {
					usage.BillingCycleStart = billing.Config.CurrentPeriod.Start.UnixMilli()
					usage.BillingCycleEnd = billing.Config.CurrentPeriod.End.UnixMilli()
				}
				cursor, err = single(cursorCurrentPeriodToReport(usage))
			} else {
				cursor, err = cursorSummaryToReports(summary)
			}
			if err != nil {
				t.Fatal(err)
			}
			results = appendAgentReports(results, "cursor", cursor)
			reports := make([]Report, 0, len(results))
			for _, result := range results {
				reports = append(reports, result.report)
			}
			wantAgent := "cursor"
			if c.withGrok {
				wantAgent = "grok"
			}
			json := buildJSON(results, errs, now)
			if len(json.Reports) != c.count || json.Reports[0].Agent != wantAgent || json.Reports[0].Provider != "xAI" || json.Reports[0].ShortName != "xAI" {
				t.Fatalf("JSON reports = %+v, want %d reports headed by %s/xAI", json.Reports, c.count, wantAgent)
			}
			view := BuildView(reports, now)
			if len(view.Reports) != c.count || len(view.Summary) != c.count || view.Reports[0].Provider != "xAI" || view.Summary[0].Provider != "xAI" {
				t.Fatalf("view = %+v, want %d reports and summary rows headed by xAI", view, c.count)
			}
			var table strings.Builder
			renderSummaryTable(&table, reports, now)
			if !strings.Contains(table.String(), "xAI") || strings.Contains(table.String(), "Cursor") != (c.count == 3) {
				t.Fatalf("unexpected summary table: %s", table.String())
			}
			if code := exitCodeFor(results, errs, 2, false); code != 0 {
				t.Fatalf("exit code = %d, want 0 for shared or standalone reports", code)
			}
		})
	}
}
