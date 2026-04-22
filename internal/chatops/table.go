package chatops

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

// NewTable returns a lipgloss table pre-wired with the shared style.
// Callers set headers and rows, then pass to fmt.Println. When stdout is
// not a tty the border is hidden for cleaner piped output.
func NewTable() *table.Table {
	border := lipgloss.RoundedBorder()
	if !StdoutIsTTY {
		border = lipgloss.HiddenBorder()
	}
	return table.New().
		Border(border).
		BorderStyle(StyleBorder).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return StyleTableHeader.Padding(0, 1)
			}
			return lipgloss.NewStyle().Padding(0, 1)
		})
}
