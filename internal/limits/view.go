package limits

import (
	"context"
	"sync"
	"time"
)

// Severity is a style-free classification both UIs map to colors.
type Severity string

const (
	SevDefault Severity = "default" // neutral text
	SevOK      Severity = "ok"      // green
	SevInfo    Severity = "info"    // blue/primary
	SevWarn    Severity = "warn"    // orange
	SevDanger  Severity = "danger"  // red
)

// WindowView is a fully-computed, render-ready window (no styling, no time.Time).
type WindowView struct {
	Label           string
	Scope           string // model or feature the window is limited to, "" when plan-wide
	Title           string // "7-day window" or "7-day window (Fable)"
	Detail          string // optional provider context, "" when none
	UsedPercent     float64
	ExpectedPercent float64 // linear pace for elapsed window time
	PaceLabel       string  // "underutilizing" | "on track" | "overutilizing"
	PaceRatio       float64
	PaceKnown       bool
	UsedSeverity    Severity
	ResetRelative   string // "in 3h 17m" / "" if unknown
	ResetAbsolute   string // "Thu 30 Apr 20:10 CEST" / ""
	ResetUnix       int64  // 0 if unknown
}

// ReportView is the render-ready projection of a Report.
type ReportView struct {
	Provider string
	Source   string
	Note     string
	Windows  []WindowView
	Resets   []ResetView
	Credits  []CreditView
}

// ResetView is a render-ready banked limit reset.
type ResetView struct {
	Label           string
	Left            int
	Total           int  // 0 when the provider does not say
	Usable          bool // redeemable right now, per the provider
	Status          string
	ExpiresUnix     int64  // 0 when unknown
	ExpiresAbsolute string // "Tue 9 Mar 08:59 CET" or ""
	Text            string // full one-line rendering, as the CLI prints it
}

// CreditView is a render-ready credit balance or spend pool.
type CreditView struct {
	Label   string
	Enabled bool
	Text    string // "28,315.67 credits", "disabled · set by your organization"
}

type SummaryCell struct {
	Present         bool
	UsedPercent     float64
	ExpectedPercent float64
	Severity        Severity
	Text            string // "21.0% used / 40.0% exp" or "--"
}

type SummaryRow struct {
	Provider   string // short name: "Claude", "Kimi", ...
	FiveHour   SummaryCell
	WeekGlobal SummaryCell
}

// View is the complete, UI-agnostic limits payload consumed by the TUI and GUI.
type View struct {
	Reports   []ReportView
	Summary   []SummaryRow
	Available bool
}

// FetchAll queries every supported provider concurrently using the same
// per-agent fetchers as the CLI. Not-installed, unavailable, and transiently
// failing agents are omitted. Reports keep canonical order: claude, codex,
// grok, kimi, cursor.
// Callers must set a deadline on ctx; FetchAll does not impose its own timeout
// (TUI and GUI callers wrap a 30 s timeout before calling this function).
func FetchAll(ctx context.Context) []Report {
	agents, _ := resolveAgents("all") // "all" is always a valid argument, so the error is never non-nil here.
	results := make([][]Report, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a string) {
			defer wg.Done()
			rs, err := fetchReports(ctx, a)
			if err != nil {
				return
			}
			results[i] = rs
		}(i, a)
	}
	wg.Wait()

	var merged []agentReport
	for i := range agents {
		merged = appendAgentReports(merged, agents[i], results[i])
	}
	var out []Report
	for _, result := range merged {
		out = append(out, result.report)
	}
	return out
}

// BuildView computes the render-ready view from raw reports. now is explicit
// for testability.
func BuildView(reports []Report, now time.Time) View {
	v := View{Available: len(reports) > 0}
	for _, r := range reports {
		rv := ReportView{Provider: r.Provider, Source: r.Source, Note: r.Note}
		for _, w := range r.Windows {
			rv.Windows = append(rv.Windows, buildWindowView(w, now))
		}
		for _, rs := range r.Resets {
			rv.Resets = append(rv.Resets, buildResetView(rs, now))
		}
		for _, c := range r.Credits {
			rv.Credits = append(rv.Credits, CreditView{Label: c.Label, Enabled: c.Enabled, Text: creditText(c)})
		}
		v.Reports = append(v.Reports, rv)
		for _, row := range summaryRowReports(r) {
			v.Summary = append(v.Summary, buildSummaryRow(row.name, row.report, now))
		}
	}
	return v
}

func buildWindowView(w Window, now time.Time) WindowView {
	wp := paceForWindow(w, now)
	wv := WindowView{
		Label:           w.Label,
		Scope:           w.Scope,
		Title:           windowTitle(w),
		Detail:          w.Detail,
		UsedPercent:     w.UsedPercent,
		ExpectedPercent: wp.elapsedPercent,
		PaceRatio:       wp.ratio,
		PaceKnown:       wp.pace != paceUnknown,
		UsedSeverity:    usedSeverity(w.UsedPercent),
	}
	if wv.PaceKnown {
		wv.PaceLabel = paceLabel(wp.pace)
	}
	if !w.ResetsAt.IsZero() {
		wv.ResetUnix = w.ResetsAt.Unix()
		wv.ResetAbsolute = w.ResetsAt.Local().Format("Mon 2 Jan 15:04 MST")
		if rem := w.ResetsAt.Sub(now); rem > 0 {
			wv.ResetRelative = "in " + humanDuration(rem)
		}
	}
	return wv
}

func buildResetView(r Reset, now time.Time) ResetView {
	rv := ResetView{
		Label:  r.Label,
		Left:   r.Left,
		Total:  r.Total,
		Usable: r.Usable,
		Status: r.Status,
		Text:   resetText(r, now),
	}
	if !r.ExpiresAt.IsZero() {
		rv.ExpiresUnix = r.ExpiresAt.Unix()
		rv.ExpiresAbsolute = r.ExpiresAt.Local().Format("Mon 2 Jan 15:04 MST")
	}
	return rv
}

func buildSummaryRow(name string, r Report, now time.Time) SummaryRow {
	return SummaryRow{
		Provider:   name,
		FiveHour:   buildSummaryCell(r, summaryFiveHour, now),
		WeekGlobal: buildSummaryCell(r, summaryWeekGlobal, now),
	}
}

func buildSummaryCell(r Report, kind summaryWindowKind, now time.Time) SummaryCell {
	text, used, p, ok := summaryCellContent(r, kind, now)
	if !ok {
		return SummaryCell{Present: false, Text: "--", Severity: SevDefault}
	}
	w, _ := summaryWindow(r.Windows, kind)
	wp := paceForWindow(w, now)
	return SummaryCell{
		Present:         true,
		UsedPercent:     used,
		ExpectedPercent: wp.elapsedPercent,
		Severity:        summarySeverityToView(summaryCellSeverity(used, p)),
		Text:            text,
	}
}

// usedSeverity mirrors the CLI detailed bar thresholds.
// Thresholds mirror barStyleForUsed in format.go.
func usedSeverity(p float64) Severity {
	switch {
	case p >= 90:
		return SevDanger
	case p >= 75:
		return SevWarn
	case p >= 50:
		return SevInfo
	default:
		return SevOK
	}
}

func summarySeverityToView(s summarySeverity) Severity {
	switch s {
	case sevUnder:
		return SevOK
	case sevWarn:
		return SevWarn
	case sevDanger:
		return SevDanger
	default:
		return SevDefault
	}
}
