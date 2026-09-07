package limits

import (
	"encoding/json"
	"io"
	"time"
)

// limitsJSON is the wire shape of `lazyagent limits --json`. Field names are
// part of the CLI contract: external consumers (shell widgets, dashboards,
// scripts) parse this instead of the human table, so keys are stable and
// every field is always present. Slices encode as [] rather than null.
type limitsJSON struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Reports     []reportJSON `json:"reports"`
	Errors      []errorJSON  `json:"errors"`
}

// reportJSON is one metered pool. Cursor emits two rows with the same agent id
// ("cursor") and different provider names, matching the human table.
type reportJSON struct {
	Agent     string       `json:"agent"`      // "claude" | "codex" | "grok" | "kimi" | "cursor"
	Provider  string       `json:"provider"`   // "Claude Code", "Cursor Models", ...
	ShortName string       `json:"short_name"` // summary-table label: "Claude", "Kimi", ...
	Source    string       `json:"source"`
	Note      string       `json:"note"`
	Windows   []windowJSON `json:"windows"`
	Summary   summaryJSON  `json:"summary"`
}

type windowJSON struct {
	Label           string     `json:"label"`
	WindowMinutes   int        `json:"window_minutes"`
	UsedPercent     float64    `json:"used_percent"`
	ExpectedPercent float64    `json:"expected_percent"` // linear pace for elapsed window time
	PaceKnown       bool       `json:"pace_known"`
	PaceRatio       float64    `json:"pace_ratio"` // used / expected; 0 when pace is unknown
	PaceLabel       string     `json:"pace_label"` // "underutilizing" | "on track" | "overutilizing" | ""
	UsedSeverity    Severity   `json:"used_severity"`
	ResetsAt        *time.Time `json:"resets_at"`        // null when the provider gives no reset time
	ResetInSeconds  int64      `json:"reset_in_seconds"` // 0 when unknown or already passed
	ResetRelative   string     `json:"reset_relative"`   // "in 3h 17m" or ""
}

// summaryJSON mirrors the two columns of the human table. A cell is null when
// the provider does not expose that window, which is what the table prints
// as "--".
type summaryJSON struct {
	FiveHour   *summaryCellJSON `json:"five_hour"`
	WeekGlobal *summaryCellJSON `json:"week_global"`
}

type summaryCellJSON struct {
	UsedPercent     float64  `json:"used_percent"`
	ExpectedPercent float64  `json:"expected_percent"`
	Severity        Severity `json:"severity"`
	Text            string   `json:"text"`
}

// errorJSON records why an agent produced no report. Kinds:
//   - "not_installed": no credentials or footprint on this machine
//   - "unavailable":   installed, but nothing meaningful to report (e.g. an
//     unlimited Cursor plan)
//   - "error":         a real failure (network, expired token, bad response)
//
// The human output hides the first two kinds in `--agent all` mode; the JSON
// output always lists them so a consumer can tell "not installed" from
// "installed but failed" without re-deriving that from the machine.
type errorJSON struct {
	Agent   string `json:"agent"`
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

func buildJSON(results []agentReport, errs []agentError, now time.Time) limitsJSON {
	out := limitsJSON{
		GeneratedAt: now,
		Reports:     make([]reportJSON, 0, len(results)),
		Errors:      make([]errorJSON, 0, len(errs)),
	}
	for _, ar := range results {
		out.Reports = append(out.Reports, buildReportJSON(ar, now))
	}
	for _, ae := range errs {
		out.Errors = append(out.Errors, errorJSON{
			Agent:   ae.agent,
			Kind:    ae.kind,
			Message: ae.message(),
		})
	}
	return out
}

func buildReportJSON(ar agentReport, now time.Time) reportJSON {
	r := ar.report
	rj := reportJSON{
		Agent:     ar.agent,
		Provider:  r.Provider,
		ShortName: summaryProviderName(r.Provider),
		Source:    r.Source,
		Note:      r.Note,
		Windows:   make([]windowJSON, 0, len(r.Windows)),
		Summary: summaryJSON{
			FiveHour:   buildSummaryCellJSON(r, summaryFiveHour, now),
			WeekGlobal: buildSummaryCellJSON(r, summaryWeekGlobal, now),
		},
	}
	for _, w := range r.Windows {
		rj.Windows = append(rj.Windows, buildWindowJSON(w, now))
	}
	return rj
}

func buildWindowJSON(w Window, now time.Time) windowJSON {
	wv := buildWindowView(w, now)
	wj := windowJSON{
		Label:           wv.Label,
		WindowMinutes:   w.WindowMinutes,
		UsedPercent:     wv.UsedPercent,
		ExpectedPercent: wv.ExpectedPercent,
		PaceKnown:       wv.PaceKnown,
		PaceRatio:       wv.PaceRatio,
		PaceLabel:       wv.PaceLabel,
		UsedSeverity:    wv.UsedSeverity,
		ResetRelative:   wv.ResetRelative,
	}
	if !w.ResetsAt.IsZero() {
		resetsAt := w.ResetsAt
		wj.ResetsAt = &resetsAt
		if rem := w.ResetsAt.Sub(now); rem > 0 {
			wj.ResetInSeconds = int64(rem.Seconds())
		}
	}
	return wj
}

func buildSummaryCellJSON(r Report, kind summaryWindowKind, now time.Time) *summaryCellJSON {
	cell := buildSummaryCell(r, kind, now)
	if !cell.Present {
		return nil
	}
	return &summaryCellJSON{
		UsedPercent:     cell.UsedPercent,
		ExpectedPercent: cell.ExpectedPercent,
		Severity:        cell.Severity,
		Text:            cell.Text,
	}
}

// writeJSON emits the payload as indented JSON followed by a newline.
func writeJSON(w io.Writer, results []agentReport, errs []agentError, now time.Time) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(buildJSON(results, errs, now))
}
