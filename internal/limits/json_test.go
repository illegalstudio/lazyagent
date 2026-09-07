package limits

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestWriteJSONShape(t *testing.T) {
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	results := []agentReport{
		{agent: "claude", report: Report{
			Provider: "Claude Code",
			Source:   "source note",
			Note:     "disclaimer",
			Windows: []Window{
				// 50% used, 50% elapsed => on track, info severity.
				{Label: "5-hour", WindowMinutes: 300, UsedPercent: 50, ResetsAt: now.Add(150 * time.Minute)},
				// 95% used => danger; reset 24h out of 7d.
				{Label: "7-day", WindowMinutes: 7 * 24 * 60, UsedPercent: 95, ResetsAt: now.Add(24 * time.Hour)},
			},
		}},
		{agent: "cursor", report: Report{
			Provider: "Cursor API",
			// No reset time and no known window length: pace and reset are unknown.
			Windows: []Window{{Label: "monthly", UsedPercent: 12}},
		}},
	}
	errs := []agentError{
		{agent: "grok", kind: errKindNotInstalled, err: errAgentNotInstalled},
		{agent: "kimi", kind: errKindError, err: errors.New("HTTP 401")},
	}

	var buf bytes.Buffer
	if err := writeJSON(&buf, results, errs, now); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "\n") {
		t.Error("output should end with a newline")
	}

	var got struct {
		GeneratedAt time.Time `json:"generated_at"`
		Reports     []struct {
			Agent     string `json:"agent"`
			Provider  string `json:"provider"`
			ShortName string `json:"short_name"`
			Source    string `json:"source"`
			Note      string `json:"note"`
			Windows   []struct {
				Label           string     `json:"label"`
				WindowMinutes   int        `json:"window_minutes"`
				UsedPercent     float64    `json:"used_percent"`
				ExpectedPercent float64    `json:"expected_percent"`
				PaceKnown       bool       `json:"pace_known"`
				PaceRatio       float64    `json:"pace_ratio"`
				PaceLabel       string     `json:"pace_label"`
				UsedSeverity    string     `json:"used_severity"`
				ResetsAt        *time.Time `json:"resets_at"`
				ResetInSeconds  int64      `json:"reset_in_seconds"`
				ResetRelative   string     `json:"reset_relative"`
			} `json:"windows"`
			Summary struct {
				FiveHour   *map[string]any `json:"five_hour"`
				WeekGlobal *map[string]any `json:"week_global"`
			} `json:"summary"`
		} `json:"reports"`
		Errors []struct {
			Agent   string `json:"agent"`
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}

	if !got.GeneratedAt.Equal(now) {
		t.Errorf("generated_at = %v, want %v", got.GeneratedAt, now)
	}
	if len(got.Reports) != 2 {
		t.Fatalf("reports = %d, want 2", len(got.Reports))
	}

	claude := got.Reports[0]
	if claude.Agent != "claude" || claude.Provider != "Claude Code" || claude.ShortName != "Claude" {
		t.Errorf("claude identity = %q/%q/%q", claude.Agent, claude.Provider, claude.ShortName)
	}
	if claude.Source != "source note" || claude.Note != "disclaimer" {
		t.Errorf("claude source/note = %q/%q", claude.Source, claude.Note)
	}
	if len(claude.Windows) != 2 {
		t.Fatalf("claude windows = %d, want 2", len(claude.Windows))
	}
	fiveH := claude.Windows[0]
	if fiveH.Label != "5-hour" || fiveH.WindowMinutes != 300 || fiveH.UsedPercent != 50 {
		t.Errorf("5h window = %+v", fiveH)
	}
	if fiveH.ExpectedPercent < 49 || fiveH.ExpectedPercent > 51 {
		t.Errorf("5h expected_percent = %.1f, want ~50", fiveH.ExpectedPercent)
	}
	if !fiveH.PaceKnown || fiveH.PaceLabel != "on track" || fiveH.PaceRatio < 0.99 || fiveH.PaceRatio > 1.01 {
		t.Errorf("5h pace = known=%v label=%q ratio=%.2f", fiveH.PaceKnown, fiveH.PaceLabel, fiveH.PaceRatio)
	}
	if fiveH.UsedSeverity != string(SevInfo) {
		t.Errorf("5h used_severity = %q, want %q", fiveH.UsedSeverity, SevInfo)
	}
	if fiveH.ResetsAt == nil || !fiveH.ResetsAt.Equal(now.Add(150*time.Minute)) {
		t.Errorf("5h resets_at = %v", fiveH.ResetsAt)
	}
	if fiveH.ResetInSeconds != 150*60 || fiveH.ResetRelative != "in 2h 30m" {
		t.Errorf("5h reset = %d s / %q", fiveH.ResetInSeconds, fiveH.ResetRelative)
	}
	if claude.Windows[1].UsedSeverity != string(SevDanger) {
		t.Errorf("7d used_severity = %q, want %q", claude.Windows[1].UsedSeverity, SevDanger)
	}
	if claude.Summary.FiveHour == nil || claude.Summary.WeekGlobal == nil {
		t.Fatalf("claude summary cells missing: %+v", claude.Summary)
	}
	if sev := (*claude.Summary.WeekGlobal)["severity"]; sev != string(SevDanger) {
		t.Errorf("week_global severity = %v, want %q", sev, SevDanger)
	}
	if text := (*claude.Summary.FiveHour)["text"]; !strings.Contains(text.(string), "50.0% used") {
		t.Errorf("five_hour text = %v", text)
	}

	cursor := got.Reports[1]
	if cursor.Agent != "cursor" || cursor.ShortName != "Cursor API" {
		t.Errorf("cursor identity = %q/%q", cursor.Agent, cursor.ShortName)
	}
	if cursor.Summary.FiveHour != nil {
		t.Errorf("cursor five_hour = %v, want null", *cursor.Summary.FiveHour)
	}
	if cursor.Summary.WeekGlobal == nil {
		t.Error("cursor week_global = null, want the monthly window")
	}
	monthly := cursor.Windows[0]
	if monthly.ResetsAt != nil || monthly.ResetInSeconds != 0 || monthly.ResetRelative != "" {
		t.Errorf("monthly reset should be unknown: %+v", monthly)
	}
	if monthly.PaceKnown || monthly.PaceLabel != "" || monthly.PaceRatio != 0 {
		t.Errorf("monthly pace should be unknown: %+v", monthly)
	}

	if len(got.Errors) != 2 {
		t.Fatalf("errors = %d, want 2", len(got.Errors))
	}
	if got.Errors[0].Agent != "grok" || got.Errors[0].Kind != errKindNotInstalled ||
		got.Errors[0].Message != notInstalledMessage("grok") {
		t.Errorf("grok error = %+v", got.Errors[0])
	}
	if got.Errors[1].Agent != "kimi" || got.Errors[1].Kind != errKindError || got.Errors[1].Message != "HTTP 401" {
		t.Errorf("kimi error = %+v", got.Errors[1])
	}
}

func TestWriteJSONEmptyEncodesArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, nil, nil, time.Now()); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	s := buf.String()
	if !strings.Contains(s, `"reports": []`) || !strings.Contains(s, `"errors": []`) {
		t.Errorf("empty slices should encode as [], got:\n%s", s)
	}
}

func TestExitCodeFor(t *testing.T) {
	report := agentReport{agent: "claude", report: Report{Provider: "Claude Code"}}
	notInstalled := agentError{agent: "grok", kind: errKindNotInstalled, err: errAgentNotInstalled}
	unavailable := agentError{agent: "cursor", kind: errKindUnavailable, err: errAgentUnavailable}
	hard := agentError{agent: "kimi", kind: errKindError, err: errors.New("boom")}

	cases := []struct {
		name      string
		results   []agentReport
		errs      []agentError
		requested int
		explicit  bool
		want      int
	}{
		{"all ok", []agentReport{report}, nil, 5, false, 0},
		{"all mode skips missing", []agentReport{report}, []agentError{notInstalled, unavailable}, 5, false, 0},
		{"all mode hard error", []agentReport{report}, []agentError{hard}, 5, false, 1},
		{"all mode nothing found", nil, []agentError{notInstalled, notInstalled, unavailable, notInstalled, notInstalled}, 5, false, 1},
		{"explicit ok", []agentReport{report}, nil, 1, true, 0},
		{"explicit missing", nil, []agentError{notInstalled}, 1, true, 1},
		{"explicit unavailable", nil, []agentError{unavailable}, 1, true, 1},
		{"explicit hard error", nil, []agentError{hard}, 1, true, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := exitCodeFor(tc.results, tc.errs, tc.requested, tc.explicit); got != tc.want {
				t.Errorf("exitCodeFor = %d, want %d", got, tc.want)
			}
		})
	}
}
