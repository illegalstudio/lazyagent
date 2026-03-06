package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nahime0/lazyclaude/internal/claude"
)

// tickMsg is sent on each refresh tick.
type tickMsg time.Time

// sessionsMsg carries newly loaded sessions.
type sessionsMsg struct {
	sessions []*claude.Session
	err      error
}

// Model is the main bubbletea model.
type Model struct {
	sessions     []*claude.Session
	cursor       int
	listOffset   int
	detailOffset int

	width  int
	height int

	err         error
	lastRefresh time.Time
	loading     bool
	focus       int  // 0 = list, 1 = detail
	showAll     bool // false = only sessions with a running process

	// Kill confirmation: first K press arms it, second K fires
	pendingKill bool
}

type keyMap struct {
	Up      key.Binding
	Down    key.Binding
	Tab     key.Binding
	Quit    key.Binding
	Refresh key.Binding
	All     key.Binding
	Kill    key.Binding
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k")),
	Down:    key.NewBinding(key.WithKeys("down", "j")),
	Tab:     key.NewBinding(key.WithKeys("tab")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Refresh: key.NewBinding(key.WithKeys("r")),
	All:     key.NewBinding(key.WithKeys("a")),
	Kill:    key.NewBinding(key.WithKeys("K")),
}

func NewModel() Model {
	return Model{loading: true}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(makeLoadCmd(m.showAll), tickCmd())
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// makeLoadCmd returns a tea.Cmd that loads sessions in the appropriate mode.
// Active mode (showAll=false): process-first — one entry per running process.
// All mode (showAll=true):    JSONL-first — every conversation file, with PIDs enriched.
func makeLoadCmd(showAll bool) tea.Cmd {
	return func() tea.Msg {
		procs, _ := claude.FindClaudeProcesses()

		if !showAll {
			sessions, err := claude.DiscoverActiveSessions(procs)
			if err != nil {
				return sessionsMsg{err: err}
			}
			sortSessions(sessions)
			return sessionsMsg{sessions: sessions}
		}

		sessions, err := claude.DiscoverSessions()
		if err != nil {
			return sessionsMsg{err: err}
		}
		claude.EnrichWithProcessInfo(sessions, procs)
		sortSessions(sessions)
		return sessionsMsg{sessions: sessions}
	}
}

func sortSessions(sessions []*claude.Session) {
	for i := 0; i < len(sessions); i++ {
		for j := i + 1; j < len(sessions); j++ {
			if sessions[j].LastActivity.After(sessions[i].LastActivity) {
				sessions[i], sessions[j] = sessions[j], sessions[i]
			}
		}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(makeLoadCmd(m.showAll), tickCmd())

	case sessionsMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.sessions = msg.sessions
			// Clamp cursor to visible list
			if n := len(m.visibleSessions()); m.cursor >= n && n > 0 {
				m.cursor = n - 1
			}
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Tab):
			m.pendingKill = false
			m.focus = (m.focus + 1) % 2
			m.detailOffset = 0

		case key.Matches(msg, keys.Kill):
			sessions := m.visibleSessions()
			if m.cursor < len(sessions) {
				s := sessions[m.cursor]
				if s.PID > 0 {
					if m.pendingKill {
						// Second K: execute kill
						killProcess(s.PID)
						m.pendingKill = false
						m.loading = true
						return m, makeLoadCmd(m.showAll)
					}
					// First K: arm confirmation
					m.pendingKill = true
				}
			}

		case key.Matches(msg, keys.All):
			m.pendingKill = false
			m.showAll = !m.showAll
			m.cursor = 0
			m.listOffset = 0
			m.loading = true
			return m, makeLoadCmd(m.showAll)

		case key.Matches(msg, keys.Refresh):
			m.pendingKill = false
			m.loading = true
			return m, makeLoadCmd(m.showAll)

		case key.Matches(msg, keys.Up):
			m.pendingKill = false
			if m.focus == 0 {
				if m.cursor > 0 {
					m.cursor--
					m.ensureListVisible()
				}
			} else {
				if m.detailOffset > 0 {
					m.detailOffset--
				}
			}

		case key.Matches(msg, keys.Down):
			m.pendingKill = false
			if m.focus == 0 {
				if m.cursor < len(m.visibleSessions())-1 {
					m.cursor++
					m.ensureListVisible()
				}
			} else {
				m.detailOffset++
			}
		}
	}

	return m, nil
}

// visibleSessions returns sessions to display.
// In active mode the list already contains only running sessions (process-first).
// In all mode we filter out sidechains to keep the list clean.
func (m Model) visibleSessions() []*claude.Session {
	if !m.showAll {
		return m.sessions // already process-first, no extra filtering needed
	}
	var out []*claude.Session
	for _, s := range m.sessions {
		if !s.IsSidechain {
			out = append(out, s)
		}
	}
	return out
}

func (m *Model) ensureListVisible() {
	vis := m.listVisibleRows()
	if vis <= 0 {
		return
	}
	n := len(m.visibleSessions())
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
//
// Each panel uses border only (no padding): 2 cols overhead (left+right border),
// 2 rows overhead (top+bottom border).
//
// Two panels side by side:
//   (listW + 2) + (detailW + 2) = m.width
//   listW + detailW = m.width - 4
//
// Height:
//   titleBar(1) + borderTop(1) + innerH + borderBottom(1) + helpBar(1) = m.height
//   innerH = m.height - 4
//
// Visible list rows (inside panel, minus header row + divider row):
//   visibleRows = innerH - 2

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
	// Kill confirmation banner replaces normal title bar
	if m.pendingKill {
		sessions := m.visibleSessions()
		pid := 0
		if m.cursor < len(sessions) {
			pid = sessions[m.cursor].PID
		}
		msg := fmt.Sprintf("  Kill PID %d? Press K to confirm, any other key to cancel  ", pid)
		return lipgloss.NewStyle().
			Background(colorDanger).Foreground(colorText).Bold(true).
			Width(m.width).
			Render(msg)
	}

	left := titleStyle.Render("lazyclaude")
	vis := m.visibleSessions()
	filterLabel := "active"
	if m.showAll {
		filterLabel = "all"
	}
	count := lipgloss.NewStyle().
		Background(colorPrimary).Foreground(colorSubtext).
		Padding(0, 1).
		Render(fmt.Sprintf("%d sessions [%s]", len(vis), filterLabel))
	refresh := lipgloss.NewStyle().
		Background(colorPrimary).Foreground(colorMuted).
		Padding(0, 1).
		Render("updated " + formatDuration(time.Since(m.lastRefresh)))

	bar := lipgloss.JoinHorizontal(lipgloss.Top, left, count, refresh)
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

	sessions := m.visibleSessions()

	if m.loading && len(sessions) == 0 {
		return pStyle.Width(listW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorMuted).Render("loading..."),
		)
	}
	if len(sessions) == 0 {
		msg := "no active sessions"
		if m.showAll {
			msg = "no sessions found"
		}
		return pStyle.Width(listW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorMuted).Render(msg),
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

	header := lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).
		Render(fmt.Sprintf("%-*s %s", nameW, "PROJECT", "STATUS"))
	divider := lipgloss.NewStyle().Foreground(colorBorder).
		Render(strings.Repeat("─", listW))

	var rows []string
	rows = append(rows, header, divider)

	for i := off; i < end; i++ {
		rows = append(rows, m.renderListRow(sessions[i], nameW, i == m.cursor))
	}

	return pStyle.Width(listW).Height(innerH).Render(strings.Join(rows, "\n"))
}

// renderListRow renders exactly ONE line per session, no wrapping.
//
// Layout: [name padded to nameW][status padded to statusColW]
// No pre-styled concatenation — we use fmt.Sprintf for the plain name padding,
// then render each column independently with the SAME background (if selected)
// so the background covers the full row width without gaps.
func (m Model) renderListRow(s *claude.Session, nameW int, selected bool) string {
	statusStr := s.Status.String()
	statusColor, ok := statusColors[statusStr]
	if !ok {
		statusColor = colorMuted
	}

	// Plain-text name, truncated/padded to exact nameW
	name := shortName(s.CWD, nameW)
	name = padRight(name, nameW)

	// Plain-text status, padded to statusColW
	padded := padRight(statusStr, statusColW)

	if selected {
		namePart := lipgloss.NewStyle().
			Background(colorSelBg).Foreground(colorText).Bold(true).
			Render(name)
		statusPart := lipgloss.NewStyle().
			Background(colorSelBg).Foreground(statusColor).Bold(true).
			Render(padded)
		return namePart + statusPart
	}

	namePart := lipgloss.NewStyle().Foreground(colorSubtext).Render(name)
	statusPart := lipgloss.NewStyle().Foreground(statusColor).Render(padded)
	return namePart + statusPart
}

// ── Detail panel ─────────────────────────────────────────────────────────────

func (m Model) renderDetail(detailW int) string {
	_, _, innerH := m.dims()
	pStyle := panelStyle
	if m.focus == 1 {
		pStyle = panelFocusStyle
	}

	sessions := m.visibleSessions()
	if len(sessions) == 0 || m.cursor >= len(sessions) {
		return pStyle.Width(detailW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(colorMuted).Render("select a session"),
		)
	}

	lines := m.buildDetailLines(sessions[m.cursor], detailW)

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

	// CWD (truncated to fit)
	add(lipgloss.NewStyle().Foreground(colorText).Bold(true).
		Render(shortName(s.CWD, width-2)))

	// Status + current tool
	statusLine := statusDot(s.Status.String()) + " " + statusLabel(s.Status.String())
	if s.CurrentTool != "" {
		statusLine += "  " + lipgloss.NewStyle().Foreground(colorPrimary).
			Render("-> "+s.CurrentTool)
	}
	add(statusLine)
	add("")
	add(lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", width-2)))
	add("")

	row := func(label, value string) string {
		return labelStyle.Render(label) + valueStyle.Render(value)
	}

	if s.PID > 0 {
		add(row("PID", fmt.Sprintf("%d", s.PID)))
	} else {
		add(row("PID", lipgloss.NewStyle().Foreground(colorMuted).Render("not running")))
	}

	if s.SessionID != "" {
		sid := s.SessionID
		if len(sid) > 12 {
			sid = sid[:12] + "..."
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

	if s.IsDangerous {
		add(row("Permissions",
			lipgloss.NewStyle().Foreground(colorDanger).Bold(true).
				Render("! dangerously-skip-permissions")))
	}

	add(row("Messages", fmt.Sprintf("%d  (%d user, %d assistant)",
		s.TotalMessages, s.UserMessages, s.AssistantMessages)))
	add(row("Last activity", formatDuration(time.Since(s.LastActivity))))

	if len(s.RecentTools) > 0 {
		add("")
		add(lipgloss.NewStyle().Foreground(colorBorder).Render(strings.Repeat("─", width-2)))
		add(lipgloss.NewStyle().Foreground(colorSubtext).Bold(true).Render("Recent Tools"))
		add("")
		// Most recent first
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
	allLabel := "show all"
	if m.showAll {
		allLabel = "active only"
	}
	if m.focus == 0 {
		parts = []string{
			helpKeyStyle.Render("k/↑") + helpStyle.Render(" prev"),
			helpKeyStyle.Render("j/↓") + helpStyle.Render(" next"),
			helpKeyStyle.Render("tab") + helpStyle.Render(" detail"),
			helpKeyStyle.Render("K") + helpStyle.Render(" kill"),
			helpKeyStyle.Render("a") + helpStyle.Render(" "+allLabel),
			helpKeyStyle.Render("r") + helpStyle.Render(" refresh"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	} else {
		parts = []string{
			helpKeyStyle.Render("k/↑") + helpStyle.Render(" scroll up"),
			helpKeyStyle.Render("j/↓") + helpStyle.Render(" scroll dn"),
			helpKeyStyle.Render("tab") + helpStyle.Render(" list"),
			helpKeyStyle.Render("K") + helpStyle.Render(" kill"),
			helpKeyStyle.Render("a") + helpStyle.Render(" "+allLabel),
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
	// Last resort: truncate base
	if maxLen > 3 {
		return "…" + base[len(base)-(maxLen-1):]
	}
	return base[:maxLen]
}

// padRight pads s with spaces to exactly n chars (byte-length based, fine for ASCII paths).
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

// killProcess sends SIGTERM to the given PID.
func killProcess(pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGTERM)
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
