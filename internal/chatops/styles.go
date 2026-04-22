// Package chatops holds CLI helpers shared between the `prune` and `compact`
// subcommands: the interactive agent picker, styled output (tables,
// disclaimer and zen boxes), confirmation prompts, and path-safety checks.
package chatops

import (
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Shared palette. Kept small on purpose: individual commands can layer extra
// colors on top, but anything that ends up on both prune and compact lives
// here so the two subcommands look like siblings.
var (
	ColDanger      = lipgloss.Color("#F38BA8")
	ColWarn        = lipgloss.Color("#FAB387")
	ColZen         = lipgloss.Color("#A6E3A1")
	ColZenAccent   = lipgloss.Color("#94E2D5")
	ColPrimary     = lipgloss.Color("#B4BEFE")
	ColTextBright  = lipgloss.Color("#CDD6F4")
	ColTextSubtle  = lipgloss.Color("#94A3B8")
	ColTextDim     = lipgloss.Color("#6C7086")
	ColBorderDim   = lipgloss.Color("#45475A")
	ColHighlightBg = lipgloss.Color("#313244")
	ColDarkText    = lipgloss.Color("#1E1E2E")
	ColKeyBg       = lipgloss.Color("#585B70")

	StyleTableHeader = lipgloss.NewStyle().Bold(true).Foreground(ColPrimary)
	StyleAgent       = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA"))
	StyleCount       = lipgloss.NewStyle().Foreground(lipgloss.Color("#F9E2AF")).Bold(true)
	StyleMuted       = lipgloss.NewStyle().Foreground(ColTextDim)
	StyleFooter      = lipgloss.NewStyle().Italic(true).Foreground(ColTextSubtle)
	StyleBorder      = lipgloss.NewStyle().Foreground(ColBorderDim)
)

// StdoutIsTTY is true when stdout is a real terminal. Callers use this to
// degrade styling for piped/redirected output (e.g. drop borders).
var StdoutIsTTY = isatty.IsTerminal(os.Stdout.Fd())
