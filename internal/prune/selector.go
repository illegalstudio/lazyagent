package prune

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-isatty"
)

// pickAgents runs a small Bubbletea multi-select so the user can pick which
// supported agents to prune when --agent isn't provided on the CLI.
func pickAgents() ([]string, error) {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, fmt.Errorf("--agent is required when stdin is not a terminal")
	}

	m := selectorModel{
		agents: SupportedAgents,
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
			out = append(out, result.agents[i])
		}
	}
	return out, nil
}

type selectorModel struct {
	agents    []string
	cursor    int
	selected  []bool
	cancelled bool
	confirmed bool
}

func (m selectorModel) Init() tea.Cmd {
	if m.selected == nil {
		m.selected = make([]bool, len(m.agents))
	}
	return nil
}

func (m selectorModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.selected == nil {
		m.selected = make([]bool, len(m.agents))
	}
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
			// Toggle-all: if everything's selected, deselect; else select all.
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
			m.confirmed = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectorModel) View() string {
	title := lipgloss.NewStyle().Bold(true).Render("Select agents to prune")
	hint := lipgloss.NewStyle().Faint(true).Render("↑/↓ move · space toggle · a toggle-all · enter confirm · q cancel")

	var b strings.Builder
	b.WriteString(title + "\n")
	b.WriteString(hint + "\n\n")

	for i, agent := range m.agents {
		cursor := "  "
		if i == m.cursor {
			cursor = "> "
		}
		box := "[ ]"
		if i < len(m.selected) && m.selected[i] {
			box = "[x]"
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, box, agent))
	}
	return b.String()
}
