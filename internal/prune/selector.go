package prune

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// agentMeta drives the selector visuals: each supported agent gets a
// signature color that paints a little round "chip" next to its name.
type agentMeta struct {
	key   string
	label string
	color lipgloss.Color
}

var agentCatalog = []agentMeta{
	{key: "claude", label: "Claude Code", color: lipgloss.Color("#E7A15E")},
	{key: "pi", label: "pi coding agent", color: lipgloss.Color("#F38BA8")},
	{key: "codex", label: "Codex CLI", color: lipgloss.Color("#A6E3A1")},
}

// pickAgents renders a bordered multi-select so the user can pick which
// supported agents to prune when --agent isn't provided on the CLI.
func pickAgents() ([]string, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, fmt.Errorf("--agent is required when stdin is not a terminal")
	}

	m := newSelectorModel()
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))
	final, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("agent picker: %w", err)
	}
	result := final.(selectorModel)
	if result.cancelled {
		return nil, fmt.Errorf("aborted")
	}
	var out []string
	for i, chosen := range result.selected {
		if chosen {
			out = append(out, result.agents[i].key)
		}
	}
	return out, nil
}

type selectorModel struct {
	agents    []agentMeta
	cursor    int
	selected  []bool
	cancelled bool
	confirmed bool
}

func newSelectorModel() selectorModel {
	return selectorModel{
		agents:   agentCatalog,
		selected: make([]bool, len(agentCatalog)),
	}
}

func (m selectorModel) Init() tea.Cmd { return nil }

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			m.cancelled = true
			return m, tea.Quit
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.agents)-1 {
				m.cursor++
			}
		case " ", "x":
			m.selected[m.cursor] = !m.selected[m.cursor]
		case "a":
			allOn := true
			for _, v := range m.selected {
				if !v {
					allOn = false
					break
				}
			}
			for i := range m.selected {
				m.selected[i] = !allOn
			}
		case "enter":
			anySelected := false
			for _, v := range m.selected {
				if v {
					anySelected = true
					break
				}
			}
			if !anySelected {
				m.selected[m.cursor] = true
			}
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// ----- palette ------------------------------------------------------------

var (
	colHighlightBg = lipgloss.Color("#313244")
	colTextBright  = lipgloss.Color("#CDD6F4")
	colTextDim     = lipgloss.Color("#6C7086")
	colSubtle      = lipgloss.Color("#94A3B8")
	colCheckOn     = lipgloss.Color("#A6E3A1")

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1E1E2E")).
			Background(lipgloss.Color("#B4BEFE")).
			Padding(0, 2).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Italic(true).
			Foreground(colSubtle).
			MarginBottom(1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colTextDim).
			Padding(1, 2)

	helpBarStyle = lipgloss.NewStyle().
			Foreground(colSubtle).
			MarginTop(1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#1E1E2E")).
			Background(lipgloss.Color("#585B70")).
			Padding(0, 1)
)

// labelWidth is padded wide enough for "pi coding agent" (the longest label)
// plus a small breathing margin, so the highlight block stays the same
// rectangular size as the cursor moves between rows.
const labelWidth = 17

func (m selectorModel) View() string {
	title := titleStyle.Render("Select agents to prune")
	subtitle := subtitleStyle.Render("Only agents with plain-text file storage are shown.")

	rows := make([]string, 0, len(m.agents))
	for i, a := range m.agents {
		rows = append(rows, m.renderRow(i, a))
	}

	help := strings.Join([]string{
		helpKeyStyle.Render("↑/↓") + " move",
		helpKeyStyle.Render("space") + " toggle",
		helpKeyStyle.Render("a") + " toggle all",
		helpKeyStyle.Render("enter") + " confirm",
		helpKeyStyle.Render("q") + " cancel",
	}, "   ")

	return "\n" + strings.Join([]string{
		title,
		subtitle,
		panelStyle.Render(strings.Join(rows, "\n")),
		helpBarStyle.Render(help),
	}, "\n") + "\n"
}

// renderRow paints a single row. Only the agent's label carries the
// highlight background when the row is focused — the cursor arrow,
// checkbox and colored dot stay on the terminal's default background.
func (m selectorModel) renderRow(i int, a agentMeta) string {
	focused := i == m.cursor
	checked := m.selected[i]

	// Cursor arrow (blank slot for non-focused rows so widths align).
	arrow := " "
	if focused {
		arrow = "▸"
	}
	arrowStyle := lipgloss.NewStyle().
		Foreground(colTextBright).
		Bold(true)

	// Checkbox: filled when checked, hollow otherwise.
	box := "○"
	boxFg := colTextDim
	if checked {
		box = "●"
		boxFg = colCheckOn
	}
	boxStyle := lipgloss.NewStyle().
		Foreground(boxFg).
		Bold(checked)

	dotStyle := lipgloss.NewStyle().
		Foreground(a.color).
		Bold(true)

	// Label: bright when checked or focused, dim otherwise.
	// Only the focused row's label gets the highlight background, padded
	// to the widest label so the block stays rectangular as the cursor moves.
	labelFg := colTextDim
	if checked || focused {
		labelFg = colTextBright
	}
	labelStyle := lipgloss.NewStyle().
		Foreground(labelFg).
		Width(labelWidth).
		Padding(0, 1)
	if focused {
		labelStyle = labelStyle.Background(colHighlightBg).Bold(true)
	}

	keyStyle := lipgloss.NewStyle().
		Foreground(colTextDim).
		Italic(true)

	return fmt.Sprintf("%s %s  %s  %s  %s",
		arrowStyle.Render(arrow),
		boxStyle.Render(box),
		dotStyle.Render("●"),
		labelStyle.Render(a.label),
		keyStyle.Render(a.key),
	)
}
