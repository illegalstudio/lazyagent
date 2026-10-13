package limits

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
