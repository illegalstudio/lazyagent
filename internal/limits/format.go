package limits

import (
	"fmt"
	"strings"
	"time"
)

// Window holds a single rate-limit window's state, normalized across providers.
type Window struct {
	Label         string    // "5-hour" / "7-day"
	WindowMinutes int       // 300 / 10080
	UsedPercent   float64   // 0-100
	ResetsAt      time.Time // when the window rolls over
}

// Report is the data we render for one provider.
type Report struct {
	Provider string   // "Claude Code" / "Codex"
	Source   string   // short note shown before the disclaimer (provenance)
	Windows  []Window // 5h, 7d, ...
	Note     string   // disclaimer (may be empty)
}

// pace classifies how the current consumption compares to a perfectly linear pace.
type pace int

const (
	paceUnknown pace = iota
	paceUnder
	paceOnTrack
	paceOver
)

// classifyPace returns (ratio, label).
//   - ratio = usedPercent / elapsedPercent. <1 means we're consuming slower than linear.
//   - label is human-readable: "underutilizing", "on track", "overutilizing".
//
// If elapsedPercent is too small (window just reset) the ratio isn't meaningful.
func classifyPace(usedPercent, elapsedPercent float64) (float64, pace) {
	if elapsedPercent < 1.0 {
		// Less than 1% into the window: not enough time elapsed to judge pace.
		return 0, paceUnknown
	}
	ratio := usedPercent / elapsedPercent
	switch {
	case ratio < 0.85:
		return ratio, paceUnder
	case ratio > 1.15:
		return ratio, paceOver
	default:
		return ratio, paceOnTrack
	}
}

func paceLabel(p pace) string {
	switch p {
	case paceUnder:
		return "underutilizing"
	case paceOnTrack:
		return "on track"
	case paceOver:
		return "overutilizing"
	default:
		return "—"
	}
}

// elapsedPercent returns how far we are into the window (0-100).
// now is passed explicitly for testability.
func elapsedPercent(windowMinutes int, resetsAt, now time.Time) float64 {
	if windowMinutes <= 0 || resetsAt.IsZero() {
		return 0
	}
	total := float64(windowMinutes)
	remaining := resetsAt.Sub(now).Minutes()
	if remaining < 0 {
		// Window already past its reset (server clock skew or stale data).
		return 100
	}
	if remaining > total {
		// Reset further out than the window itself — shouldn't happen, clamp.
		return 0
	}
	elapsed := total - remaining
	return elapsed / total * 100
}

func bar(percent float64, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	filled := int(percent/100*float64(width) + 0.5)
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// humanDuration formats a positive duration as "1d 3h", "4h 23m", or "12m".
func humanDuration(d time.Duration) string {
	if d < 0 {
		return "0m"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, mins)
	default:
		return fmt.Sprintf("%dm", mins)
	}
}

// renderReport writes a human-readable view of one Report to b.
func renderReport(b *strings.Builder, r Report, now time.Time) {
	fmt.Fprintf(b, "%s\n", r.Provider)
	for _, w := range r.Windows {
		ep := elapsedPercent(w.WindowMinutes, w.ResetsAt, now)
		ratio, p := classifyPace(w.UsedPercent, ep)

		fmt.Fprintf(b, "\n  %s window\n", w.Label)
		fmt.Fprintf(b, "    Used:    %5.1f%%  %s\n", w.UsedPercent, bar(w.UsedPercent, 20))
		fmt.Fprintf(b, "    Elapsed: %5.1f%%  %s\n", ep, elapsedDetail(w.ResetsAt, now))

		if p == paceUnknown {
			fmt.Fprintf(b, "    Pace:    — (window just reset)\n")
		} else {
			fmt.Fprintf(b, "    Pace:    %s (%.2f× of expected %.1f%%)\n", paceLabel(p), ratio, ep)
		}
	}
	if r.Source != "" || r.Note != "" {
		b.WriteString("\n")
	}
	if r.Source != "" {
		fmt.Fprintf(b, "  %s\n", r.Source)
	}
	if r.Note != "" {
		fmt.Fprintf(b, "  %s\n", r.Note)
	}
}

func elapsedDetail(resetsAt, now time.Time) string {
	if resetsAt.IsZero() {
		return "reset time unknown"
	}
	remaining := resetsAt.Sub(now)
	if remaining <= 0 {
		return "window expired"
	}
	return fmt.Sprintf("resets in %s (%s)",
		humanDuration(remaining),
		resetsAt.Local().Format("Mon 2 Jan 15:04 MST"),
	)
}
