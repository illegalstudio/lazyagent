package ui

import tea "github.com/charmbracelet/bubbletea"

// fileWatchMsg is sent when any JSONL file in ~/.claude/projects changes.
type fileWatchMsg struct{}

// watchCmd returns a tea.Cmd that blocks until the next file change event.
func watchCmd(events <-chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-events
		return fileWatchMsg{}
	}
}
