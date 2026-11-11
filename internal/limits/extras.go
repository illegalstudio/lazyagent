package limits

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Reset is a limit reset the account holds and could redeem in the provider's
// own client. lazyagent only reports resets; it never redeems one.
type Reset struct {
	Label     string    // provider wording: "Full reset", "5-hour limit reset", ...
	Left      int       // resets still available under this grant
	Total     int       // resets granted in total; 0 when the provider does not say
	ExpiresAt time.Time // when unused resets lapse; zero when unknown
	NextAt    time.Time // when a spent recurring reset comes back; zero when unknown
	Usable    bool      // the provider says it can be redeemed right now
	Status    string    // why it cannot be redeemed now, or "" when usable or unknown
}

// Credit is a balance or spend pool beyond the plan's included usage, such as
// Claude extra usage, Codex credits, Cursor on-demand spend, or a Grok prepaid
// balance. Amounts are in Unit (an ISO currency code or "credits") and nil
// when the provider does not report them.
type Credit struct {
	Label   string
	Unit    string
	Enabled bool
	Balance *float64
	Used    *float64
	Limit   *float64
	Status  string // short qualifier: disabled reason, limit reached, estimates
}

func floatPtr(v float64) *float64 { return &v }

// resetText renders one reset as a single line: "1 of 1 left · expires in
// 118d 9h (Tue 9 Mar 08:59 CET) · usable now".
func resetText(r Reset, now time.Time) string {
	parts := []string{resetCountText(r)}
	if !r.ExpiresAt.IsZero() {
		parts = append(parts, "expires "+relativeAbsolute(r.ExpiresAt, now))
	}
	if !r.NextAt.IsZero() {
		parts = append(parts, "next available "+relativeAbsolute(r.NextAt, now))
	}
	switch {
	case r.Status != "":
		parts = append(parts, r.Status)
	case r.Usable:
		parts = append(parts, "usable now")
	}
	return strings.Join(parts, " · ")
}

func resetCountText(r Reset) string {
	if r.Total > 0 {
		return fmt.Sprintf("%d of %d left", r.Left, r.Total)
	}
	return fmt.Sprintf("%d left", r.Left)
}

// relativeAbsolute renders "in 3h 17m (Thu 30 Apr 20:10 CEST)", or only the
// absolute time once the instant has passed.
func relativeAbsolute(t, now time.Time) string {
	absolute := t.Local().Format("Mon 2 Jan 15:04 MST")
	if rem := t.Sub(now); rem > 0 {
		return "in " + humanDuration(rem) + " (" + absolute + ")"
	}
	return absolute
}

// creditText renders one credit pool as a single line, for example
// "28,315.67 credits", "€0.00 of €50.00 used", or "disabled · set by your
// organization".
func creditText(c Credit) string {
	var parts []string
	if !c.Enabled {
		parts = append(parts, "disabled")
	}
	switch {
	case c.Balance != nil:
		parts = append(parts, formatAmount(*c.Balance, c.Unit))
	case c.Used != nil && c.Limit != nil && *c.Limit > 0:
		parts = append(parts, fmt.Sprintf("%s of %s used", formatAmount(*c.Used, c.Unit), formatAmount(*c.Limit, c.Unit)))
	case c.Used != nil && (c.Enabled || *c.Used > 0):
		parts = append(parts, formatAmount(*c.Used, c.Unit)+" used")
	}
	if c.Balance != nil && c.Limit != nil && *c.Limit > 0 {
		parts = append(parts, "limit "+formatAmount(*c.Limit, c.Unit))
	}
	if c.Status != "" {
		parts = append(parts, c.Status)
	}
	if len(parts) == 0 {
		return "available"
	}
	return strings.Join(parts, " · ")
}

// formatAmount renders an amount in a currency or in credits with thousands
// separators: "$1,234.50", "€0.00", "28,315.67 credits", "12.00 GBP".
func formatAmount(v float64, unit string) string {
	sign := ""
	if v < 0 {
		sign = "-"
	}
	num := groupThousands(math.Abs(v))
	switch strings.ToUpper(unit) {
	case "USD":
		return sign + "$" + num
	case "EUR":
		return sign + "€" + num
	case "GBP":
		return sign + "£" + num
	case "", "CREDITS":
		return sign + num + " credits"
	default:
		return sign + num + " " + strings.ToUpper(unit)
	}
}

func groupThousands(v float64) string {
	neg := v < 0
	v = math.Abs(v)
	s := fmt.Sprintf("%.2f", v)
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, ch := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(ch)
	}
	out := b.String() + "." + frac
	if neg {
		out = "-" + out
	}
	return out
}

// humanReason turns a provider enum such as "cli_version" into readable
// words ("cli version"). Providers that send enums with a
// known meaning map them to a phrase first (see claudeDisabledReason).
func humanReason(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "_", " "))
}

// windowTitle is the heading the detailed views print for a window:
// "7-day window" or "7-day window (Fable)".
func windowTitle(w Window) string {
	if w.Scope != "" {
		return w.Label + " window (" + w.Scope + ")"
	}
	return w.Label + " window"
}

// renderExtras writes the Resets and Credits sections of the detailed CLI
// report. Labels are padded so the values line up within each section.
func renderExtras(b *strings.Builder, r Report, now time.Time) {
	if len(r.Resets) > 0 {
		fmt.Fprintf(b, "\n  %s\n", styled("Resets", styleWindowLabel))
		width := 0
		for _, rs := range r.Resets {
			width = max(width, len([]rune(rs.Label)))
		}
		for _, rs := range r.Resets {
			fmt.Fprintf(b, "    %-*s  %s\n", width, rs.Label, resetText(rs, now))
		}
	}
	if len(r.Credits) > 0 {
		fmt.Fprintf(b, "\n  %s\n", styled("Credits", styleWindowLabel))
		width := 0
		for _, c := range r.Credits {
			width = max(width, len([]rune(c.Label)))
		}
		for _, c := range r.Credits {
			fmt.Fprintf(b, "    %-*s  %s\n", width, c.Label, creditText(c))
		}
	}
}
