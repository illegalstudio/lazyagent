package sessions

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/chatops"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// pickerAction is what the user chose when the picker exited.
type pickerAction int

const (
	actionQuit pickerAction = iota
	actionOpen
	actionCopy
	// actionEmpty fires when the discovery stream finishes with zero
	// sessions total — distinct from actionQuit (an explicit user quit)
	// so Run can print the "No sessions found" message only in this case.
	actionEmpty
)

// sessionBatchMsg carries one discovery member's already dir-filtered
// results (sessions/titles, parallel slices) as they arrive from the
// streaming discovery goroutine. See runPicker.
type sessionBatchMsg struct {
	sessions []*model.Session
	titles   []string
}

// streamDoneMsg signals that every discovery member has finished (the
// DiscoverMatchingStream done channel closed). See runPicker.
type streamDoneMsg struct{}

// agentColors maps agent keys to identity colors. The keys shared with the
// prune/compact selectors reuse their palette; the rest stay in the family.
var agentColors = map[string]lipgloss.Color{
	"claude":   "#E7A15E",
	"pi":       "#F38BA8",
	"codex":    "#A6E3A1",
	"grok":     "#89B4FA",
	"kimi":     "#CBA6F7",
	"opencode": "#94E2D5",
	"kilo":     "#F9E2AF",
	"cursor":   "#B4BEFE",
	"amp":      "#EBA0AC",
}

var (
	stylePickerBox = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(chatops.ColBorderDim).
			Padding(0, 1)
	styleCursor = lipgloss.NewStyle().Foreground(chatops.ColPrimary).Bold(true)
	styleStatus = lipgloss.NewStyle().Foreground(chatops.ColWarn)
)

type pickerModel struct {
	sessions []*model.Session
	titles   []string // pre-computed row titles, same indexing as sessions
	cursor   int
	action   pickerAction
	// chosen is the concrete session the user acted on, captured
	// at the moment of the decision rather than re-derived later from
	// sessions[cursor]. A batch can still be in flight on p.msgs when the
	// user decides -- both a sessionBatchMsg and the QuitMsg from tea.Quit
	// travel through the same channel -- so if that batch is processed
	// after the key and re-sorts the list, sessions[cursor] could end up
	// pointing at a different session than the one the user actually
	// looked at. Capturing it here means runPicker never has to re-derive
	// it from a cursor that may have moved.
	chosen *model.Session
	// resumeMode is the mode selected for the open or copy action. It also
	// supplies the default used by enter when the picker was launched
	// with lazyagent sessions --yolo.
	resumeMode core.ResumeMode
	status     string
	// decided is set the moment updateKey sets a terminal action
	// (actionOpen/actionCopy/actionQuit). Once true, applyBatch and
	// applyStreamDone become no-ops: the decision has already been made
	// and must not be disturbed by a message that was already in flight.
	decided  bool
	dirLabel string
	now      time.Time
	height   int // terminal height from the last tea.WindowSizeMsg; 0 = unknown

	// Streaming discovery state (see sessionBatchMsg/streamDoneMsg). loading
	// is true until the discovery stream's done message arrives; total is
	// the number of discovery members expected (memberCount), doneCount the
	// number that have delivered a batch so far, bounded by total. A member
	// that fails contributes no batch at all (best-effort), so doneCount can
	// stay below total even after loading flips false -- the footer just
	// switches away from the progress text at that point regardless.
	loading   bool
	doneCount int
	total     int
}

// defaultVisibleRows is the row budget View falls back to when the terminal
// height is unknown (height == 0, e.g. in unit tests that never deliver a
// tea.WindowSizeMsg).
const defaultVisibleRows = 20

// chromeLines is the number of non-row lines View always emits: the box's
// top border, the title, the blank line separating the title from the rows,
// the box's bottom border, and the footer. The status line, when present,
// and the "N more" clip indicators are counted on top of this.
const chromeLines = 5

func (m pickerModel) Init() tea.Cmd { return nil }

func (m pickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		return m, nil
	case tea.KeyMsg:
		return m.updateKey(msg)
	case sessionBatchMsg:
		return m.applyBatch(msg), nil
	case streamDoneMsg:
		return m.applyStreamDone()
	}
	return m, nil
}

// applyBatch merges one discovery member's batch into the accumulated
// list, keeps the cursor on whatever session it was tracking (see
// relocateCursor), and advances the loading progress counter.
//
// It's a no-op once the user has already made a terminal decision
// (m.decided): a batch can still be in flight on p.msgs at the moment
// enter/c/q is pressed (see the chosen field's doc comment), and once the
// program is quitting, nothing should keep mutating the list, the cursor,
// or the loading counters underneath that decision.
func (m pickerModel) applyBatch(msg sessionBatchMsg) pickerModel {
	if m.decided {
		return m
	}
	prevSessions, prevCursor := m.sessions, m.cursor
	m.sessions, m.titles = mergeSessions(m.sessions, m.titles, msg.sessions, msg.titles)
	m.cursor = relocateCursor(prevSessions, prevCursor, m.sessions)
	if m.doneCount < m.total {
		m.doneCount++
	}
	return m
}

// applyStreamDone reacts to every discovery member having finished: if
// nothing was ever found, the picker quits itself with actionEmpty so Run
// can print the "No sessions found" message; otherwise it just switches the
// footer out of its loading state.
//
// Like applyBatch, this is a no-op once m.decided: without this guard, a
// streamDoneMsg arriving right after an explicit quit on an
// still-empty-so-far list would flip m.action from the user's actionQuit
// to actionEmpty, corrupting a decision that had already been made. In
// practice DiscoverMatchingStream never sends a batch after streamDoneMsg
// (done only closes once every member's emit has already been delivered),
// so this guard's only real job is protecting against a decision racing
// the done message itself, not a batch racing after it.
func (m pickerModel) applyStreamDone() (tea.Model, tea.Cmd) {
	if m.decided {
		return m, nil
	}
	m.loading = false
	if len(m.sessions) == 0 {
		m.action = actionEmpty
		m.decided = true
		return m, tea.Quit
	}
	return m, nil
}

func (m pickerModel) updateKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		m.status = ""
	case "down", "j":
		if m.cursor < len(m.sessions)-1 {
			m.cursor++
		}
		m.status = ""
	case "enter":
		return m.chooseOpen(m.resumeMode)
	case "n":
		return m.chooseOpen(core.ResumeNormal)
	case "y":
		return m.chooseOpen(core.ResumeYolo)
	case "c":
		return m.chooseCopy(core.ResumeNormal)
	case "C":
		return m.chooseCopy(core.ResumeYolo)
	case "q", "esc", "ctrl+c":
		m.action = actionQuit
		m.decided = true
		return m, tea.Quit
	}
	return m, nil
}

func (m pickerModel) chooseOpen(mode core.ResumeMode) (tea.Model, tea.Cmd) {
	// During loading the list can still be empty. There is nothing to act on
	// until the first batch arrives.
	if len(m.sessions) == 0 {
		return m, nil
	}
	s := m.sessions[m.cursor]
	if core.ResumeArgvWithMode(s.Agent, s.SessionID, mode) == nil {
		if mode == core.ResumeYolo && core.ResumeArgv(s.Agent, s.SessionID) != nil {
			m.status = fmt.Sprintf("no YOLO mode available for %s sessions", s.Agent)
		} else {
			m.status = fmt.Sprintf("no resume available for %s sessions", s.Agent)
		}
		return m, nil
	}
	m.action = actionOpen
	m.chosen = s
	m.resumeMode = mode
	m.decided = true
	return m, tea.Quit
}

func (m pickerModel) chooseCopy(mode core.ResumeMode) (tea.Model, tea.Cmd) {
	if len(m.sessions) == 0 {
		return m, nil
	}
	s := m.sessions[m.cursor]
	if core.ResumeCommandWithMode(s.Agent, s.SessionID, mode) == "" {
		if mode == core.ResumeYolo && core.ResumeCommand(s.Agent, s.SessionID) != "" {
			m.status = fmt.Sprintf("no YOLO mode available for %s sessions", s.Agent)
		} else {
			m.status = fmt.Sprintf("no resume command for %s sessions", s.Agent)
		}
		return m, nil
	}
	m.action = actionCopy
	m.chosen = s
	m.resumeMode = mode
	m.decided = true
	return m, tea.Quit
}

// rowBudget returns how many session rows View can render: the terminal
// height minus chromeLines minus one more if a status line is showing, with
// a floor of 1. Falls back to defaultVisibleRows when the height is unknown.
func (m pickerModel) rowBudget() int {
	if m.height <= 0 {
		return defaultVisibleRows
	}
	budget := m.height - chromeLines
	if m.status != "" {
		budget--
	}
	if budget < 1 {
		budget = 1
	}
	return budget
}

// visibleRange returns the half-open row window [start, end) of at most
// height rows that contains cursor, scrolling the window — never the
// cursor — as needed to stay within [0, total). When the whole list fits,
// it returns the full range.
func visibleRange(cursor, total, height int) (start, end int) {
	if height <= 0 || total <= height {
		return 0, total
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	end = start + height
	if end > total {
		end = total
		start = end - height
		if start < 0 {
			start = 0
		}
	}
	return start, end
}

// mergeSessions merges an incoming discovery batch (batchSessions/
// batchTitles, parallel slices) into the already-accumulated sessions/
// titles (also parallel), returning a new pair of parallel slices sorted by
// bySessionRecency — the same ordering rule FilterByDir uses, so the merged
// list is deterministic at any point in the stream regardless of the order
// batches arrive in. Titles travel alongside their session through the sort
// so the two slices never desync.
func mergeSessions(sessions []*model.Session, titles []string, batchSessions []*model.Session, batchTitles []string) ([]*model.Session, []string) {
	type row struct {
		session *model.Session
		title   string
	}
	rows := make([]row, 0, len(sessions)+len(batchSessions))
	for i, s := range sessions {
		rows = append(rows, row{s, titles[i]})
	}
	for i, s := range batchSessions {
		rows = append(rows, row{s, batchTitles[i]})
	}
	slices.SortStableFunc(rows, func(a, b row) int {
		return bySessionRecency(a.session, b.session)
	})
	mergedSessions := make([]*model.Session, len(rows))
	mergedTitles := make([]string, len(rows))
	for i, r := range rows {
		mergedSessions[i] = r.session
		mergedTitles[i] = r.title
	}
	return mergedSessions, mergedTitles
}

// relocateCursor computes where the cursor should land in merged given
// where it was before the merge (prevCursor into prevSessions).
//
// If the user hasn't navigated away from the top row yet (prevCursor <= 0),
// the cursor stays pinned to the top: there's no meaningful selection to
// preserve, so we don't try to "follow" whichever session happened to be
// first. Otherwise the previously selected session's SessionID is looked up
// in merged and the cursor follows it to its new index; if that session is
// no longer present, the cursor clamps to the nearest valid index.
func relocateCursor(prevSessions []*model.Session, prevCursor int, merged []*model.Session) int {
	if len(merged) == 0 {
		return 0
	}
	if prevCursor <= 0 || prevCursor >= len(prevSessions) {
		return 0
	}
	selectedID := prevSessions[prevCursor].SessionID
	for i, s := range merged {
		if s.SessionID == selectedID {
			return i
		}
	}
	// The previously selected session disappeared: clamp to the nearest
	// valid index (sessions are only ever added by this feature's merges,
	// so this branch is defensive rather than something that happens in
	// practice today).
	if prevCursor >= len(merged) {
		return len(merged) - 1
	}
	return prevCursor
}

func (m pickerModel) View() string {
	total := len(m.sessions)
	budget := m.rowBudget()
	start, end := visibleRange(m.cursor, total, budget)
	clippedAbove := start > 0
	clippedBelow := end < total
	// The clip indicators themselves consume rows out of the same budget,
	// so once we know we need one, shrink the budget and re-window.
	if clippedAbove {
		budget--
	}
	if clippedBelow {
		budget--
	}
	if clippedAbove || clippedBelow {
		if budget < 1 {
			budget = 1
		}
		start, end = visibleRange(m.cursor, total, budget)
		clippedAbove = start > 0
		clippedBelow = end < total
	}

	var rows []string
	if clippedAbove {
		rows = append(rows, chatops.StyleMuted.Render(fmt.Sprintf("  … %d more above", start)))
	}
	for i := start; i < end; i++ {
		s := m.sessions[i]
		marker := "  "
		if i == m.cursor {
			marker = styleCursor.Render("▸ ")
		}
		agent := lipgloss.NewStyle().Foreground(agentColor(s.Agent)).Render(fmt.Sprintf("%-8s", s.Agent))
		row := fmt.Sprintf("%s%s %-10s %4d  %s",
			marker, agent, relTime(s.LastActivity, m.now), s.TotalMessages, m.titles[i])
		if core.ResumeCommand(s.Agent, s.SessionID) == "" {
			row += chatops.StyleMuted.Render("  (no resume)")
		}
		rows = append(rows, row)
	}
	if clippedBelow {
		rows = append(rows, chatops.StyleMuted.Render(fmt.Sprintf("  … %d more below", total-end)))
	}

	title := chatops.StyleTableHeader.Render(fmt.Sprintf("Sessions in %s (%d)", m.dirLabel, total))
	box := stylePickerBox.Render(title + "\n\n" + strings.Join(rows, "\n"))
	footer := chatops.StyleFooter.Render(m.footerText())
	if m.status != "" {
		footer += "\n" + styleStatus.Render("  "+m.status)
	}
	return box + "\n" + footer + "\n"
}

// footerText is the footer line's content: a loading indicator with
// progress while the discovery stream is still running, the normal
// keybinding hint once it's done.
func (m pickerModel) footerText() string {
	if m.loading {
		return fmt.Sprintf("  loading agents… (%d/%d)", m.doneCount, m.total)
	}
	if m.resumeMode == core.ResumeYolo {
		return "  ↑/↓ move · enter YOLO · n normal · y YOLO · c/C copy normal/YOLO · q quit"
	}
	return "  ↑/↓ move · enter normal · y YOLO · c/C copy normal/YOLO · q quit"
}

func agentColor(agent string) lipgloss.Color {
	if c, ok := agentColors[agent]; ok {
		return c
	}
	return chatops.ColTextSubtle
}

// memberCount returns how many independent discovery members provider has,
// matching DiscoverMatchingStream's granularity: a MultiProvider fans out
// to one member per entry in Providers (zero for an empty MultiProvider,
// e.g. when every agent is disabled); any other provider is a single
// member. Used to size the picker's "loading agents… (done/total)" footer.
func memberCount(p core.SessionProvider) int {
	if mp, ok := p.(core.MultiProvider); ok {
		return len(mp.Providers)
	}
	return 1
}

// runPicker opens the interactive picker immediately and streams discovery
// results into it via core.DiscoverMatchingStream as they arrive, instead
// of blocking until every agent has finished. Each incoming batch is
// filtered to dir's variants (filterBatch — the same per-session rule
// FilterByDir applies) and given per-session titles (titleFor) before being
// delivered to the running program as a sessionBatchMsg; a final
// streamDoneMsg follows once every member is done.
//
// The discovery goroutine is never joined here: once the picker exits,
// runPicker returns immediately regardless of whether background discovery
// is still running (Program.Send is documented safe to call, and a no-op,
// once the program has exited — see bubbletea's Program.Send/Quit docs).
// This is deliberate: Run must not block the user's chosen action (open/
// copy) on unfinished discovery.
//
// streamComplete reports whether the discovery stream's done message had
// already arrived by the time the picker exited. Run uses it to decide
// whether saving provider caches is safe — see Run's comment on cache-save
// timing on the picker path.
func runPicker(provider core.SessionProvider, match func(string) bool, dir, dirLabel string, names *core.SessionNames, mode core.ResumeMode) (chosen *model.Session, action pickerAction, resumeMode core.ResumeMode, streamComplete bool, err error) {
	// dir was already validated by Run (os.Stat + this same targetVariants
	// call both succeeded there) before runPicker is ever reached, so this
	// error is not expected in practice; degrade to "nothing matches"
	// rather than propagate a surprising failure out of the picker.
	variants, _ := targetVariants(dir)

	m := pickerModel{dirLabel: dirLabel, now: time.Now(), loading: true, total: memberCount(provider), resumeMode: mode}
	p := tea.NewProgram(m, tea.WithInput(os.Stdin), tea.WithOutput(os.Stderr))

	go func() {
		emit := func(batch []*model.Session) {
			filtered := filterBatch(batch, variants)
			titles := make([]string, len(filtered))
			for i, s := range filtered {
				titles[i] = titleFor(s, names.Get(s.SessionID))
			}
			p.Send(sessionBatchMsg{sessions: filtered, titles: titles})
		}
		done := core.DiscoverMatchingStream(provider, match, emit)
		<-done
		p.Send(streamDoneMsg{})
	}()

	final, runErr := p.Run()
	if runErr != nil {
		return nil, actionQuit, core.ResumeNormal, false, fmt.Errorf("session picker: %w", runErr)
	}
	res := final.(pickerModel)
	streamComplete = !res.loading
	if res.action == actionQuit || res.action == actionEmpty {
		return nil, res.action, res.resumeMode, streamComplete, nil
	}
	// res.chosen, not res.sessions[res.cursor]: the session was captured at
	// the moment of the decision (see pickerModel.chosen's doc comment), so
	// it's correct even if a batch that was already in flight got merged in
	// afterward (applyBatch is a no-op post-decision, but chosen is what
	// actually protects the returned session's identity).
	return res.chosen, res.action, res.resumeMode, streamComplete, nil
}

// relTime renders t relative to now, degrading to an absolute date after
// 30 days. Zero times render as "unknown".
func relTime(t, now time.Time) string {
	if t.IsZero() {
		return "unknown"
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

// titleFor picks the row title: user alias, then agent-provided name, then
// the earliest user message still in the RecentMessages window.
func titleFor(s *model.Session, alias string) string {
	if alias != "" {
		return alias
	}
	if s.Name != "" {
		return s.Name
	}
	for _, msg := range s.RecentMessages {
		if msg.Role == "user" && strings.TrimSpace(msg.Text) != "" {
			return truncate(msg.Text, 60)
		}
	}
	return "(no messages)"
}

// truncate collapses whitespace and cuts s to max runes with an ellipsis.
func truncate(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
