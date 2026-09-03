package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/limits"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/illegalstudio/lazyagent/internal/sessions"
	"github.com/illegalstudio/lazyagent/internal/version"
)

// tickMsg triggers a full session reload (fallback when file watcher misses events).
type tickMsg time.Time

// renderTickMsg triggers a re-render to keep "X ago" timestamps live — no I/O.
type renderTickMsg time.Time

// sessionsMsg carries newly loaded sessions.
type sessionsMsg struct {
	sessions []*model.Session
	err      error
}

// updateAvailableMsg is sent when a newer release is found on GitHub.
type updateAvailableMsg struct{ version string }

// editorFinishedMsg is sent when a TUI editor (tea.Exec) exits.
type editorFinishedMsg struct{ err error }

// limitsLoadedMsg is sent when a limits fetch completes. The request ID keeps
// an older fetch from overwriting a newer refresh.
type limitsLoadedMsg struct {
	view      limits.View
	requestID uint64
}

func loadLimitsCmd(requestID uint64) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		return limitsLoadedMsg{
			view:      limits.BuildView(limits.FetchAll(ctx), time.Now()),
			requestID: requestID,
		}
	}
}

// Model is the main bubbletea model.
type Model struct {
	theme     Theme
	sty       styles
	actColors map[core.ActivityKind]lipgloss.Color

	manager      *core.SessionManager
	cursor       int
	selectedID   string // session ID of the currently selected item
	listOffset   int
	detailOffset int

	// Progressive first load (see streaming.go): the update/done channels
	// startStreamingLoadCmd created for the in-flight
	// core.SessionManager.ReloadStreaming call, so streamNextCmd can keep
	// waiting on them across repeated Update calls until the stream
	// finishes.
	streamUpdates <-chan struct{}
	streamDone    <-chan struct{}

	width  int
	height int

	err         error
	lastRefresh time.Time
	loading     bool
	focus       int // 0 = list, 1 = detail
	spinFrame   int // animation frame counter for spinners

	// Filter / search
	searchMode  bool
	searchQuery string

	// Cached visible sessions, recomputed via refreshVisible().
	visible []*model.Session

	// Flash message (modal popup, dismissed by any key)
	flashMsg string

	// Inline "copied!" indicator
	copiedAt    time.Time
	copiedField string // "remote", "resume", or "resume-yolo"

	// Update notification shown in footer
	updateVersion string

	// Editor picker popup
	editorPicker       bool
	editorPickerCursor int // 0 = VISUAL (GUI), 1 = EDITOR (TUI)
	editorPickerCWD    string

	// Rename mode
	renameMode      bool
	renameInput     string
	renameSessionID string

	// Limits modal
	limitsOpen    bool
	limitsTab     int // 0 = summary, 1 = detailed
	limitsLoading bool
	limitsView    limits.View
	limitsScroll  int
	limitsRequest uint64
}

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Tab      key.Binding
	Quit     key.Binding
	Rename   key.Binding
	Plus     key.Binding
	Minus    key.Binding
	Filter   key.Binding
	Search   key.Binding
	Esc      key.Binding
	Open     key.Binding
	Copy     key.Binding
	CopyYolo key.Binding
	Limits   key.Binding
}

var keys = keyMap{
	Up:       key.NewBinding(key.WithKeys("up", "k")),
	Down:     key.NewBinding(key.WithKeys("down", "j")),
	Tab:      key.NewBinding(key.WithKeys("tab")),
	Quit:     key.NewBinding(key.WithKeys("q", "ctrl+c")),
	Rename:   key.NewBinding(key.WithKeys("r")),
	Plus:     key.NewBinding(key.WithKeys("+", "=")),
	Minus:    key.NewBinding(key.WithKeys("-")),
	Filter:   key.NewBinding(key.WithKeys("f")),
	Search:   key.NewBinding(key.WithKeys("/")),
	Esc:      key.NewBinding(key.WithKeys("esc")),
	Open:     key.NewBinding(key.WithKeys("o")),
	Copy:     key.NewBinding(key.WithKeys("c")),
	CopyYolo: key.NewBinding(key.WithKeys("C")),
	Limits:   key.NewBinding(key.WithKeys("l")),
}

func NewModel(provider core.SessionProvider, bus *core.EventBus) Model {
	cfg := core.LoadConfig()
	t := LoadTheme(cfg.TUI.Theme)
	mgr := core.NewSessionManager(cfg.WindowMinutes, provider)
	mgr.SetExcludeCWDSubstrings(cfg.ExcludeCWDSubstrings)
	if bus != nil {
		mgr.SetEventBus(bus)
	}
	if dir, ok := sessions.ResolveCacheDir(); ok {
		mgr.EnableCachePersistence(dir)
	}
	_ = mgr.StartWatcher()
	return Model{
		theme:     t,
		sty:       newStyles(t),
		actColors: activityColorMap(t),
		loading:   true,
		manager:   mgr,
	}
}

// Manager returns the underlying SessionManager so callers (main.go) can run
// shutdown logic — e.g. Close(), which flushes the persisted discovery cache
// if EnableCachePersistence was used — after the bubbletea program exits.
func (m Model) Manager() *core.SessionManager {
	return m.manager
}

func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg {
		if v := version.CheckLatest(); v != "" {
			return updateAvailableMsg{version: v}
		}
		return nil
	}
}

func (m Model) Init() tea.Cmd {
	// The initial load is the manager's progressive first load (see
	// streaming.go) rather than a plain Reload: sessions appear as each
	// provider member completes instead of waiting for the slowest one.
	// Subsequent reloads (below, watcher/tick-driven) stay synchronous via
	// makeLoadCmd/Reload -- they're incremental-fast already.
	//
	// BeginReloadStreaming is called synchronously, right here, BEFORE any
	// Cmd in this batch is dispatched -- including watchCmd, whose channel
	// can already have a queued event waiting (StartWatcher ran earlier, in
	// NewModel). That ordering is what actually guarantees
	// core.SessionManager's streamInFlight guard is active before a watcher
	// event or the first tick could otherwise race the stream's own
	// startup; deferring the flag-set into a goroutine spawned by one of
	// these Cmds would not give the same guarantee (Go makes no promise a
	// spawned goroutine's first statement runs before a sibling Cmd's
	// does). See BeginReloadStreaming's doc for the full reasoning.
	run := m.manager.BeginReloadStreaming()
	cmds := []tea.Cmd{runStreamingLoadCmd(run), renderTickCmd(), checkUpdateCmd()}
	if events := m.manager.WatcherEvents(); events != nil {
		cmds = append(cmds, watchCmd(events))
	} else {
		// No file watcher — fall back to periodic reload.
		cmds = append(cmds, tickCmd())
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

// makeLoadCmd loads all JSONL sessions via the SessionManager.
func makeLoadCmd(mgr *core.SessionManager) tea.Cmd {
	return func() tea.Msg {
		err := mgr.Reload()
		if err != nil {
			return sessionsMsg{err: err}
		}
		return sessionsMsg{sessions: mgr.Sessions()}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case updateAvailableMsg:
		m.updateVersion = msg.version

	case editorFinishedMsg:
		// TUI editor exited, bubbletea resumes automatically.

	case limitsLoadedMsg:
		if m.limitsOpen && msg.requestID == m.limitsRequest {
			m.limitsView = msg.view
			m.limitsLoading = false
		}
		return m, nil

	case fileWatchMsg:
		// A JSONL file changed — reload immediately and re-arm the watcher.
		// If the progressive first load is still in flight, Reload() is a
		// no-op (core.SessionManager absorbs it — see streamInFlight) rather
		// than racing the stream's own merges; the sessionsMsg this produces
		// still carries whatever's currently in the manager and is handled
		// exactly like any other reload below.
		return m, tea.Batch(makeLoadCmd(m.manager), watchCmd(m.manager.WatcherEvents()))

	case renderTickMsg:
		// Re-render only — no I/O, but update in-memory activity states.
		m.spinFrame++
		m.manager.UpdateActivities()
		m.refreshVisible()
		return m, renderTickCmd()

	case tickMsg:
		return m, tea.Batch(makeLoadCmd(m.manager), tickCmd())

	case sessionsMsg:
		// Deliberately does NOT touch m.loading: during the progressive
		// first load, only streamDoneMsg (below) may clear it — a
		// watcher/tick-driven reload landing mid-stream must not make the
		// loading indicator disappear early (see fileWatchMsg above).
		m.lastRefresh = time.Now()
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.afterSessionsUpdate()
		}

	case streamStartedMsg:
		// The progressive first load's background goroutine is running;
		// remember its channels so streamNextCmd can keep waiting on them
		// across every subsequent batch/done message.
		m.streamUpdates = msg.updates
		m.streamDone = msg.done
		return m, streamNextCmd(msg.updates, msg.done)

	case streamBatchMsg:
		// A provider member's sessions were just merged into the manager —
		// re-render with whatever's accumulated so far and keep waiting for
		// the next batch or completion. loading stays true.
		m.afterSessionsUpdate()
		return m, streamNextCmd(m.streamUpdates, m.streamDone)

	case streamDoneMsg:
		// The stream has finished — every provider member has completed (or
		// failed; best-effort). This is the only thing that clears loading.
		m.loading = false
		m.lastRefresh = time.Now()
		m.afterSessionsUpdate()
		return m, nil

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

		// Limits modal: intercept keys while open.
		if m.limitsOpen {
			switch msg.String() {
			case "l", "esc", "q":
				m.limitsOpen = false
				m.limitsLoading = false
				m.limitsScroll = 0
				m.limitsView = limits.View{}
			case "tab", "left", "right":
				m.limitsTab = (m.limitsTab + 1) % 2
				m.limitsScroll = 0
			case "down", "j":
				m.limitsScroll++
			case "up", "k":
				if m.limitsScroll > 0 {
					m.limitsScroll--
				}
			case "pgdown":
				m.limitsScroll += 5
			case "pgup":
				m.limitsScroll -= 5
				if m.limitsScroll < 0 {
					m.limitsScroll = 0
				}
			case "r":
				m.limitsLoading = true
				m.limitsRequest++
				return m, loadLimitsCmd(m.limitsRequest)
			}
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

		// Rename mode intercepts all keys except esc/enter.
		if m.renameMode {
			switch msg.String() {
			case "esc":
				m.renameMode = false
				m.renameInput = ""
				m.renameSessionID = ""
			case "enter":
				if m.renameSessionID != "" {
					_ = m.manager.SetSessionName(m.renameSessionID, strings.TrimSpace(m.renameInput))
				}
				m.renameMode = false
				m.renameInput = ""
				m.renameSessionID = ""
			case "backspace":
				runes := []rune(m.renameInput)
				if len(runes) > 0 {
					m.renameInput = string(runes[:len(runes)-1])
				}
			default:
				if len(msg.Runes) == 1 {
					m.renameInput += string(msg.Runes)
				}
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
				runes := []rune(m.searchQuery)
				if len(runes) > 0 {
					m.searchQuery = string(runes[:len(runes)-1])
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
		case key.Matches(msg, keys.Limits):
			m.limitsOpen = true
			m.limitsLoading = true
			m.limitsTab = 0
			m.limitsScroll = 0
			m.limitsView = limits.View{}
			m.limitsRequest++
			return m, loadLimitsCmd(m.limitsRequest)

		case key.Matches(msg, keys.Quit):
			return m, tea.Quit

		case key.Matches(msg, keys.Tab):
			m.focus = (m.focus + 1) % 2
			m.detailOffset = 0

		case key.Matches(msg, keys.Plus):
			m.manager.SetWindowMinutes(m.manager.WindowMinutes() + 10)
			m.refreshVisible()
			if n := len(m.visible); m.cursor >= n && n > 0 {
				m.cursor = n - 1
			}

		case key.Matches(msg, keys.Minus):
			m.manager.SetWindowMinutes(m.manager.WindowMinutes() - 10)
			m.refreshVisible()
			if n := len(m.visible); m.cursor >= n && n > 0 {
				m.cursor = n - 1
			}

		case key.Matches(msg, keys.Rename):
			if len(m.visible) > 0 && m.cursor < len(m.visible) {
				sess := m.visible[m.cursor]
				m.renameMode = true
				m.renameSessionID = sess.SessionID
				m.renameInput = m.manager.SessionName(sess.SessionID)
			}

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
			m.manager.SetActivityFilter(core.NextActivityFilter(m.manager.ActivityFilter()))
			m.cursor = 0
			m.listOffset = 0
			m.refreshVisible()

		case key.Matches(msg, keys.Open):
			if len(m.visible) > 0 && m.cursor < len(m.visible) {
				s := m.visible[m.cursor]
				cwd := s.CWD

				// Cursor sessions open in Cursor IDE if available.
				if s.Agent == "cursor" && cwd != "" {
					if _, err := exec.LookPath("cursor"); err == nil {
						c := exec.Command("cursor", cwd)
						c.Stdin = nil
						c.Stdout = nil
						c.Stderr = nil
						_ = c.Start()
						break
					}
				}

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

		case key.Matches(msg, keys.Copy):
			if len(m.visible) > 0 && m.cursor < len(m.visible) {
				s := m.visible[m.cursor]
				if cmd := core.ResumeCommand(s.Agent, s.SessionID); cmd != "" {
					if core.CopyToClipboard(cmd) == nil {
						m.copiedAt = time.Now()
						m.copiedField = "resume"
					}
				}
			}

		case key.Matches(msg, keys.CopyYolo):
			if len(m.visible) > 0 && m.cursor < len(m.visible) {
				s := m.visible[m.cursor]
				if cmd := core.YoloResumeCommand(s.Agent, s.SessionID); cmd != "" {
					if core.CopyToClipboard(cmd) == nil {
						m.copiedAt = time.Now()
						m.copiedField = "resume-yolo"
					}
				} else if core.ResumeCommand(s.Agent, s.SessionID) != "" {
					m.flashMsg = fmt.Sprintf("No YOLO resume mode available for %s sessions.", s.Agent)
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
			// Copy remote URL to clipboard on detail panel click.
			if m.cursor >= 0 && m.cursor < len(m.visible) {
				if u := m.visible[m.cursor].RemoteURL; u != "" {
					if core.CopyToClipboard(u) == nil {
						m.copiedAt = time.Now()
						m.copiedField = "remote"
					}
				}
			}
		}
	}
}

// refreshVisible recomputes the cached visible sessions list via the SessionManager.
func (m *Model) refreshVisible() {
	m.manager.SetSearchQuery(m.searchQuery)
	m.visible = m.manager.VisibleSessions()
}

// afterSessionsUpdate recomputes the visible list and preserves cursor
// selection by session ID (or clamps into range if the previously selected
// session disappeared) whenever the underlying session set changes — shared
// by the periodic-reload sessionsMsg handler and the progressive
// first-load's streamBatchMsg/streamDoneMsg handlers, which all need
// identical selection-preserving behavior each time the manager's snapshot
// grows or is replaced.
func (m *Model) afterSessionsUpdate() {
	m.refreshVisible()
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
		if n := len(m.visible); m.cursor >= n && n > 0 {
			m.cursor = n - 1
		}
		if len(m.visible) > 0 {
			m.selectedID = m.visible[m.cursor].SessionID
		}
	}
	m.ensureListVisible()
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
			opt0 = lipgloss.NewStyle().Background(m.theme.SelectionBg).Foreground(m.theme.Text).Bold(true).Render("▸ " + visual + "  (GUI)")
			opt1 = lipgloss.NewStyle().Foreground(m.theme.Subtext).Render(opt1)
		} else {
			opt0 = lipgloss.NewStyle().Foreground(m.theme.Subtext).Render(opt0)
			opt1 = lipgloss.NewStyle().Background(m.theme.SelectionBg).Foreground(m.theme.Text).Bold(true).Render("▸ " + editor + "  (TUI)")
		}

		title := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render("Open with:")
		hint := lipgloss.NewStyle().Foreground(m.theme.Muted).Render("↑/↓ select  enter confirm  esc cancel")
		body := title + "\n\n" + opt0 + "\n" + opt1 + "\n\n" + hint

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.theme.BorderFocus).
			Background(m.theme.ModalBg).
			Foreground(m.theme.Text).
			Padding(1, 3).
			Render(body)

		out = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			box,
			lipgloss.WithWhitespaceBackground(m.theme.OverlayBg),
		)
	}

	// Overlay rename input.
	if m.renameMode {
		title := lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render("Rename session:")
		input := lipgloss.NewStyle().Foreground(m.theme.Accent).Render(m.renameInput + "█")
		hint := lipgloss.NewStyle().Foreground(m.theme.Muted).Render("enter confirm  esc cancel  empty = reset")
		body := title + "\n\n" + input + "\n\n" + hint

		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.theme.BorderFocus).
			Background(m.theme.ModalBg).
			Foreground(m.theme.Text).
			Padding(1, 3).
			Width(40).
			Render(body)

		out = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			box,
			lipgloss.WithWhitespaceBackground(m.theme.OverlayBg),
		)
	}

	// Overlay flash message centered over the existing UI.
	if m.flashMsg != "" {
		dismiss := lipgloss.NewStyle().Foreground(m.theme.Muted).Render("Press any key to continue")
		body := m.flashMsg + "\n\n" + dismiss
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(m.theme.Warning).
			Background(m.theme.ModalBg).
			Foreground(m.theme.Text).
			Padding(1, 3).
			Render(body)

		out = lipgloss.Place(m.width, m.height,
			lipgloss.Center, lipgloss.Center,
			box,
			lipgloss.WithWhitespaceBackground(m.theme.OverlayBg),
		)
	}

	// Overlay limits modal.
	if m.limitsOpen {
		out = m.renderLimitsModal()
	}

	return out
}

func (m Model) renderTitleBar() string {
	title := "lazyagent"
	if version.Version != "dev" {
		title += " v" + version.Version
	}
	left := m.sty.title.Render(title)
	countLabel := fmt.Sprintf("%d sessions [last %dm]", len(m.visible), m.manager.WindowMinutes())
	if m.loading {
		// Progressive first load still in flight (see streaming.go) —
		// disappears the moment streamDoneMsg clears m.loading.
		countLabel += "  loading…"
	}
	count := lipgloss.NewStyle().
		Background(m.theme.Primary).Foreground(m.theme.TitleSubtext).
		Padding(0, 1).
		Render(countLabel)

	parts := []string{left, count}

	if af := m.manager.ActivityFilter(); af != "" {
		filterBadge := lipgloss.NewStyle().
			Background(m.theme.Primary).Foreground(m.theme.TitleWarning).Bold(true).
			Padding(0, 1).
			Render("▸ " + string(af))
		parts = append(parts, filterBadge)
	}

	refresh := lipgloss.NewStyle().
		Background(m.theme.Primary).Foreground(m.theme.TitleMuted).
		Padding(0, 1).
		Render("updated " + core.FormatDuration(time.Since(m.lastRefresh)))
	parts = append(parts, refresh)

	bar := lipgloss.JoinHorizontal(lipgloss.Top, parts...)
	return lipgloss.NewStyle().
		Background(m.theme.Primary).
		Width(m.width).
		Render(bar)
}

// ── List panel ───────────────────────────────────────────────────────────────

const statusColW = 11 // "processing" = 10 chars + 1 padding

func (m Model) renderList(listW, innerH int) string {
	pStyle := m.sty.panel
	if m.focus == 0 {
		pStyle = m.sty.panelFocus
	}

	sessions := m.visible

	if m.loading && len(sessions) == 0 {
		return pStyle.Width(listW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(m.theme.Muted).Render("loading..."),
		)
	}
	if len(sessions) == 0 && !m.searchMode {
		return pStyle.Width(listW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(m.theme.Muted).Render("no sessions found"),
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
	off := core.Clamp(0, maxOff, m.listOffset)
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
		header = lipgloss.NewStyle().Foreground(m.theme.Warning).Bold(true).
			Render("/ " + m.searchQuery + "█")
	} else {
		projectLabel := "PROJECT"
		if af := m.manager.ActivityFilter(); af != "" {
			projectLabel += " [" + string(af) + "]"
		}
		header = lipgloss.NewStyle().Foreground(m.theme.Subtext).Bold(true).
			Render(fmt.Sprintf("%-*s %s", nameW+sparkW, projectLabel, "STATUS"))
	}
	divider := lipgloss.NewStyle().Foreground(m.theme.Border).
		Render(strings.Repeat("─", listW))

	var rows []string
	rows = append(rows, header, divider)

	if len(sessions) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(m.theme.Muted).Render("no results"))
		return pStyle.Width(listW).Height(innerH).Render(strings.Join(rows, "\n"))
	}

	for i := off; i < end; i++ {
		rows = append(rows, m.renderListRow(sessions[i], nameW, sparkW, i == m.cursor))
	}

	return pStyle.Width(listW).Height(innerH).Render(strings.Join(rows, "\n"))
}

func (m Model) renderListRow(s *model.Session, nameW, sparkW int, selected bool) string {
	activity := m.manager.ActivityFor(s.SessionID)
	actColor, ok := m.actColors[activity]
	if !ok {
		actColor = m.theme.Muted
	}

	actStr := core.PadRight(string(activity), statusColW)
	if core.IsActiveActivity(activity) {
		spin := string(core.SpinnerFrames[m.spinFrame%len(core.SpinnerFrames)])
		actStr = spin + core.PadRight(string(activity), statusColW-1)
	}

	customName := m.manager.SessionName(s.SessionID)
	// Agent/source badge for visual distinction.
	agentPrefix := ""
	if s.Agent == "pi" {
		agentPrefix = "π "
	} else if s.Agent == "opencode" {
		agentPrefix = "O "
	} else if s.Agent == "kilo" {
		agentPrefix = "L "
	} else if s.Agent == "cursor" {
		agentPrefix = "C "
	} else if s.Agent == "grok" {
		agentPrefix = "G "
	} else if s.Agent == "kimi" {
		agentPrefix = "K "
	} else if s.Desktop != nil {
		agentPrefix = "D "
	}
	// Priority: manual rename > agent-provided name > CWD short name
	displayName := ""
	if customName != "" {
		displayName = customName
	} else if s.Name != "" {
		displayName = s.Name
	}
	var name string
	if displayName != "" {
		name = core.TruncateCells(agentPrefix+displayName, nameW, "…")
	} else {
		nameBudget := nameW - core.DisplayWidth(agentPrefix)
		if nameBudget < 0 {
			nameBudget = 0
		}
		name = agentPrefix + core.ShortName(s.CWD, nameBudget)
		name = core.TruncateCells(name, nameW, "")
	}
	name = core.PadRight(name, nameW)

	var sparkStr string
	if sparkW > 0 {
		spark := core.RenderSparkline(s.EntryTimestamps, time.Duration(m.manager.WindowMinutes())*time.Minute, sparkW-2)
		sparkStr = " " + spark + " "
	}

	nameStyle := lipgloss.NewStyle().Foreground(m.theme.Subtext)
	actStyle := lipgloss.NewStyle().Foreground(actColor)
	sparkStyle := actStyle
	if selected {
		nameStyle = nameStyle.Background(m.theme.SelectionBg).Foreground(m.theme.Text).Bold(true)
		sparkStyle = sparkStyle.Background(m.theme.SelectionBg)
		actStyle = actStyle.Background(m.theme.SelectionBg).Bold(true)
	}

	return nameStyle.Render(name) + sparkStyle.Render(sparkStr) + actStyle.Render(actStr)
}

// ── Detail panel ─────────────────────────────────────────────────────────────

func (m Model) renderDetail(detailW, innerH int) string {
	pStyle := m.sty.panel
	if m.focus == 1 {
		pStyle = m.sty.panelFocus
	}

	if m.err != nil && len(m.visible) == 0 {
		return pStyle.Width(detailW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(m.theme.Warning).Render("error: " + m.err.Error()),
		)
	}
	if len(m.visible) == 0 || m.cursor >= len(m.visible) {
		return pStyle.Width(detailW).Height(innerH).Render(
			lipgloss.NewStyle().Foreground(m.theme.Muted).Render("select a session"),
		)
	}

	lines := m.buildDetailLines(m.visible[m.cursor], detailW)

	vis := innerH
	maxOff := len(lines) - vis
	if maxOff < 0 {
		maxOff = 0
	}
	off := core.Clamp(0, maxOff, m.detailOffset)
	end := off + vis
	if end > len(lines) {
		end = len(lines)
	}

	return pStyle.Width(detailW).Height(innerH).Render(
		strings.Join(lines[off:end], "\n"),
	)
}

func (m Model) buildDetailLines(s *model.Session, width int) []string {
	var lines []string
	add := func(line string) { lines = append(lines, line) }

	detailTitle := m.manager.SessionName(s.SessionID)
	if detailTitle == "" && s.Name != "" {
		detailTitle = s.Name
	}
	if detailTitle == "" {
		detailTitle = core.ShortName(s.CWD, width-2)
	}
	add(lipgloss.NewStyle().Foreground(m.theme.Text).Bold(true).Render(detailTitle))

	activity := m.manager.ActivityFor(s.SessionID)
	actColor := m.actColors[activity]
	statusLine := lipgloss.NewStyle().Foreground(actColor).Bold(true).Render("● ") +
		lipgloss.NewStyle().Foreground(actColor).Bold(true).Render(string(activity))
	if s.CurrentTool != "" {
		statusLine += "  " + lipgloss.NewStyle().Foreground(m.theme.Muted).
			Render("("+s.CurrentTool+")")
	}
	add(statusLine)
	add("")
	add(lipgloss.NewStyle().Foreground(m.theme.Border).Render(strings.Repeat("─", width-2)))
	add("")

	row := func(label, value string) string {
		return m.sty.label.Render(label) + m.sty.value.Render(value)
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
	if s.Agent != "" {
		add(row("Agent", s.Agent))
	}
	if s.Desktop != nil {
		add(row("Source", "Claude Desktop"))
		if s.Desktop.Title != "" {
			add(row("Title", s.Desktop.Title))
		}
		if s.Desktop.PermissionMode != "" {
			add(row("Permissions", s.Desktop.PermissionMode))
		}
	} else if s.Agent == "claude" {
		add(row("Source", "CLI"))
	}
	if s.GitBranch != "" && s.GitBranch != "HEAD" {
		add(row("Git Branch", s.GitBranch))
	}
	if s.RemoteURL != "" {
		remoteVal := lipgloss.NewStyle().Foreground(m.theme.Accent).Render(s.RemoteURL)
		if m.copiedField == "remote" && time.Since(m.copiedAt) < 2*time.Second {
			remoteVal += lipgloss.NewStyle().Foreground(m.theme.Muted).Render("  copied!")
		}
		add(row("Remote", remoteVal))
	}
	if cmd := core.ResumeCommand(s.Agent, s.SessionID); cmd != "" {
		resumeVal := lipgloss.NewStyle().Foreground(m.theme.Accent).Render(cmd)
		if m.copiedField == "resume" && time.Since(m.copiedAt) < 2*time.Second {
			resumeVal += lipgloss.NewStyle().Foreground(m.theme.Muted).Render("  copied!")
		}
		add(row("Resume", resumeVal))
	}
	if cmd := core.YoloResumeCommand(s.Agent, s.SessionID); cmd != "" {
		resumeVal := lipgloss.NewStyle().Foreground(m.theme.Warning).Render(cmd)
		if m.copiedField == "resume-yolo" && time.Since(m.copiedAt) < 2*time.Second {
			resumeVal += lipgloss.NewStyle().Foreground(m.theme.Muted).Render("  copied!")
		}
		add(row("Resume YOLO", resumeVal))
	}

	wtStr := "no"
	if s.IsWorktree {
		wtStr = lipgloss.NewStyle().Foreground(m.theme.Accent).Render("yes")
		if s.MainRepo != "" {
			wtStr += lipgloss.NewStyle().Foreground(m.theme.Subtext).
				Render(" (" + core.ShortName(s.MainRepo, 28) + ")")
		}
	}
	add(row("Worktree", wtStr))

	add(row("Messages", fmt.Sprintf("%d  (%d user, %d assistant)",
		s.TotalMessages, s.UserMessages, s.AssistantMessages)))

	if s.InputTokens > 0 || s.OutputTokens > 0 {
		cost := core.EffectiveCost(s.Model, s.CostUSD, s.InputTokens, s.OutputTokens, s.CacheCreationTokens, s.CacheReadTokens)
		tokenInfo := core.FormatTokens(s.InputTokens+s.CacheCreationTokens+s.CacheReadTokens) + " in / " + core.FormatTokens(s.OutputTokens) + " out"
		if cost > 0.001 {
			tokenInfo += "  " + lipgloss.NewStyle().Foreground(m.theme.Accent).Render(core.FormatCost(cost))
		}
		add(row("Tokens", tokenInfo))
	}

	if len(s.RecentTools) > 0 {
		last := s.RecentTools[len(s.RecentTools)-1]
		add(row("Last operation", last.Name+"  "+
			lipgloss.NewStyle().Foreground(m.theme.Muted).Render("("+core.FormatDuration(time.Since(last.Timestamp))+")")))
	} else {
		add(row("Last operation", core.FormatDuration(time.Since(s.LastActivity))))
	}

	if s.LastFileWrite != "" {
		agePart := " (" + core.FormatDuration(time.Since(s.LastFileWriteAt)) + ")"
		maxFile := width - 2 - 22 - core.DisplayWidth(agePart)
		if maxFile < 4 {
			maxFile = 4
		}
		filePart := core.ShortName(s.LastFileWrite, maxFile)
		add(row("Last file", filePart+lipgloss.NewStyle().Foreground(m.theme.Muted).Render(agePart)))
	}

	if len(s.RecentMessages) > 0 {
		add("")
		add(lipgloss.NewStyle().Foreground(m.theme.Border).Render(strings.Repeat("─", width-2)))
		add(lipgloss.NewStyle().Foreground(m.theme.Subtext).Bold(true).Render("Conversation"))
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
			role := core.PadRight(roleLabel, 4)
			text := msg.Text
			// Collapse newlines for single-line display
			text = strings.ReplaceAll(text, "\n", " ")
			text = core.TruncateCells(text, msgW, "…")
			add(lipgloss.NewStyle().Foreground(m.theme.Subtext).Render("  "+role+"  ") +
				lipgloss.NewStyle().Foreground(m.theme.Text).Render(text))
		}
	}

	if len(s.RecentTools) > 0 {
		add("")
		add(lipgloss.NewStyle().Foreground(m.theme.Border).Render(strings.Repeat("─", width-2)))
		add(lipgloss.NewStyle().Foreground(m.theme.Subtext).Bold(true).Render("Recent Tools"))
		add("")
		tools := s.RecentTools
		if len(tools) > 20 {
			tools = tools[len(tools)-20:]
		}
		for i := len(tools) - 1; i >= 0; i-- {
			tc := tools[i]
			line := lipgloss.NewStyle().Foreground(m.theme.Primary).Render("  " + tc.Name)
			// Only timestamped tool calls get a relative age — agents like
			// Grok record a per-tool time only for the most recent call.
			if !tc.Timestamp.IsZero() {
				line += lipgloss.NewStyle().Foreground(m.theme.Muted).Render("  " + core.FormatDuration(time.Since(tc.Timestamp)))
			}
			add(line)
		}
	}

	return lines
}

// ── Help bar ─────────────────────────────────────────────────────────────────

func (m Model) renderHelp() string {
	var parts []string
	if m.searchMode {
		parts = []string{
			m.sty.helpKey.Render("esc") + m.sty.help.Render(" clear"),
			m.sty.helpKey.Render("backspace") + m.sty.help.Render(" del"),
		}
		return m.sty.help.Width(m.width).Render(strings.Join(parts, "  "))
	}

	if m.focus == 0 {
		parts = append(parts,
			m.sty.helpKey.Render("k/↑")+m.sty.help.Render(" prev"),
			m.sty.helpKey.Render("j/↓")+m.sty.help.Render(" next"),
			m.sty.helpKey.Render("tab")+m.sty.help.Render(" detail"),
			m.sty.helpKey.Render("click")+m.sty.help.Render(" select"),
		)
	} else {
		parts = append(parts,
			m.sty.helpKey.Render("k/↑")+m.sty.help.Render(" scroll up"),
			m.sty.helpKey.Render("j/↓")+m.sty.help.Render(" scroll dn"),
			m.sty.helpKey.Render("tab")+m.sty.help.Render(" list"),
			m.sty.helpKey.Render("click")+m.sty.help.Render(" focus"),
		)
	}
	parts = append(parts,
		m.sty.helpKey.Render("scroll")+m.sty.help.Render(" navigate"),
		m.sty.helpKey.Render("+/-")+m.sty.help.Render(" mins"),
		m.sty.helpKey.Render("f")+m.sty.help.Render(" filter"),
		m.sty.helpKey.Render("l")+m.sty.help.Render(" limits"),
		m.sty.helpKey.Render("/")+m.sty.help.Render(" search"),
		m.sty.helpKey.Render("o")+m.sty.help.Render(" open"),
		m.sty.helpKey.Render("c/C")+m.sty.help.Render(" copy normal/YOLO"),
		m.sty.helpKey.Render("r")+m.sty.help.Render(" rename"),
		m.sty.helpKey.Render("q")+m.sty.help.Render(" quit"),
	)
	helpLine := m.sty.help.Width(m.width).Render(strings.Join(parts, "  "))
	if m.updateVersion != "" {
		updateLine := lipgloss.NewStyle().
			Foreground(m.theme.Accent).
			Background(m.theme.HelpBg).
			Width(m.width).
			Render(fmt.Sprintf("  ↑ lazyagent %s available — https://github.com/illegalstudio/lazyagent/releases", m.updateVersion))
		return updateLine + "\n" + helpLine
	}
	return helpLine
}
