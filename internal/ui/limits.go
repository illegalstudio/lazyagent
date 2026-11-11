package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/limits"
)

const (
	limitsTabSummary  = 0
	limitsTabDetailed = 1
)

// limitsSeverityColor maps a computed severity to a TUI theme color.
func (m Model) limitsSeverityColor(sev limits.Severity) lipgloss.Color {
	switch sev {
	case limits.SevOK:
		return m.theme.Accent
	case limits.SevInfo:
		return m.theme.Primary
	case limits.SevWarn:
		return m.theme.Warning
	case limits.SevDanger:
		return m.theme.Danger
	default:
		return m.theme.Text
	}
}

// renderLimitsModal renders the centered limits overlay (tabs + body + hint).
func (m Model) renderLimitsModal() string {
	width := m.width - 4
	if width > 80 {
		width = 80
	}
	if width < 24 {
		width = 24
	}

	maxBodyH := m.height - 10
	if maxBodyH < 3 {
		maxBodyH = 3
	}

	var bodyLines []string
	switch {
	case m.limitsLoading:
		bodyLines = []string{"", lipgloss.NewStyle().Foreground(m.theme.Subtext).Render("  Loading limits…")}
	case !m.limitsView.Available:
		bodyLines = []string{"", lipgloss.NewStyle().Foreground(m.theme.Subtext).Render("  No supported agents detected.")}
	case m.limitsTab == limitsTabSummary:
		bodyLines = m.renderLimitsSummaryLines()
	default:
		bodyLines = m.renderLimitsDetailedLines()
	}

	scroll := 0
	if m.limitsTab == limitsTabDetailed {
		scroll = m.limitsScroll
		if maxScroll := len(bodyLines) - maxBodyH; scroll > maxScroll {
			scroll = maxScroll
		}
		if scroll < 0 {
			scroll = 0
		}
	}
	visible, moreBelow := windowLines(bodyLines, scroll, maxBodyH)
	body := strings.Join(visible, "\n")

	title := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render("Limits")
	tabs := m.renderLimitsTabs()
	hint := m.renderLimitsHint(moreBelow)
	content := title + "\n" + tabs + "\n\n" + body + "\n\n" + hint

	// Truncate every line to the modal's inner width so nothing wraps. A wrapped
	// line would render taller than its budgeted single row and push the box
	// past the screen height (long Source/Note disclaimers on Detailed, or the
	// hint line on narrow terminals).
	content = truncateLinesToWidth(content, width-4)

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.BorderFocus).
		Background(m.theme.ModalBg).
		Foreground(m.theme.Text).
		Padding(1, 2).
		Width(width).
		MaxHeight(m.height).
		Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
		lipgloss.WithWhitespaceBackground(m.theme.OverlayBg),
	)
}

func (m Model) renderLimitsTabs() string {
	mk := func(label string, active bool) string {
		if active {
			return lipgloss.NewStyle().Foreground(m.theme.Text).Background(m.theme.SelectionBg).Bold(true).Padding(0, 1).Render(label)
		}
		return lipgloss.NewStyle().Foreground(m.theme.Subtext).Padding(0, 1).Render(label)
	}
	return mk("Summary", m.limitsTab == limitsTabSummary) + "  " + mk("Detailed", m.limitsTab == limitsTabDetailed)
}

func (m Model) renderLimitsHint(moreBelow bool) string {
	h := "tab/←→ switch · j/k scroll · r refresh · l/esc close"
	if moreBelow {
		h = "↓ more · " + h
	}
	return lipgloss.NewStyle().Foreground(m.theme.Muted).Render(h)
}

func (m Model) renderLimitsSummaryLines() []string {
	lines := []string{
		lipgloss.NewStyle().Foreground(m.theme.Subtext).Bold(true).Render(
			fmt.Sprintf("  %-14s  %-24s  %-24s", "Agent", "5h", "Week / Global")),
	}
	for _, row := range m.limitsView.Summary {
		prov := lipgloss.NewStyle().Foreground(m.theme.Text).Render(fmt.Sprintf("%-14s", row.Provider))
		lines = append(lines, "  "+prov+"  "+m.summaryCellText(row.FiveHour)+"  "+m.summaryCellText(row.WeekGlobal))
	}
	return lines
}

func (m Model) summaryCellText(c limits.SummaryCell) string {
	padded := fmt.Sprintf("%-24s", c.Text)
	return lipgloss.NewStyle().Foreground(m.limitsSeverityColor(c.Severity)).Render(padded)
}

func (m Model) renderLimitsDetailedLines() []string {
	const barW = 16
	var lines []string
	for i, r := range m.limitsView.Reports {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, lipgloss.NewStyle().Foreground(m.theme.Primary).Bold(true).Render(r.Provider))
		for _, w := range r.Windows {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(w.Title))
			lines = append(lines, fmt.Sprintf("    Used:     %5.1f%%  %s",
				w.UsedPercent, m.limitsBar(w.UsedPercent, barW, m.limitsSeverityColor(w.UsedSeverity))))
			lines = append(lines, fmt.Sprintf("    Expected: %5.1f%%  %s",
				w.ExpectedPercent, m.limitsBar(w.ExpectedPercent, barW, m.theme.Subtext)))

			reset := "reset time unknown"
			if w.ResetRelative != "" {
				reset = w.ResetRelative + " (" + w.ResetAbsolute + ")"
			} else if w.ResetAbsolute != "" {
				reset = w.ResetAbsolute
			}
			lines = append(lines, "    Resets:   "+lipgloss.NewStyle().Foreground(m.theme.Subtext).Render(reset))

			pace := "— (window just reset)"
			paceColor := m.theme.Muted
			if w.PaceKnown {
				pace = fmt.Sprintf("%s (%.2f× of expected %.1f%%)", w.PaceLabel, w.PaceRatio, w.ExpectedPercent)
				switch w.PaceLabel {
				case "overutilizing":
					paceColor = m.theme.Danger
				case "on track":
					paceColor = m.theme.Accent
				default:
					paceColor = m.theme.Subtext
				}
			}
			lines = append(lines, "    Pace:     "+lipgloss.NewStyle().Foreground(paceColor).Render(pace))
			if w.Detail != "" {
				lines = append(lines, "    Detail:   "+lipgloss.NewStyle().Foreground(m.theme.Subtext).Render(w.Detail))
			}
		}
		lines = append(lines, m.renderLimitsExtrasLines(r)...)
		if r.Source != "" {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(m.theme.Muted).Render(r.Source))
		}
		if r.Note != "" {
			lines = append(lines, "  "+lipgloss.NewStyle().Foreground(m.theme.Muted).Render(r.Note))
		}
	}
	return lines
}

// renderLimitsExtrasLines renders the Resets and Credits sections of one
// report: label in the text color, the one-line value muted. A reset usable
// right now is highlighted with the accent color.
func (m Model) renderLimitsExtrasLines(r limits.ReportView) []string {
	var lines []string
	heading := func(title string) string {
		return "  " + lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(title)
	}
	if len(r.Resets) > 0 {
		lines = append(lines, heading("Resets"))
		for _, rs := range r.Resets {
			color := m.theme.Subtext
			if rs.Usable {
				color = m.theme.Accent
			}
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(m.theme.Text).Render(rs.Label)+"  "+
				lipgloss.NewStyle().Foreground(color).Render(rs.Text))
		}
	}
	if len(r.Credits) > 0 {
		lines = append(lines, heading("Credits"))
		for _, c := range r.Credits {
			lines = append(lines, "    "+lipgloss.NewStyle().Foreground(m.theme.Text).Render(c.Label)+"  "+
				lipgloss.NewStyle().Foreground(m.theme.Subtext).Render(c.Text))
		}
	}
	return lines
}

func (m Model) limitsBar(percent float64, w int, color lipgloss.Color) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	n := int(percent/100*float64(w) + 0.5)
	if n > w {
		n = w
	}
	filled := lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("█", n))
	empty := lipgloss.NewStyle().Foreground(m.theme.Muted).Render(strings.Repeat("░", w-n))
	return filled + empty
}

// truncateLinesToWidth truncates each line of s to at most width display cells,
// in an ANSI-aware way, so styled lines never wrap inside the modal. Lines that
// already fit are left untouched.
func truncateLinesToWidth(s string, width int) string {
	if width < 1 {
		width = 1
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if lipgloss.Width(ln) > width {
			lines[i] = lipgloss.NewStyle().MaxWidth(width).Render(ln)
		}
	}
	return strings.Join(lines, "\n")
}

// windowLines returns the slice of lines visible at the given scroll offset and
// whether more lines exist below the window.
func windowLines(lines []string, offset, height int) (visible []string, moreBelow bool) {
	if offset < 0 {
		offset = 0
	}
	if offset > len(lines) {
		offset = len(lines)
	}
	end := offset + height
	if end >= len(lines) {
		return lines[offset:], false
	}
	return lines[offset:end], true
}
