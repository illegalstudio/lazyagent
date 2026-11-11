package limits

import (
	"context"
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
	Resets    []resetJSON  `json:"resets"`
	Credits   []creditJSON `json:"credits"`
	Summary   summaryJSON  `json:"summary"`
}

// resetJSON is a banked limit reset the account holds. lazyagent never
// redeems one; this only reports what the provider says is available.
type resetJSON struct {
	Label            string     `json:"label"`
	Left             int        `json:"left"`
	Total            int        `json:"total"` // 0 when the provider does not say
	Usable           bool       `json:"usable"`
	Status           string     `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at"`         // null when unknown
	ExpiresInSeconds int64      `json:"expires_in_seconds"` // 0 when unknown or passed
	// A spent recurring reset (Claude's weekly 5-hour reset) comes back here.
	NextAvailableAt        *time.Time `json:"next_available_at"`         // null when unknown
	NextAvailableInSeconds int64      `json:"next_available_in_seconds"` // 0 when unknown or passed
	Text                   string     `json:"text"`
}

// creditJSON is a balance or spend pool beyond the included usage. Amounts
// are in unit (an ISO currency code or "credits") and null when unreported.
type creditJSON struct {
	Label   string   `json:"label"`
	Unit    string   `json:"unit"`
	Enabled bool     `json:"enabled"`
	Balance *float64 `json:"balance"`
	Used    *float64 `json:"used"`
	Limit   *float64 `json:"limit"`
	Status  string   `json:"status"`
	Text    string   `json:"text"`
}

type windowJSON struct {
	Label           string     `json:"label"`
	Scope           string     `json:"scope"`  // model or feature the window is limited to, "" when plan-wide
	Detail          string     `json:"detail"` // optional provider context, "" when none
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
		Resets:    make([]resetJSON, 0, len(r.Resets)),
		Credits:   make([]creditJSON, 0, len(r.Credits)),
		Summary: summaryJSON{
			FiveHour:   buildSummaryCellJSON(r, summaryFiveHour, now),
			WeekGlobal: buildSummaryCellJSON(r, summaryWeekGlobal, now),
		},
	}
	for _, w := range r.Windows {
		rj.Windows = append(rj.Windows, buildWindowJSON(w, now))
	}
	for _, rs := range r.Resets {
		rsj := resetJSON{
			Label:  rs.Label,
			Left:   rs.Left,
			Total:  rs.Total,
			Usable: rs.Usable,
			Status: rs.Status,
			Text:   resetText(rs, now),
		}
		if !rs.ExpiresAt.IsZero() {
			expires := rs.ExpiresAt
			rsj.ExpiresAt = &expires
			if rem := expires.Sub(now); rem > 0 {
				rsj.ExpiresInSeconds = int64(rem.Seconds())
			}
		}
		if !rs.NextAt.IsZero() {
			next := rs.NextAt
			rsj.NextAvailableAt = &next
			if rem := next.Sub(now); rem > 0 {
				rsj.NextAvailableInSeconds = int64(rem.Seconds())
			}
		}
		rj.Resets = append(rj.Resets, rsj)
	}
	for _, c := range r.Credits {
		rj.Credits = append(rj.Credits, creditJSON{
			Label:   c.Label,
			Unit:    c.Unit,
			Enabled: c.Enabled,
			Balance: c.Balance,
			Used:    c.Used,
			Limit:   c.Limit,
			Status:  c.Status,
			Text:    creditText(c),
		})
	}
	return rj
}

func buildWindowJSON(w Window, now time.Time) windowJSON {
	wv := buildWindowView(w, now)
	wj := windowJSON{
		Label:           wv.Label,
		Scope:           w.Scope,
		Detail:          w.Detail,
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

// Snapshot is one fetch of a set of agents, kept so it can be rendered as the
// `lazyagent limits --json` document at any later instant. Rendering is
// separate from fetching so a cached snapshot still reports live countdowns
// (reset_in_seconds, expires_in_seconds) instead of the ones at fetch time.
type Snapshot struct {
	results []agentReport
	errs    []agentError
}

// CheckAgent reports whether agent is a selector Collect accepts ("all", or
// one of claude, codex, grok, kimi, cursor), with the CLI's error otherwise.
func CheckAgent(agent string) error {
	_, err := resolveAgents(agent)
	return err
}

// Collect fetches agent ("all", or one of claude, codex, grok, kimi, cursor)
// exactly as `lazyagent limits --json` does. The only error is an unsupported
// agent name; per-agent failures land in the document's "errors" array.
func Collect(ctx context.Context, agent string) (Snapshot, error) {
	agents, err := resolveAgents(agent)
	if err != nil {
		return Snapshot{}, err
	}
	results, errs := collect(ctx, agents)
	return Snapshot{results: results, errs: errs}, nil
}

// JSON returns the `lazyagent limits --json` document for this snapshot as of
// now, ready for json.Marshal.
func (s Snapshot) JSON(now time.Time) any {
	return buildJSON(s.results, s.errs, now)
}
