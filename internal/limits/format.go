package limits

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/chatops"
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

// bar returns a width-wide progress bar for percent (0-100). It always uses
// the same characters; styling is applied in the caller, so the same
// function can produce a colored or plain bar.
func bar(percent float64, width int) (filled, empty string) {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	n := int(percent/100*float64(width) + 0.5)
	return strings.Repeat("█", n), strings.Repeat("░", width-n)
}

// renderBar joins the filled and empty halves with optional styling. When stdout
// is not a TTY (piped, redirected) all styling is dropped — the function still
// returns the same characters, just unstyled.
func renderBar(percent float64, width int, filledStyle lipgloss.Style) string {
	filled, empty := bar(percent, width)
	if !chatops.StdoutIsTTY {
		return filled + empty
	}
	return filledStyle.Render(filled) + chatops.StyleMuted.Render(empty)
}

// barStyleForUsed returns the color to use on the *Used* bar based on severity.
// Elapsed bar always uses muted styling (it's pure information, not a metric).
func barStyleForUsed(percent float64) lipgloss.Style {
	switch {
	case percent >= 90:
		return lipgloss.NewStyle().Foreground(chatops.ColDanger)
	case percent >= 75:
		return lipgloss.NewStyle().Foreground(chatops.ColWarn)
	case percent >= 50:
		return lipgloss.NewStyle().Foreground(chatops.ColPrimary)
	default:
		return lipgloss.NewStyle().Foreground(chatops.ColZen)
	}
}

// styleForPace colors the pace label so the eye can scan a multi-window report
// and immediately spot whichever line is in trouble.
func styleForPace(p pace) lipgloss.Style {
	switch p {
	case paceUnder:
		return lipgloss.NewStyle().Foreground(chatops.ColTextSubtle)
	case paceOnTrack:
		return lipgloss.NewStyle().Foreground(chatops.ColZen)
	case paceOver:
		return lipgloss.NewStyle().Foreground(chatops.ColDanger).Bold(true)
	default:
		return chatops.StyleMuted
	}
}

// styled wraps s in style.Render() only when stdout is a TTY. Keeps piped/redirected
// output free of ANSI escape sequences while letting interactive terminals show colors.
func styled(s string, style lipgloss.Style) string {
	if !chatops.StdoutIsTTY {
		return s
	}
	return style.Render(s)
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

// resetsLine formats the "Resets: in 3h 17m (Thu 30 Apr 20:10 CEST)" string.
// The relative time is the load-bearing piece; the absolute time is muted.
func resetsLine(resetsAt, now time.Time) string {
	if resetsAt.IsZero() {
		return styled("reset time unknown", chatops.StyleMuted)
	}
	remaining := resetsAt.Sub(now)
	if remaining <= 0 {
		return styled("window expired", chatops.StyleMuted)
	}
	relative := "in " + humanDuration(remaining)
	absolute := resetsAt.Local().Format("Mon 2 Jan 15:04 MST")
	return relative + " " + styled("("+absolute+")", chatops.StyleMuted)
}

const barWidth = 20

// styleProvider renders a provider header (e.g. "Claude Code"). Bold + primary
// in a TTY, plain in a pipe.
var styleProvider = lipgloss.NewStyle().Bold(true).Foreground(chatops.ColPrimary)

// styleWindowLabel renders e.g. "5-hour window".
var styleWindowLabel = lipgloss.NewStyle().Foreground(chatops.ColTextBright).Bold(true)

// styleNote renders the disclaimer / source lines under each report.
var styleNote = chatops.StyleFooter

// renderReport writes a human-readable view of one Report to b.
func renderReport(b *strings.Builder, r Report, now time.Time) {
	fmt.Fprintf(b, "%s\n", styled(r.Provider, styleProvider))
	for _, w := range r.Windows {
		ep := elapsedPercent(w.WindowMinutes, w.ResetsAt, now)
		ratio, p := classifyPace(w.UsedPercent, ep)

		fmt.Fprintf(b, "\n  %s\n", styled(w.Label+" window", styleWindowLabel))
		fmt.Fprintf(b, "    Used:     %5.1f%%  %s\n",
			w.UsedPercent,
			renderBar(w.UsedPercent, barWidth, barStyleForUsed(w.UsedPercent)),
		)
		fmt.Fprintf(b, "    Elapsed:  %5.1f%%  %s\n",
			ep,
			renderBar(ep, barWidth, lipgloss.NewStyle().Foreground(chatops.ColTextSubtle)),
		)
		fmt.Fprintf(b, "    Resets:   %s\n", resetsLine(w.ResetsAt, now))

		if p == paceUnknown {
			fmt.Fprintf(b, "    Pace:     %s\n",
				styled("— (window just reset)", chatops.StyleMuted),
			)
		} else {
			fmt.Fprintf(b, "    Pace:     %s %s\n",
				styled(paceLabel(p), styleForPace(p)),
				styled(fmt.Sprintf("(%.2f× of expected %.1f%%)", ratio, ep), chatops.StyleMuted),
			)
		}
	}
	if r.Source != "" || r.Note != "" {
		b.WriteString("\n")
	}
	if r.Source != "" {
		fmt.Fprintf(b, "  %s\n", styled(r.Source, styleNote))
	}
	if r.Note != "" {
		fmt.Fprintf(b, "  %s\n", styled(r.Note, styleNote))
	}
}
