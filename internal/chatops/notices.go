package chatops

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleDisclaimerBox = lipgloss.NewStyle().
				Border(lipgloss.ThickBorder()).
				BorderForeground(ColDanger).
				Foreground(ColWarn).
				Padding(1, 2).
				Margin(1, 0)

	styleDisclaimerTitle = lipgloss.NewStyle().
				Bold(true).
				Foreground(ColDanger).
				Underline(true)

	styleZenBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColZen).
			Foreground(ColZen).
			Padding(1, 3).
			Margin(1, 0).
			Align(lipgloss.Center)

	styleZenTitle = lipgloss.NewStyle().Bold(true).Foreground(ColZen)
	styleZenQuote = lipgloss.NewStyle().Italic(true).Foreground(ColZenAccent)
)

// PrintDestructiveDisclaimer prints a prominent red/orange warning box before
// the user is asked to confirm a destructive action. The title and body are
// caller-provided so `prune` and `compact` can vary the language.
func PrintDestructiveDisclaimer(title string, body string) {
	rendered := styleDisclaimerTitle.Render("⚠  " + title + "  ⚠")
	fmt.Fprintln(os.Stderr, styleDisclaimerBox.Render(rendered+"\n\n"+body))
}

// PrintZenBox prints a calm green panel. Used as the "nothing to do" state
// where no candidates matched the filters.
func PrintZenBox(title, body, quote string) {
	parts := []string{styleZenTitle.Render(title), "", body}
	if quote != "" {
		parts = append(parts, "", styleZenQuote.Render(quote))
	}
	fmt.Println(styleZenBox.Render(strings.Join(parts, "\n")))
}

// Confirm reads a y/N answer from stdin. Any input other than "y"/"yes"
// (case-insensitive) — including read errors — is treated as a no.
func Confirm(prompt string) bool {
	fmt.Printf("%s [y/N]: ", prompt)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes"
}

// HumanBytes renders a byte count with a binary suffix (KiB/MiB/GiB/…).
func HumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	suffix := "KMGTPE"[exp]
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), suffix)
}
