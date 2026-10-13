package limits

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// makeJWT builds a minimal unsigned JWT carrying the given `sub` claim, so we can
// exercise cursorUserIDFromToken without a real Cursor token.
func makeJWT(sub string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"` + sub + `"}`))
	return header + "." + payload + ".sig"
}

func TestCursorUserIDFromToken(t *testing.T) {
	t.Run("strips the auth0/workos prefix before the pipe", func(t *testing.T) {
		got, err := cursorUserIDFromToken(makeJWT("auth0|user_01J7B053"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "user_01J7B053" {
			t.Fatalf("got %q, want %q", got, "user_01J7B053")
		}
	})

	t.Run("returns sub verbatim when there is no pipe", func(t *testing.T) {
		got, err := cursorUserIDFromToken(makeJWT("user_plain"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "user_plain" {
			t.Fatalf("got %q, want %q", got, "user_plain")
		}
	})

	t.Run("errors on a non-JWT string", func(t *testing.T) {
		if _, err := cursorUserIDFromToken("not-a-jwt"); err == nil {
			t.Fatal("expected an error for a malformed token, got nil")
		}
	})
}

// loadCursorSummary decodes a testdata fixture into the wire type, so the
// mapping tests exercise the same JSON path production does.
func loadCursorSummary(t *testing.T, name string) *cursorUsageSummary {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var s cursorUsageSummary
	if err := json.Unmarshal(data, &s); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	return &s
}

func TestCursorSummaryToReports_Individual(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	reports, err := cursorSummaryToReports(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want exactly 2", len(reports))
	}

	wantEnd := time.Date(2026, 8, 25, 13, 40, 53, 0, time.UTC)

	// Build the expected billing-cycle text from the same fixture timestamps
	// cursorSummaryToReports parses, formatted the same way (start.Local() /
	// end.Local()) so the assertion doesn't hardcode a date that would only
	// match in one timezone.
	start, err := time.Parse(time.RFC3339, s.BillingCycleStart)
	if err != nil {
		t.Fatalf("parse fixture BillingCycleStart: %v", err)
	}
	end, err := time.Parse(time.RFC3339, s.BillingCycleEnd)
	if err != nil {
		t.Fatalf("parse fixture BillingCycleEnd: %v", err)
	}
	cycle := fmt.Sprintf("billing cycle %s – %s", start.Local().Format("2 Jan"), end.Local().Format("2 Jan"))

	// The Models row asserts autoPercentUsed (5.219), never totalPercentUsed
	// (12.484 in the fixture) — the latter would double-count the API spend the
	// second row already reports.
	cases := []struct {
		provider string
		pct      float64
		source   string
	}{
		{"Cursor Models", 5.219, fmt.Sprintf("Source: 5.2%% of the plan's included Auto/Composer allowance (Ultra); %s", cycle)},
		{"Cursor API", 41.544, fmt.Sprintf("Source: 41.5%% of the plan's included API allowance (Ultra); %s", cycle)},
	}
	for i, c := range cases {
		r := reports[i]
		if r.Provider != c.provider {
			t.Errorf("reports[%d].Provider = %q, want %q", i, r.Provider, c.provider)
		}
		if len(r.Windows) != 1 {
			t.Fatalf("reports[%d]: got %d windows, want exactly 1", i, len(r.Windows))
		}
		w := r.Windows[0]
		if w.UsedPercent != c.pct {
			t.Errorf("%s UsedPercent = %v, want %v", c.provider, w.UsedPercent, c.pct)
		}
		if !w.ResetsAt.Equal(wantEnd) {
			t.Errorf("%s ResetsAt = %v, want %v", c.provider, w.ResetsAt, wantEnd)
		}
		if w.WindowMinutes != 31*24*60 {
			t.Errorf("%s WindowMinutes = %d, want %d", c.provider, w.WindowMinutes, 31*24*60)
		}
		if !isGlobalWindow(w) {
			t.Errorf("%s: monthly window should classify as global, got label %q", c.provider, w.Label)
		}
		if r.Note == "" {
			t.Errorf("%s: expected a non-empty disclaimer Note", c.provider)
		}
		if r.Source != c.source {
			t.Errorf("%s Source = %q, want %q", c.provider, r.Source, c.source)
		}
	}
}

func TestCursorSummaryToReports_Unlimited(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.IsUnlimited = true
	if _, err := cursorSummaryToReports(s); !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("err = %v, want errAgentUnavailable", err)
	}
}

func TestCursorSummaryToReports_UnifiedSubscription(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary_unified.json")
	if s.MembershipType != "unified_ultra" {
		t.Fatalf("membership = %q, want unified_ultra", s.MembershipType)
	}
	reports, err := cursorSummaryToReports(s)
	if len(reports) != 0 || !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("reports = %+v, err = %v, want no reports and errAgentUnavailable", reports, err)
	}
	if !strings.Contains(err.Error(), "xAI") || !strings.Contains(err.Error(), "--agent grok") {
		t.Fatalf("err = %v, want guidance to the shared xAI limits", err)
	}
}

func TestCursorSummaryToReports_UnifiedWithoutLegacyUsage(t *testing.T) {
	for _, membership := range []string{"unified_free", "unified_plus", "unified_premium", "unified_super", "unified_ultra"} {
		t.Run(membership, func(t *testing.T) {
			// Unified responses do not need the old billing dates or percent fields.
			s := &cursorUsageSummary{MembershipType: membership}
			reports, err := cursorSummaryToReports(s)
			if len(reports) != 0 || !errors.Is(err, errAgentUnavailable) || !strings.Contains(err.Error(), "xAI") {
				t.Fatalf("reports = %+v, err = %v, want unified subscription guidance", reports, err)
			}
		})
	}
}

func TestCursorSummaryToReports_PlanDisabled(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.IndividualUsage.Plan.Enabled = false
	if _, err := cursorSummaryToReports(s); !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("err = %v, want errAgentUnavailable", err)
	}
}

// TestCursorSummaryToReports_MissingPercentFields covers a renamed or dropped
// upstream field: AutoPercentUsed/APIPercentUsed are *float64 specifically so
// this case is distinguishable from a genuine 0.0% (which would otherwise be
// reported, confidently and wrongly, in green).
func TestCursorSummaryToReports_MissingPercentFields(t *testing.T) {
	t.Run("autoPercentUsed absent", func(t *testing.T) {
		s := loadCursorSummary(t, "cursor_usage_summary.json")
		s.IndividualUsage.Plan.AutoPercentUsed = nil
		_, err := cursorSummaryToReports(s)
		if !errors.Is(err, errAgentUnavailable) {
			t.Fatalf("err = %v, want errAgentUnavailable", err)
		}
		if !strings.Contains(err.Error(), "autoPercentUsed") {
			t.Fatalf("err = %v, want it to name autoPercentUsed", err)
		}
	})
	t.Run("apiPercentUsed absent", func(t *testing.T) {
		s := loadCursorSummary(t, "cursor_usage_summary.json")
		s.IndividualUsage.Plan.APIPercentUsed = nil
		_, err := cursorSummaryToReports(s)
		if !errors.Is(err, errAgentUnavailable) {
			t.Fatalf("err = %v, want errAgentUnavailable", err)
		}
		if !strings.Contains(err.Error(), "apiPercentUsed") {
			t.Fatalf("err = %v, want it to name apiPercentUsed", err)
		}
	})
}

// Team accounts report through teamUsage with the same shape; the individual
// block is absent.
func TestCursorSummaryToReports_FallsBackToTeamUsage(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.TeamUsage.Plan = s.IndividualUsage.Plan
	s.IndividualUsage.Plan = nil
	reports, err := cursorSummaryToReports(s)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d reports, want 2", len(reports))
	}
	if got := reports[1].Windows[0].UsedPercent; got != 41.544 {
		t.Errorf("API UsedPercent = %v, want 41.544", got)
	}
}

func TestCursorSummaryToReports_NoUsageBlocks(t *testing.T) {
	s := loadCursorSummary(t, "cursor_usage_summary.json")
	s.IndividualUsage.Plan = nil
	if _, err := cursorSummaryToReports(s); !errors.Is(err, errAgentUnavailable) {
		t.Fatalf("err = %v, want errAgentUnavailable", err)
	}
}

func TestCursorSummaryToReports_MalformedCycleDates(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		s := loadCursorSummary(t, "cursor_usage_summary.json")
		s.BillingCycleStart = "not-a-date"
		_, err := cursorSummaryToReports(s)
		if err == nil || !strings.Contains(err.Error(), "billingCycleStart") {
			t.Fatalf("err = %v, want an error naming billingCycleStart", err)
		}
	})
	t.Run("end", func(t *testing.T) {
		s := loadCursorSummary(t, "cursor_usage_summary.json")
		s.BillingCycleEnd = ""
		_, err := cursorSummaryToReports(s)
		if err == nil || !strings.Contains(err.Error(), "billingCycleEnd") {
			t.Fatalf("err = %v, want an error naming billingCycleEnd", err)
		}
	})
}

func TestCursorPlanName(t *testing.T) {
	cases := map[string]string{
		"ultra":    "Ultra",
		"pro":      "Pro",
		"pro_plus": "Pro+",
		"business": "Business",
		"":         "Cursor",
	}
	for in, want := range cases {
		if got := cursorPlanName(in); got != want {
			t.Errorf("cursorPlanName(%q) = %q, want %q", in, got, want)
		}
	}
}
