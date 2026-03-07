package ui

import (
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nahime0/lazyagent/internal/claude"
)

// tickMsg triggers a full session reload (fallback when file watcher misses events).
type tickMsg time.Time

// renderTickMsg triggers a re-render to keep "X ago" timestamps live — no I/O.
type renderTickMsg time.Time

// sessionsMsg carries newly loaded sessions.
type sessionsMsg struct {
	sessions []*claude.Session
	err      error
}

// editorFinishedMsg is sent when a TUI editor (tea.Exec) exits.
type editorFinishedMsg struct{ err error }

// Model is the main bubbletea model.
type Model struct {
	sessions     []*claude.Session
	cursor       int
	selectedID   string // session ID of the currently selected item
	listOffset   int
	detailOffset int

	width  int
	height int

	err           error
	lastRefresh   time.Time
	loading       bool
	focus         int // 0 = list, 1 = detail
	windowMinutes int // show sessions modified in last N minutes
	spinFrame     int // animation frame counter for spinners

	// Sticky activity states, keyed by session ID
	activities map[string]*activityEntry

	// FSEvents-based watcher for ~/.claude/projects
	watcher *projectWatcher

	// waitingSince tracks when each session first entered StatusWaitingForUser.
	// Used to apply a grace period before displaying ActivityWaiting.
	waitingSince map[string]time.Time

	// Filter / search
	activityFilter ActivityKind // "" = show all
	searchMode     bool
	searchQuery    string

	// Cached visible sessions, recomputed via refreshVisible().
	visible []*claude.Session

	// Flash message (modal popup, dismissed by any key)
	flashMsg string

	// Editor picker popup
	editorPicker       bool
	editorPickerCursor int // 0 = VISUAL (GUI), 1 = EDITOR (TUI)
	editorPickerCWD    string
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Tab     key.Binding
	Quit    key.Binding
	Refresh key.Binding
	Plus    key.Binding
	Minus   key.Binding
	Filter key.Binding
	Search key.Binding
	Esc     key.Binding
	Open    key.Binding
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Tab:     key.NewBinding(key.WithKeys("tab")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Refresh: key.NewBinding(key.WithKeys("r")),
	Plus:    key.NewBinding(key.WithKeys("+", "=")),
	Minus:   key.NewBinding(key.WithKeys("-")),
	Filter: key.NewBinding(key.WithKeys("f")),
	Search: key.NewBinding(key.WithKeys("/")),
	Esc:     key.NewBinding(key.WithKeys("esc")),
	Open:    key.NewBinding(key.WithKeys("o")),
}

func NewModel() Model {
	w, _ := newProjectWatcher()
	return Model{
		loading:       true,
		activities:    make(map[string]*activityEntry),
		windowMinutes: 30,
		watcher:       w,
	}
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{makeLoadCmd(), tickCmd(), renderTickCmd()}
	if m.watcher != nil {
		cmds = append(cmds, watchCmd(m.watcher.Events))
	}
	return tea.Batch(cmds...)
}

func tickCmd() tea.Cmd {
	// Fallback tick in case the file watcher misses an event.
	return tea.Tick(30*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func renderTickCmd() tea.Cmd {
	// Fast tick just to keep "X ago" timestamps live — no I/O.
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return renderTickMsg(t)
	})
}

// makeLoadCmd loads all JSONL sessions from ~/.claude/projects.
func makeLoadCmd() tea.Cmd {
	return func() tea.Msg {
		sessions, err := claude.DiscoverSessions()
		if err != nil {
			return sessionsMsg{err: err}
		}
		return sessionsMsg{sessions: sessions}
	}
}

func sortSessions(sessions []*claude.Session) {
	slices.SortFunc(sessions, func(a, b *claude.Session) int {
		return cmp.Compare(b.LastActivity.UnixNano(), a.LastActivity.UnixNano())
	})
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case editorFinishedMsg:
		// TUI editor exited, bubbletea resumes automatically.

	case fileWatchMsg:
		// A JSONL file changed — reload immediately and re-arm the watcher.
		return m, tea.Batch(makeLoadCmd(), watchCmd(m.watcher.Events))

	case renderTickMsg:
		// Re-render only — no I/O, but update in-memory activity states.
		m.spinFrame++
		m.updateActivities(time.Now())
		m.refreshVisible()
		return m, renderTickCmd()

	case tickMsg:
		return m, tea.Batch(makeLoadCmd(), tickCmd())

	case sessionsMsg:
		m.loading = false
		now := time.Now()
		m.lastRefresh = now
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.sessions = msg.sessions
			m.updateActivities(now)
			sortSessions(m.sessions)
			m.refreshVisible()
			// Try to restore selection by session ID.
			found := false
			if m.selectedID != "" {
				for i, s := range m.visible {
					if s.SessionID == m.selectedID {
						m.cursor = i
						found = true
						break
					}
				}
			}
			if !found {
				// Clamp cursor and update selectedID.
				if n := len(m.visible); m.cursor >= n && n > 0 {
					m.cursor = n - 1
				}
				if len(m.visible) > 0 {
					m.selectedID = m.visible[m.cursor].SessionID
				}
			}
			m.ensureListVisible()
		}

	case tea.MouseMsg:
		if !m.searchMode {
			m.handleMouse(msg)
		}

	case tea.KeyMsg:
		// Flash popup: any key dismisses it.
		if m.flashMsg != "" {
			m.flashMsg = ""
			return m, nil
		}

		// Editor picker popup.
		if m.editorPicker {
			switch msg.String() {
			case "up", "k":
				m.editorPickerCursor = 0
			case "down", "j":
				m.editorPickerCursor = 1
			case "enter":
				m.editorPicker = false
				cwd := m.editorPickerCWD
				if m.editorPickerCursor == 0 {
					// GUI editor via VISUAL
					c := exec.Command(os.Getenv("VISUAL"), cwd)
					c.Stdin = nil
					c.Stdout = nil
					c.Stderr = nil
					_ = c.Start()
				} else {
					// TUI editor via EDITOR — suspend the TUI
					editor := os.Getenv("EDITOR")
					c := exec.Command(editor, cwd)
					return m, tea.ExecProcess(c, func(err error) tea.Msg {
						return editorFinishedMsg{err}
					})
				}
			case "esc":
				m.editorPicker = false
			}
			return m, nil
		}

		// Search mode intercepts all keys except esc.
		if m.searchMode {
			switch msg.String() {
			case "esc":
				m.searchMode = false
				m.searchQuery = ""
				m.cursor = 0
				m.listOffset = 0
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				}
				m.cursor = 0
				m.listOffset = 0
			default:
				if len(msg.Runes) == 1 {
					m.searchQuery += string(msg.Runes)
				}
				m.cursor = 0
				m.listOffset = 0
			}
			m.refreshVisible()
			return m, nil
		}

		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Tab):
			m.focus = (m.focus + 1) % 2
			m.detailOffset = 0

		case key.Matches(msg, keys.Plus):
			if m.windowMinutes < 480 {
				m.windowMinutes += 10
			}
			m.refreshVisible()
			if n := len(m.visible); m.cursor >= n && n > 0 {
				m.cursor = n - 1
			}

		case key.Matches(msg, keys.Minus):
			if m.windowMinutes > 10 {
				m.windowMinutes -= 10
			}
			m.refreshVisible()
			if n := len(m.visible); m.cursor >= n && n > 0 {
				m.cursor = n - 1
			}

		case key.Matches(msg, keys.Refresh):
			m.loading = true
			return m, makeLoadCmd()

		case key.Matches(msg, keys.Up):
			if m.focus == 0 {
				if m.cursor > 0 {
					m.cursor--
					m.detailOffset = 0
					m.ensureListVisible()
					if m.cursor < len(m.visible) {
						m.selectedID = m.visible[m.cursor].SessionID
					}
				}
			} else {
				if m.detailOffset > 0 {
					m.detailOffset--
				}
			}

		case key.Matches(msg, keys.Down):
			if m.focus == 0 {
				if m.cursor < len(m.visible)-1 {
					m.cursor++
					m.detailOffset = 0
					m.ensureListVisible()
					if m.cursor < len(m.visible) {
						m.selectedID = m.visible[m.cursor].SessionID
					}
				}
			} else {
				m.detailOffset++
			}

		case key.Matches(msg, keys.Filter):
			m.activityFilter = nextActivityFilter(m.activityFilter)
			m.cursor = 0
			m.listOffset = 0
			m.refreshVisible()

		case key.Matches(msg, keys.Open):
			if len(m.visible) > 0 && m.cursor < len(m.visible) {
				cwd := m.visible[m.cursor].CWD
				hasVisual := os.Getenv("VISUAL") != ""
				hasEditor := os.Getenv("EDITOR") != ""

				switch {
				case hasVisual && hasEditor:
					// Both set — let the user choose.
					m.editorPicker = true
					m.editorPickerCursor = 0
					m.editorPickerCWD = cwd
				case hasVisual:
					c := exec.Command(os.Getenv("VISUAL"), cwd)
					c.Stdin = nil
					c.Stdout = nil
					c.Stderr = nil
					_ = c.Start()
				case hasEditor:
					c := exec.Command(os.Getenv("EDITOR"), cwd)
					return m, tea.ExecProcess(c, func(err error) tea.Msg {
						return editorFinishedMsg{err}
					})
				default:
					m.flashMsg = "Set $VISUAL or $EDITOR, e.g.\n\n  export VISUAL=\"code\"  # add to ~/.zshrc or ~/.bashrc"
				}
			}

		case key.Matches(msg, keys.Search):
			m.searchMode = true
		}
	}

	return m, nil
}

// handleMouse processes mouse events for click selection and scroll.
func (m *Model) handleMouse(msg tea.MouseMsg) {
	// Render title/help once to measure heights and compute layout.
	titleH := lipgloss.Height(m.renderTitleBar())
	helpH := lipgloss.Height(m.renderHelp())
	listW, _, _ := m.layout(titleH, helpH)

	panelTop := titleH               // first row of the panel (top border)
	panelBot := m.height - helpH - 1 // last row of the panel (bottom border)

	// Determine which panel the mouse is over based on X coordinate.
	panelBoundary := listW + 2

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.X < panelBoundary {
			if m.cursor > 0 {
				m.cursor--
				m.ensureListVisible()
				if m.cursor < len(m.visible) {
					m.selectedID = m.visible[m.cursor].SessionID
				}
				m.detailOffset = 0
			}
		} else {
			if m.detailOffset > 0 {
				m.detailOffset -= 3
				if m.detailOffset < 0 {
					m.detailOffset = 0
				}
			}
		}

	case tea.MouseButtonWheelDown:
		if msg.X < panelBoundary {
			if m.cursor < len(m.visible)-1 {
				m.cursor++
				m.ensureListVisible()
				if m.cursor < len(m.visible) {
					m.selectedID = m.visible[m.cursor].SessionID
				}
				m.detailOffset = 0
			}
		} else {
			m.detailOffset += 3
		}

	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionMotion {
			return
		}
		if msg.Y < panelTop || msg.Y > panelBot {
			return
		}

		if msg.X < panelBoundary {
			m.focus = 0

			// panelTop+0 = top border, +1 = header, +2 = divider, +3 = first item
			itemRow := msg.Y - panelTop - 3
			if itemRow < 0 {
				return
			}
			idx := m.listOffset + itemRow
			if idx >= 0 && idx < len(m.visible) {
				m.cursor = idx
				m.selectedID = m.visible[m.cursor].SessionID
				m.detailOffset = 0
			}
		} else {
			m.focus = 1
		}
	}
}

var activityFilterOrder = []ActivityKind{
	"",
	ActivityIdle,
	ActivityWaiting,
	ActivityThinking,
	ActivityCompacting,
	ActivityReading,
	ActivityWriting,
	ActivityRunning,
	ActivitySearching,
	ActivityBrowsing,
	ActivitySpawning,
}

func nextActivityFilter(current ActivityKind) ActivityKind {
	for i, k := range activityFilterOrder {
		if k == current {
			return activityFilterOrder[(i+1)%len(activityFilterOrder)]
		}
	}
	return ""
}

// refreshVisible recomputes the cached visible sessions list.
// Must be called whenever sessions, filters, search, or time window change.
func (m *Model) refreshVisible() {
	cutoff := time.Now().Add(-time.Duration(m.windowMinutes) * time.Minute)
	lowerQuery := strings.ToLower(m.searchQuery)
	m.visible = m.visible[:0]
	for _, s := range m.sessions {
		if s.IsSidechain || !s.LastActivity.After(cutoff) {
			continue
		}
		if m.activityFilter != "" && m.activityFor(s.SessionID) != m.activityFilter {
			continue
		}
		if lowerQuery != "" &&
			!strings.Contains(strings.ToLower(s.CWD), lowerQuery) {
			continue
		}
		m.visible = append(m.visible, s)
	}
}

func (m *Model) ensureListVisible() {
	vis := m.listVisibleRows()
	if vis <= 0 {
		return
	}
	n := len(m.visible)
	if m.cursor >= n && n > 0 {
		m.cursor = n - 1
	}
	if m.cursor < m.listOffset {
		m.listOffset = m.cursor
	} else if m.cursor >= m.listOffset+vis {
		m.listOffset = m.cursor - vis + 1
	}
}

// ── Layout math ──────────────────────────────────────────────────────────────

// layout computes panel widths and inner height from pre-measured bar heights.
func (m Model) layout(titleH, helpH int) (listW, detailW, innerH int) {
	total := m.width - 4
	if total < 8 {
		total = 8
	}
	listW = total * 35 / 100
	if listW < 12 {
		listW = 12
	}
	detailW = total - listW
	if detailW < 8 {
		detailW = 8
	}
	innerH = m.height - titleH - helpH - 2 // 2 = panel top + bottom border
	if innerH < 1 {
		innerH = 1
	}
	return
}

// dims computes layout by rendering the title/help bars to measure their height.
// Use layout() directly when title/help are already rendered to avoid redundant work.
func (m Model) dims() (listW, detailW, innerH int) {
	return m.layout(lipgloss.Height(m.renderTitleBar()), lipgloss.Height(m.renderHelp()))
}

func (m Model) listVisibleRows() int {
	_, _, innerH := m.dims()
	v := innerH - 2 // header + divider
	if v < 0 {
		return 0
	}
	return v
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	title := m.renderTitleBar()
	help := m.renderHelp()
	listW, detailW, innerH := m.layout(lipgloss.Height(title), lipgloss.Height(help))

	left := m.renderList(listW, innerH)
	right := m.renderDetail(detailW, innerH)
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	out := lipgloss.JoinVertical(lipgloss.Left, title, content, help)

	// Clamp output to terminal height to prevent scrolling on resize.
	if m.height > 0 {
		lines := strings.Split(out, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
		out = strings.Join(lines, "\n")
	}

	// Overlay editor picker popup.
	if m.editorPicker {
		visual := os.Getenv("VISUAL")
		editor := os.Getenv("EDITOR")

		opt0 := "  " + visual + "  (GUI)"
		opt1 := "  " + editor + "  (TUI)"
		if m.editorPickerCursor == 0 {
			opt0 = lipgloss.NewStyle().Background(colorSelBg).Foreground(colorText).Bold(true).Render("▸ " + visual + "  (GUI)")
			opt1 = lipgloss.NewStyle().Foreground(colorSubtext).Render(opt1)
		} else {
			opt0 = lipgloss.NewStyle().Foreground(colorSubtext).Render(opt0)
			opt1 = lipgloss.NewStyle().Background(colorSelBg).Foreground(colorText).Bold(true).Render("▸ " + editor + "  (TUI)")
		}

		title := lipgloss.NewStyle().Foreground(colorText).Bold(true).Render("Open with:")
		hint := lipgloss.NewStyle().Foreground(colorMuted).Render("↑/↓ select  enter confirm  esc cancel")
		body := title + "\n\n" + opt0 + "\n" + opt1 + "\n\n" + hint

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorderFocus).
			Background(lipgloss.Color("#1F2937")).
			Foreground(colorText).
			Padding(1, 3).
			Render(body)

		out = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			box,
			lipgloss.WithWhitespaceBackground(lipgloss.Color("#111827")),
		)
	}

	// Overlay flash message centered over the existing UI.
	if m.flashMsg != "" {
		dismiss := lipgloss.NewStyle().Foreground(colorMuted).Render("Press any key to continue")
		body := m.flashMsg + "\n\n" + dismiss
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorWarning).
			Background(lipgloss.Color("#1F2937")).
			Foreground(colorText).
			Padding(1, 3).
			Render(body)

		out = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			box,
			lipgloss.WithWhitespaceBackground(lipgloss.Color("#111827")),
		)
	}

	return out
}

func (m Model) renderTitleBar() string {
	left := titleStyle.Render("lazyagent")
	count := lipgloss.NewStyle().
		Background(colorPrimary).Foreground(colorSubtext).
		Padding(0, 1).
		Render(fmt.Sprintf("%d sessions [last %dm]", len(m.visible), m.windowMinutes))

	parts := []string{left, count}

	if m.activityFilter != "" {
		filterBadge := lipgloss.NewStyle().
			Background(colorPrimary).Foreground(colorWarning).Bold(true).
			Padding(0, 1).
			Render("▸ " + string(m.activityFilter))
		parts = append(parts, filterBadge)
	}

	refresh := lipgloss.NewStyle().
		Background(colorPrimary).Foreground(colorMuted).
		Padding(0, 1).
		Render("updated " + formatDuration(time.Since(m.lastRefresh)))
	parts = append(parts, refresh)

	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return lipgloss.NewStyle().
		Background(colorPrimary).
		Width(m.width).
		Render(bar)
}

// ── List panel ───────────────────────────────────────────────────────────────

const statusColW = 11 // "processing" = 10 chars + 1 padding

func (m Model) renderList(listW, innerH int) string {
	pStyle := panelStyle
	if m.focus == 0 {
		pStyle = panelFocusStyle
	}

	sessions := m.visible

	if m.loading && len(sessions) == 0 {
		return pStyle.Width(listW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorMuted).Render("loading..."),
		)
	}
	if len(sessions) == 0 && !m.searchMode {
		return pStyle.Width(listW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorMuted).Render("no sessions found"),
		)
	}

	vis := innerH - 2 // header + divider
	if vis < 1 {
		vis = 1
	}

	maxOff := len(sessions) - vis
	if maxOff < 0 {
		maxOff = 0
	}
	off := clamp(0, maxOff, m.listOffset)
	end := off + vis
	if end > len(sessions) {
		end = len(sessions)
	}

	sparkW := 0
	if listW > 44 {
		sparkW = 12
	}
	nameW := listW - statusColW - sparkW
	if nameW < 4 {
		nameW = 4
	}

	var header string
	if m.searchMode {
		header = lipgloss.NewStyle().Foreground(colorWarning).Bold(true).
			Render("/ " + m.searchQuery + "█")
	} else {
		projectLabel := "PROJECT"
		if m.activityFilter != "" {
			projectLabel += " [" + string(m.activityFilter) + "]"
		}
		header = lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).
			Render(fmt.Sprintf("%-*s %s", nameW+sparkW, projectLabel, "STATUS"))
	}
	divider := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", listW))

	var rows []string
	rows = append(rows, header, divider)

	if len(sessions) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(colorMuted).Render("no results"))
		return pStyle.Width(listW).Height(innerH).Render(strings.Join(rows, "\n"))
	}

	for i := off; i < end; i++ {
		rows = append(rows, m.renderListRow(sessions[i], nameW, sparkW, i == m.cursor))
	}

	return pStyle.Width(listW).Height(innerH).Render(strings.Join(rows, "\n"))
}

func (m Model) renderListRow(s *claude.Session, nameW, sparkW int, selected bool) string {
	activity := m.activityFor(s.SessionID)
	actColor, ok := activityColors[activity]
	if !ok {
		actColor = colorMuted
	}

	// Activity label with spinner for active states
	actStr := padRight(string(activity), statusColW)
	if isActiveActivity(activity) {
		spin := string(spinnerFrames[m.spinFrame%len(spinnerFrames)])
		actStr = spin + padRight(string(activity), statusColW-1)
	}

	name := shortName(s.CWD, nameW)
	name = padRight(name, nameW)

	// Sparkline
	var sparkStr string
	if sparkW > 0 {
		spark := renderSparkline(s.EntryTimestamps, time.Duration(m.windowMinutes)*time.Minute, sparkW-2)
		sparkStr = " " + spark + " "
	}

	nameStyle := lipgloss.NewStyle().Foreground(colorSubtext)
	actStyle := lipgloss.NewStyle().Foreground(actColor)
	sparkStyle := actStyle
	if selected {
		nameStyle = nameStyle.Background(colorSelBg).Foreground(colorText).Bold(true)
		sparkStyle = sparkStyle.Background(colorSelBg)
		actStyle = actStyle.Background(colorSelBg).Bold(true)
	}

	return nameStyle.Render(name) + sparkStyle.Render(sparkStr) + actStyle.Render(actStr)
}

// ── Detail panel ─────────────────────────────────────────────────────────────

func (m Model) renderDetail(detailW, innerH int) string {
	pStyle := panelStyle
	if m.focus == 1 {
		pStyle = panelFocusStyle
	}

	if m.err != nil && len(m.visible) == 0 {
		return pStyle.Width(detailW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorWarning).Render("error: "+m.err.Error()),
		)
	}
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return pStyle.Width(detailW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorMuted).Render("select a session"),
		)
	}

	lines := m.buildDetailLines(m.visible[m.cursor], detailW)

	vis := innerH
	maxOff := len(lines) - vis
	if maxOff < 0 {
		maxOff = 0
	}
	off := clamp(0, maxOff, m.detailOffset)
	end := off + vis
	if end > len(lines) {
		end = len(lines)
	}

	return pStyle.Width(detailW).Height(innerH).Render(
		strings.Join(lines[off:end], "\n"),
	)
}

func (m Model) buildDetailLines(s *claude.Session, width int) []string {
	var lines []string
	add := func(line string) { lines = append(lines, line) }

	// CWD
	add(lipgloss.NewStyle().Foreground(colorText).Bold(true).
		Render(shortName(s.CWD, width-2)))

	// Activity + current tool
	activity := m.activityFor(s.SessionID)
	actColor := activityColors[activity]
	statusLine := lipgloss.NewStyle().Foreground(actColor).Bold(true).Render("● ") +
		lipgloss.NewStyle().Foreground(actColor).Bold(true).Render(string(activity))
	if s.CurrentTool != "" {
		statusLine += "  " + lipgloss.NewStyle().Foreground(colorMuted).
			Render("(" + s.CurrentTool + ")")
	}
	add(statusLine)
	add("")
	add(lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", width-2)))
	add("")

	row := func(label, value string) string {
		return labelStyle.Render(label) + valueStyle.Render(value)
	}

	if s.SessionID != "" {
		sid := s.SessionID
		if len(sid) > 16 {
			sid = sid[:8] + "…" + sid[len(sid)-4:]
		}
		add(row("Session ID", sid))
	}
	if s.Version != "" {
		add(row("Version", s.Version))
	}
	if s.Model != "" {
		add(row("Model", s.Model))
	}
	if s.GitBranch != "" && s.GitBranch != "HEAD" {
		add(row("Git Branch", s.GitBranch))
	}

	wtStr := "no"
	if s.IsWorktree {
		wtStr = lipgloss.NewStyle().Foreground(colorAccent).Render("yes")
		if s.MainRepo != "" {
			wtStr += lipgloss.NewStyle().Foreground(colorSubtext).
				Render(" (" + shortName(s.MainRepo, 28) + ")")
		}
	}
	add(row("Worktree", wtStr))

	add(row("Messages", fmt.Sprintf("%d  (%d user, %d assistant)",
		s.TotalMessages, s.UserMessages, s.AssistantMessages)))

	// Token usage and cost
	if s.InputTokens > 0 || s.OutputTokens > 0 {
		cost := s.CostUSD
		if cost == 0 {
			cost = estimateCost(s.Model, s.InputTokens, s.OutputTokens, s.CacheCreationTokens, s.CacheReadTokens)
		}
		tokenInfo := formatTokens(s.InputTokens+s.CacheCreationTokens+s.CacheReadTokens) + " in / " + formatTokens(s.OutputTokens) + " out"
		if cost > 0.001 {
			tokenInfo += "  " + lipgloss.NewStyle().Foreground(colorAccent).Render(formatCost(cost))
		}
		add(row("Tokens", tokenInfo))
	}

	if len(s.RecentTools) > 0 {
		last := s.RecentTools[len(s.RecentTools)-1]
		add(row("Last operation", last.Name+"  "+
			lipgloss.NewStyle().Foreground(colorMuted).Render("("+formatDuration(time.Since(last.Timestamp))+")")))
	} else {
		add(row("Last operation", formatDuration(time.Since(s.LastActivity))))
	}

	if s.LastFileWrite != "" {
		agePart := " (" + formatDuration(time.Since(s.LastFileWriteAt)) + ")"
		// width-2 for panel borders, -22 for label, -len(agePart) for the age suffix
		maxFile := width - 2 - 22 - len(agePart)
		if maxFile < 4 {
			maxFile = 4
		}
		filePart := shortName(s.LastFileWrite, maxFile)
		add(row("Last file", filePart+lipgloss.NewStyle().Foreground(colorMuted).Render(agePart)))
	}

	if len(s.RecentMessages) > 0 {
		add("")
		add(lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", width-2)))
		add(lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Conversation"))
		add("")
		msgs := s.RecentMessages
		if len(msgs) > 5 {
			msgs = msgs[len(msgs)-5:]
		}
		msgW := width - 8
		if msgW < 4 {
			msgW = 4
		}
		for i := len(msgs) - 1; i >= 0; i-- {
			msg := msgs[i]
			roleLabel := msg.Role
			if roleLabel == "assistant" {
				roleLabel = "AI"
			} else if roleLabel == "user" {
				roleLabel = "User"
			}
			role := padRight(roleLabel, 4)
			text := msg.Text
			// Collapse newlines for single-line display
			text = strings.ReplaceAll(text, "\n", " ")
			if len(text) > msgW {
				text = text[:msgW-1] + "…"
			}
			add(lipgloss.NewStyle().Foreground(colorSubtext).Render("  "+role+"  ") +
				lipgloss.NewStyle().Foreground(colorText).Render(text))
		}
	}

	if len(s.RecentTools) > 0 {
		add("")
		add(lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", width-2)))
		add(lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Recent Tools"))
		add("")
		tools := s.RecentTools
		if len(tools) > 20 {
			tools = tools[len(tools)-20:]
		}
		for i := len(tools) - 1; i >= 0; i-- {
			tc := tools[i]
			ago := formatDuration(time.Since(tc.Timestamp))
			add(lipgloss.NewStyle().Foreground(colorPrimary).Render("  "+tc.Name) +
				lipgloss.NewStyle().Foreground(colorMuted).Render("  "+ago))
		}
	}

	return lines
}

// ── Help bar ─────────────────────────────────────────────────────────────────

func (m Model) renderHelp() string {
	var parts []string
	if m.searchMode {
		parts = []string{
			helpKeyStyle.Render("esc") + helpStyle.Render(" clear"),
			helpKeyStyle.Render("backspace") + helpStyle.Render(" del"),
		}
		return helpStyle.Width(m.width).Render(strings.Join(parts, "  "))
	}

	if m.focus == 0 {
		parts = append(parts,
			helpKeyStyle.Render("k/↑")+helpStyle.Render(" prev"),
			helpKeyStyle.Render("j/↓")+helpStyle.Render(" next"),
			helpKeyStyle.Render("tab")+helpStyle.Render(" detail"),
			helpKeyStyle.Render("click")+helpStyle.Render(" select"),
		)
	} else {
		parts = append(parts,
			helpKeyStyle.Render("k/↑")+helpStyle.Render(" scroll up"),
			helpKeyStyle.Render("j/↓")+helpStyle.Render(" scroll dn"),
			helpKeyStyle.Render("tab")+helpStyle.Render(" list"),
			helpKeyStyle.Render("click")+helpStyle.Render(" focus"),
		)
	}
	parts = append(parts,
		helpKeyStyle.Render("scroll")+helpStyle.Render(" navigate"),
		helpKeyStyle.Render("+/-")+helpStyle.Render(" mins"),
		helpKeyStyle.Render("f")+helpStyle.Render(" filter"),
		helpKeyStyle.Render("/")+helpStyle.Render(" search"),
		helpKeyStyle.Render("o")+helpStyle.Render(" open"),
		helpKeyStyle.Render("r")+helpStyle.Render(" refresh"),
		helpKeyStyle.Render("q")+helpStyle.Render(" quit"),
	)
	return helpStyle.Width(m.width).Render(strings.Join(parts, "  "))
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func shortName(path string, maxLen int) string {
	if maxLen <= 2 {
		return ""
	}
	if len(path) <= maxLen {
		return path
	}
	base := filepath.Base(path)
	parent := filepath.Base(filepath.Dir(path))
	short := parent + "/" + base
	if len(short)+2 <= maxLen {
		return "…/" + short
	}
	if len(base)+2 <= maxLen {
		return "…/" + base
	}
	if maxLen > 3 {
		return "…" + base[len(base)-(maxLen-1):]
	}
	return base[:maxLen]
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s[:n]
	}
	return s + strings.Repeat(" ", n-len(s))
}

func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

func clamp(lo, hi, v int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── Sparkline ─────────────────────────────────────────────────────────────────

var sparkBlocks = []rune{' ', '▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

func renderSparkline(timestamps []time.Time, window time.Duration, width int) string {
	if width <= 0 {
		return ""
	}
	if len(timestamps) == 0 {
		return strings.Repeat(" ", width)
	}

	now := time.Now()
	cutoff := now.Add(-window)
	bucketDur := window / time.Duration(width)
	if bucketDur <= 0 {
		return strings.Repeat(" ", width)
	}

	buckets := make([]int, width)
	for _, ts := range timestamps {
		if ts.Before(cutoff) || ts.After(now) {
			continue
		}
		idx := int(ts.Sub(cutoff) / bucketDur)
		if idx >= width {
			idx = width - 1
		}
		buckets[idx]++
	}

	maxVal := 0
	for _, v := range buckets {
		if v > maxVal {
			maxVal = v
		}
	}

	if maxVal == 0 {
		return strings.Repeat(" ", width)
	}

	var sb strings.Builder
	sb.Grow(width * 3)
	for _, v := range buckets {
		idx := v * 8 / maxVal
		if idx > 8 {
			idx = 8
		}
		sb.WriteRune(sparkBlocks[idx])
	}
	return sb.String()
}

// ── Spinner & cost helpers ────────────────────────────────────────────────────

var spinnerFrames = []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}

func formatTokens(n int) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1_000_000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1_000_000)
}

func formatCost(usd float64) string {
	if usd < 0.01 {
		return "<$0.01"
	}
	return fmt.Sprintf("$%.2f", usd)
}

func estimateCost(model string, inputTokens, outputTokens, cacheCreation, cacheRead int) float64 {
	var inputRate, outputRate float64
	lowerModel := strings.ToLower(model)
	switch {
	case strings.Contains(lowerModel, "opus"):
		inputRate, outputRate = 15.0/1_000_000, 75.0/1_000_000
	case strings.Contains(lowerModel, "haiku"):
		inputRate, outputRate = 1.0/1_000_000, 5.0/1_000_000
	default: // sonnet and others
		inputRate, outputRate = 3.0/1_000_000, 15.0/1_000_000
	}
	cacheWriteRate := inputRate * 1.25
	cacheReadRate := inputRate * 0.1
	return float64(inputTokens)*inputRate +
		float64(cacheCreation)*cacheWriteRate +
		float64(cacheRead)*cacheReadRate +
		float64(outputTokens)*outputRate
}

