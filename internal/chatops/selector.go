package chatops

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// Agent describes a selectable entry in the interactive picker.
type Agent struct {
	Key   string         // machine id, e.g. "claude"
	Label string         // human label, e.g. "Claude Code"
	Color lipgloss.Color // identity dot color
}

// PickAgents runs a bordered multi-select picker. It returns the chosen
// Agent.Key values. `subtitle` lets callers vary the helper text between
// prune and compact (e.g. "Only agents with plain-text file storage").
func PickAgents(agents []Agent, title, subtitle string) ([]string, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, fmt.Errorf("--agent is required when stdin is not a terminal")
	}
	if len(agents) == 0 {
		return nil, fmt.Errorf("no supported agents available")
	}
	// Single-agent shortcut: skip the picker entirely and return it
	// pre-selected — making the user confirm a single option is rude.
	if len(agents) == 1 {
		return []string{agents[0].Key}, nil
	}

	m := selectorModel{
		agents:   agents,
		selected: make([]bool, len(agents)),
		title:    title,
		subtitle: subtitle,
	}
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
			out = append(out, result.agents[i].Key)
		}
	}
	return out, nil
}

type selectorModel struct {
	agents    []Agent
	cursor    int
	selected  []bool
	cancelled bool
	title     string
	subtitle  string
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
			return m, tea.Quit
		}
	}
	return m, nil
}

var (
	selTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColDarkText).
			Background(ColPrimary).
			Padding(0, 2).
			MarginBottom(1)

	selSubtitleStyle = lipgloss.NewStyle().
				Italic(true).
				Foreground(ColTextSubtle).
				MarginBottom(1)

	selPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColTextDim).
			Padding(1, 2)

	selHelpBar = lipgloss.NewStyle().
			Foreground(ColTextSubtle).
			MarginTop(1)

	selHelpKey = lipgloss.NewStyle().
			Foreground(ColDarkText).
			Background(ColKeyBg).
			Padding(0, 1)
)

const selLabelWidth = 22

func (m selectorModel) View() string {
	title := selTitleStyle.Render(m.title)
	subtitle := selSubtitleStyle.Render(m.subtitle)

	rows := make([]string, 0, len(m.agents))
	for i, a := range m.agents {
		rows = append(rows, m.renderRow(i, a))
	}

	help := strings.Join([]string{
		selHelpKey.Render("↑/↓") + " move",
		selHelpKey.Render("space") + " toggle",
		selHelpKey.Render("a") + " toggle all",
		selHelpKey.Render("enter") + " confirm",
		selHelpKey.Render("q") + " cancel",
	}, "   ")

	return "\n" + strings.Join([]string{
		title,
		subtitle,
		selPanelStyle.Render(strings.Join(rows, "\n")),
		selHelpBar.Render(help),
	}, "\n") + "\n"
}

func (m selectorModel) renderRow(i int, a Agent) string {
	focused := i == m.cursor
	checked := m.selected[i]

	arrow := " "
	if focused {
		arrow = "▸"
	}
	arrowStyle := lipgloss.NewStyle().Foreground(ColTextBright).Bold(true)

	box := "○"
	boxFg := ColTextDim
	if checked {
		box = "●"
		boxFg = ColZen
	}
	boxStyle := lipgloss.NewStyle().Foreground(boxFg).Bold(checked)

	dotStyle := lipgloss.NewStyle().Foreground(a.Color).Bold(true)

	labelFg := ColTextDim
	if checked || focused {
		labelFg = ColTextBright
	}
	labelStyle := lipgloss.NewStyle().
		Foreground(labelFg).
		Width(selLabelWidth).
		Padding(0, 1)
	if focused {
		labelStyle = labelStyle.Background(ColHighlightBg).Bold(true)
	}

	keyStyle := lipgloss.NewStyle().Foreground(ColTextDim).Italic(true)

	return fmt.Sprintf("%s %s  %s  %s  %s",
		arrowStyle.Render(arrow),
		boxStyle.Render(box),
		dotStyle.Render("●"),
		labelStyle.Render(a.Label),
		keyStyle.Render(a.Key),
	)
}
