package ui

import (
	"cmp"
	"fmt"
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

	case fileWatchMsg:
		// A JSONL file changed — reload immediately and re-arm the watcher.
		return m, tea.Batch(makeLoadCmd(), watchCmd(m.watcher.Events))

	case renderTickMsg:
		// Re-render only — no I/O, but update in-memory activity states.
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

		case key.Matches(msg, keys.Search):
			m.searchMode = true
		}
	}

	return m, nil
}

// handleMouse processes mouse events for click selection and scroll.
func (m *Model) handleMouse(msg tea.MouseMsg) {
	listW, _, _ := m.dims()

	// Content area starts at row 1 (after title bar) and ends before help bar.
	// Panel borders add 1 row top + 1 row bottom.
	contentTop := 1    // title bar is row 0
	contentBot := m.height - 2 // help bar is last row

	// Determine which panel the mouse is over based on X coordinate.
	// List panel: x in [0, listW+2) (including border)
	// Detail panel: x >= listW+2
	panelBoundary := listW + 2

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.X < panelBoundary {
			// Scroll list up
			if m.cursor > 0 {
				m.cursor--
				m.ensureListVisible()
				if m.cursor < len(m.visible) {
					m.selectedID = m.visible[m.cursor].SessionID
				}
				m.detailOffset = 0
			}
		} else {
			// Scroll detail up
			if m.detailOffset > 0 {
				m.detailOffset -= 3
				if m.detailOffset < 0 {
					m.detailOffset = 0
				}
			}
		}

	case tea.MouseButtonWheelDown:
		if msg.X < panelBoundary {
			// Scroll list down
			if m.cursor < len(m.visible)-1 {
				m.cursor++
				m.ensureListVisible()
				if m.cursor < len(m.visible) {
					m.selectedID = m.visible[m.cursor].SessionID
				}
				m.detailOffset = 0
			}
		} else {
			// Scroll detail down
			m.detailOffset += 3
		}

	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return
		}
		if msg.Y < contentTop || msg.Y > contentBot {
			return
		}

		if msg.X < panelBoundary {
			// Click in list panel — switch focus and select session.
			m.focus = 0

			// Calculate which row was clicked.
			// Inside the panel: row 0 = top border, row 1 = header, row 2 = divider,
			// rows 3+ = session items.
			rowInPanel := msg.Y - contentTop
			// Subtract border (1), header (1), divider (1) = 3 rows before items.
			itemRow := rowInPanel - 3
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
			// Click in detail panel — switch focus.
			m.focus = 1
		}
	}
}

var activityFilterOrder = []ActivityKind{
	"",
	ActivityIdle,
	ActivityWaiting,
	ActivityThinking,
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
	m.visible = m.visible[:0]
	for _, s := range m.sessions {
		if s.IsSidechain || !s.LastActivity.After(cutoff) {
			continue
		}
		if m.activityFilter != "" && m.activityFor(s.SessionID) != m.activityFilter {
			continue
		}
		if m.searchQuery != "" &&
			!strings.Contains(strings.ToLower(s.CWD), strings.ToLower(m.searchQuery)) {
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

func (m Model) dims() (listW, detailW, innerH int) {
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
	innerH = m.height - 4
	if innerH < 1 {
		innerH = 1
	}
	return
}

func (m Model) listVisibleRows() int {
	_, _, innerH := m.dims()
	v := innerH - 2 // header + divider
	if v < 0 {
		return 0
	}
	return v
}

func (m Model) detailVisibleLines() int {
	_, _, innerH := m.dims()
	return innerH
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return "Initializing..."
	}

	listW, detailW, _ := m.dims()

	title := m.renderTitleBar()
	left := m.renderList(listW)
	right := m.renderDetail(detailW)
	content := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	help := m.renderHelp()

	return lipgloss.JoinVertical(lipgloss.Left, title, content, help)
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

func (m Model) renderList(listW int) string {
	_, _, innerH := m.dims()
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

	vis := m.listVisibleRows()
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

	nameW := listW - statusColW
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
			Render(fmt.Sprintf("%-*s %s", nameW, projectLabel, "STATUS"))
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
		rows = append(rows, m.renderListRow(sessions[i], nameW, i == m.cursor))
	}

	return pStyle.Width(listW).Height(innerH).Render(strings.Join(rows, "\n"))
}

func (m Model) renderListRow(s *claude.Session, nameW int, selected bool) string {
	activity := m.activityFor(s.SessionID)
	actColor, ok := activityColors[activity]
	if !ok {
		actColor = colorMuted
	}
	actStr := padRight(string(activity), statusColW)

	name := shortName(s.CWD, nameW)
	name = padRight(name, nameW)

	if selected {
		namePart := lipgloss.NewStyle().
			Background(colorSelBg).Foreground(colorText).Bold(true).
			Render(name)
		actPart := lipgloss.NewStyle().
			Background(colorSelBg).Foreground(actColor).Bold(true).
			Render(actStr)
		return namePart + actPart
	}

	namePart := lipgloss.NewStyle().Foreground(colorSubtext).Render(name)
	actPart := lipgloss.NewStyle().Foreground(actColor).Render(actStr)
	return namePart + actPart
}

// ── Detail panel ─────────────────────────────────────────────────────────────

func (m Model) renderDetail(detailW int) string {
	_, _, innerH := m.dims()
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

	vis := m.detailVisibleLines()
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
		parts = []string{
			helpKeyStyle.Render("k/↑") + helpStyle.Render(" prev"),
			helpKeyStyle.Render("j/↓") + helpStyle.Render(" next"),
			helpKeyStyle.Render("tab") + helpStyle.Render(" detail"),
			helpKeyStyle.Render("click") + helpStyle.Render(" select"),
			helpKeyStyle.Render("scroll") + helpStyle.Render(" navigate"),
			helpKeyStyle.Render("+/-") + helpStyle.Render(" mins"),
			helpKeyStyle.Render("f") + helpStyle.Render(" filter"),
			helpKeyStyle.Render("/") + helpStyle.Render(" search"),
			helpKeyStyle.Render("r") + helpStyle.Render(" refresh"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	} else {
		parts = []string{
			helpKeyStyle.Render("k/↑") + helpStyle.Render(" scroll up"),
			helpKeyStyle.Render("j/↓") + helpStyle.Render(" scroll dn"),
			helpKeyStyle.Render("tab") + helpStyle.Render(" list"),
			helpKeyStyle.Render("click") + helpStyle.Render(" focus"),
			helpKeyStyle.Render("scroll") + helpStyle.Render(" navigate"),
			helpKeyStyle.Render("+/-") + helpStyle.Render(" mins"),
			helpKeyStyle.Render("f") + helpStyle.Render(" filter"),
			helpKeyStyle.Render("/") + helpStyle.Render(" search"),
			helpKeyStyle.Render("r") + helpStyle.Render(" refresh"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	}
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

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
