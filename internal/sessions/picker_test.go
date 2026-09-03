package sessions

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func TestPickerNavigationAndOpen(t *testing.T) {
	m := pickerModel{
		sessions: []*model.Session{
			{Agent: "claude", SessionID: "a"},
			{Agent: "unknown", SessionID: "b"},
		},
		titles: []string{"one", "two"},
	}

	next, _ := m.Update(keyRune('j'))
	m = next.(pickerModel)
	if m.cursor != 1 {
		t.Fatalf("cursor = %d, want 1", m.cursor)
	}
	// j at the bottom stays put.
	next, _ = m.Update(keyRune('j'))
	m = next.(pickerModel)
	if m.cursor != 1 {
		t.Fatalf("cursor moved past end: %d", m.cursor)
	}

	// Enter on an agent without resume support: stay open and show a status message.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("picker must stay open on a row without resume support")
	}
	if m.status == "" {
		t.Fatal("expected a status message for an unsupported agent")
	}

	// back up to claude and open.
	next, _ = m.Update(keyRune('k'))
	m = next.(pickerModel)
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionOpen {
		t.Fatalf("action = %v, want actionOpen", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after open")
	}
}

func TestPickerEnterOpensOpenCode(t *testing.T) {
	m := pickerModel{sessions: []*model.Session{{Agent: "opencode", SessionID: "x"}}, titles: []string{"t"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionOpen {
		t.Fatalf("action = %v, want actionOpen", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after open")
	}
}

func TestPickerYOpensYolo(t *testing.T) {
	m := pickerModel{sessions: []*model.Session{{Agent: "claude", SessionID: "x"}}, titles: []string{"t"}}
	next, cmd := m.Update(keyRune('y'))
	m = next.(pickerModel)
	if m.action != actionOpen || m.resumeMode != core.ResumeYolo {
		t.Fatalf("action = %v, mode = %v, want YOLO open", m.action, m.resumeMode)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after YOLO open")
	}
}

func TestPickerExplicitNormalOverridesYoloDefault(t *testing.T) {
	m := pickerModel{
		sessions:   []*model.Session{{Agent: "claude", SessionID: "x"}},
		titles:     []string{"t"},
		resumeMode: core.ResumeYolo,
	}
	next, cmd := m.Update(keyRune('n'))
	m = next.(pickerModel)
	if m.action != actionOpen || m.resumeMode != core.ResumeNormal {
		t.Fatalf("action = %v, mode = %v, want normal open", m.action, m.resumeMode)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after normal open")
	}
}

func TestPickerEnterUsesYoloDefault(t *testing.T) {
	m := pickerModel{
		sessions:   []*model.Session{{Agent: "claude", SessionID: "x"}},
		titles:     []string{"t"},
		resumeMode: core.ResumeYolo,
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionOpen || m.resumeMode != core.ResumeYolo {
		t.Fatalf("action = %v, mode = %v, want YOLO open", m.action, m.resumeMode)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after YOLO open")
	}
}

func TestPickerCopyKeysIgnoreDefaultMode(t *testing.T) {
	m := pickerModel{
		sessions:   []*model.Session{{Agent: "claude", SessionID: "x"}},
		titles:     []string{"t"},
		resumeMode: core.ResumeYolo,
	}
	next, _ := m.Update(keyRune('c'))
	m = next.(pickerModel)
	if m.action != actionCopy || m.resumeMode != core.ResumeNormal {
		t.Fatalf("c selected action = %v, mode = %v, want normal copy", m.action, m.resumeMode)
	}
}

func TestPickerYoloUnavailableForPi(t *testing.T) {
	m := pickerModel{sessions: []*model.Session{{Agent: "pi", SessionID: "x"}}, titles: []string{"t"}}
	next, cmd := m.Update(keyRune('y'))
	m = next.(pickerModel)
	if cmd != nil || m.action != actionQuit {
		t.Fatalf("YOLO pi selection should stay open, action = %v, cmd = %v", m.action, cmd)
	}
	if !strings.Contains(m.status, "no YOLO mode") {
		t.Fatalf("status = %q, want unavailable YOLO message", m.status)
	}
}

func TestPickerQuitKeys(t *testing.T) {
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEsc}, {Type: tea.KeyCtrlC}, keyRune('q')} {
		m := pickerModel{sessions: []*model.Session{{Agent: "claude", SessionID: "a"}}, titles: []string{"t"}}
		next, cmd := m.Update(k)
		m = next.(pickerModel)
		if m.action != actionQuit || cmd == nil {
			t.Fatalf("key %v: action=%v cmd=%v, want quit", k, m.action, cmd)
		}
	}
}

func TestRelTime(t *testing.T) {
	now := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-5 * time.Minute), "5m ago"},
		{now.Add(-2 * time.Hour), "2h ago"},
		{now.Add(-30 * time.Hour), "yesterday"},
		{now.Add(-3 * 24 * time.Hour), "3d ago"},
		{now.Add(-40 * 24 * time.Hour), "2026-06-10"},
		{time.Time{}, "unknown"},
	}
	for _, c := range cases {
		if got := relTime(c.in, now); got != c.want {
			t.Errorf("relTime(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTitleFor(t *testing.T) {
	s := &model.Session{
		Name: "agent-name",
		RecentMessages: []model.ConversationMessage{
			{Role: "assistant", Text: "hi"},
			{Role: "user", Text: "fix the build\nplease"},
		},
	}
	if got := titleFor(s, "custom"); got != "custom" {
		t.Errorf("alias must win, got %q", got)
	}
	if got := titleFor(s, ""); got != "agent-name" {
		t.Errorf("agent name second, got %q", got)
	}
	s.Name = ""
	if got := titleFor(s, ""); got != "fix the build please" {
		t.Errorf("user message preview third, got %q", got)
	}
	if got := titleFor(&model.Session{}, ""); got != "(no messages)" {
		t.Errorf("fallback, got %q", got)
	}
}

func TestPickerCKeyOnGrokCopiesAndQuits(t *testing.T) {
	m := pickerModel{sessions: []*model.Session{{Agent: "grok", SessionID: "g"}}, titles: []string{"t"}}
	next, cmd := m.Update(keyRune('c'))
	m = next.(pickerModel)
	if m.action != actionCopy {
		t.Fatalf("action = %v, want actionCopy", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after copy")
	}
}

func TestPickerEnterOnGrokOpensAndQuits(t *testing.T) {
	m := pickerModel{sessions: []*model.Session{{Agent: "grok", SessionID: "g"}}, titles: []string{"t"}}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionOpen {
		t.Fatalf("action = %v, want actionOpen", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after open")
	}
}

func TestPickerCKeyOnCopyableSessionCopiesAndQuits(t *testing.T) {
	// Copy remains available independently of direct execution support.
	m := pickerModel{sessions: []*model.Session{{Agent: "opencode", SessionID: "o"}}, titles: []string{"t"}}
	next, cmd := m.Update(keyRune('c'))
	m = next.(pickerModel)
	if m.action != actionCopy {
		t.Fatalf("action = %v, want actionCopy", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after copy")
	}
}

func TestPickerArrowAliasNavigation(t *testing.T) {
	m := pickerModel{
		sessions: []*model.Session{
			{Agent: "claude", SessionID: "a"},
			{Agent: "claude", SessionID: "b"},
		},
		titles: []string{"one", "two"},
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(pickerModel)
	if m.cursor != 1 {
		t.Fatalf("down arrow: cursor = %d, want 1", m.cursor)
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(pickerModel)
	if m.cursor != 0 {
		t.Fatalf("up arrow: cursor = %d, want 0", m.cursor)
	}
}

func TestVisibleRangeWithinHeightReturnsFullRange(t *testing.T) {
	start, end := visibleRange(2, 5, 20)
	if start != 0 || end != 5 {
		t.Errorf("visibleRange(2,5,20) = (%d,%d), want (0,5)", start, end)
	}
}

func TestVisibleRangeAtTop(t *testing.T) {
	start, end := visibleRange(0, 100, 10)
	if start != 0 || end != 10 {
		t.Errorf("visibleRange(0,100,10) = (%d,%d), want (0,10)", start, end)
	}
}

func TestVisibleRangeAtBottom(t *testing.T) {
	start, end := visibleRange(99, 100, 10)
	if start != 90 || end != 100 {
		t.Errorf("visibleRange(99,100,10) = (%d,%d), want (90,100)", start, end)
	}
}

func TestVisibleRangeInMiddleIsCentered(t *testing.T) {
	start, end := visibleRange(50, 100, 10)
	if start != 45 || end != 55 {
		t.Errorf("visibleRange(50,100,10) = (%d,%d), want (45,55)", start, end)
	}
}

func TestVisibleRangeCursorAlwaysWithinWindow(t *testing.T) {
	total := 100
	height := 7
	for cursor := 0; cursor < total; cursor++ {
		start, end := visibleRange(cursor, total, height)
		if cursor < start || cursor >= end {
			t.Fatalf("cursor %d out of window [%d,%d)", cursor, start, end)
		}
		if end-start > height {
			t.Fatalf("window [%d,%d) exceeds height %d", start, end, height)
		}
	}
}

func TestPickerWindowFollowsCursorAfterWindowSizeMsg(t *testing.T) {
	sessions := make([]*model.Session, 30)
	titles := make([]string, 30)
	for i := range sessions {
		sessions[i] = &model.Session{Agent: "claude", SessionID: fmt.Sprintf("s%d", i)}
		titles[i] = fmt.Sprintf("session %d", i)
	}
	m := pickerModel{sessions: sessions, titles: titles}

	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 15})
	m = next.(pickerModel)
	if m.height != 15 {
		t.Fatalf("height = %d, want 15", m.height)
	}

	// Cursor starts at 0: the window must start at 0.
	budget := m.rowBudget()
	start, end := visibleRange(m.cursor, len(m.sessions), budget)
	if start != 0 {
		t.Fatalf("initial window start = %d, want 0", start)
	}
	if m.cursor < start || m.cursor >= end {
		t.Fatalf("cursor %d not within initial window [%d,%d)", m.cursor, start, end)
	}

	// Drive the cursor to the bottom; the window must follow and reach the end.
	for i := 0; i < len(sessions)-1; i++ {
		next, _ = m.Update(keyRune('j'))
		m = next.(pickerModel)
	}
	if m.cursor != len(sessions)-1 {
		t.Fatalf("cursor = %d, want %d", m.cursor, len(sessions)-1)
	}
	budget = m.rowBudget()
	start, end = visibleRange(m.cursor, len(m.sessions), budget)
	if m.cursor < start || m.cursor >= end {
		t.Fatalf("cursor %d not within window [%d,%d) at bottom", m.cursor, start, end)
	}
	if end != len(sessions) {
		t.Fatalf("window should reach the end when cursor is at the bottom, got end=%d", end)
	}
}

func TestPickerRowBudgetFallsBackWhenHeightUnknown(t *testing.T) {
	m := pickerModel{}
	if got := m.rowBudget(); got != defaultVisibleRows {
		t.Errorf("rowBudget() with unknown height = %d, want %d", got, defaultVisibleRows)
	}
}

// --- mergeSessions (pure) ---

func TestMergeSessionsSortsOutOfOrderBatches(t *testing.T) {
	now := time.Now()
	sessions := []*model.Session{{SessionID: "a", LastActivity: now.Add(-time.Hour)}}
	titles := []string{"A"}

	// Incoming batch is MORE recent than what's already merged -- out-of-
	// order arrival relative to final sort order.
	batchSessions := []*model.Session{{SessionID: "b", LastActivity: now}}
	batchTitles := []string{"B"}

	gotSessions, gotTitles := mergeSessions(sessions, titles, batchSessions, batchTitles)
	if len(gotSessions) != 2 {
		t.Fatalf("len = %d, want 2", len(gotSessions))
	}
	if gotSessions[0].SessionID != "b" || gotSessions[1].SessionID != "a" {
		t.Fatalf("sessions = [%s, %s], want [b, a] (most recent first)", gotSessions[0].SessionID, gotSessions[1].SessionID)
	}
	if gotTitles[0] != "B" || gotTitles[1] != "A" {
		t.Fatalf("titles = %v, want [B, A] (must stay zipped with sessions after sort)", gotTitles)
	}
}

func TestMergeSessionsKeepsTitlesZippedWithSessions(t *testing.T) {
	now := time.Now()
	sessions := []*model.Session{
		{SessionID: "mid", LastActivity: now.Add(-time.Hour)},
	}
	titles := []string{"mid-title"}
	batchSessions := []*model.Session{
		{SessionID: "newest", LastActivity: now},
		{SessionID: "oldest", LastActivity: now.Add(-2 * time.Hour)},
	}
	batchTitles := []string{"newest-title", "oldest-title"}

	gotSessions, gotTitles := mergeSessions(sessions, titles, batchSessions, batchTitles)
	wantOrder := []string{"newest", "mid", "oldest"}
	for i, id := range wantOrder {
		if gotSessions[i].SessionID != id {
			t.Fatalf("sessions[%d] = %s, want %s", i, gotSessions[i].SessionID, id)
		}
		if gotTitles[i] != id+"-title" {
			t.Fatalf("titles[%d] = %s, want %s", i, gotTitles[i], id+"-title")
		}
	}
}

// --- relocateCursor (pure) ---

func TestRelocateCursorFollowsSelectedSession(t *testing.T) {
	prev := []*model.Session{{SessionID: "x"}, {SessionID: "y"}}
	// User had navigated down to "y" (cursor 1). A merge reorders the list
	// so "y" is now first.
	merged := []*model.Session{{SessionID: "y"}, {SessionID: "z"}, {SessionID: "x"}}
	if got := relocateCursor(prev, 1, merged); got != 0 {
		t.Errorf("relocateCursor = %d, want 0 (cursor must follow the selected session)", got)
	}
}

func TestRelocateCursorStaysAtTopWhenNotNavigated(t *testing.T) {
	prev := []*model.Session{{SessionID: "x"}, {SessionID: "y"}}
	// cursor is still 0 (user hasn't navigated): the merge reorders the
	// list, but the cursor must stay pinned to the top regardless of
	// identity.
	merged := []*model.Session{{SessionID: "y"}, {SessionID: "x"}, {SessionID: "z"}}
	if got := relocateCursor(prev, 0, merged); got != 0 {
		t.Errorf("relocateCursor = %d, want 0 (must stay at top when not navigated)", got)
	}
}

func TestRelocateCursorClampsWhenSelectedSessionDisappears(t *testing.T) {
	prev := []*model.Session{{SessionID: "x"}, {SessionID: "y"}}
	// "y" (cursor 1) is no longer present in merged.
	merged := []*model.Session{{SessionID: "x"}}
	if got := relocateCursor(prev, 1, merged); got != 0 {
		t.Errorf("relocateCursor = %d, want 0 (clamp to the last valid index)", got)
	}
}

func TestRelocateCursorOnEmptyMergedIsZero(t *testing.T) {
	prev := []*model.Session{{SessionID: "x"}}
	if got := relocateCursor(prev, 0, nil); got != 0 {
		t.Errorf("relocateCursor = %d, want 0", got)
	}
}

// --- Update-driven stream transitions ---

func TestPickerBatchMsgAppendsAndResorts(t *testing.T) {
	now := time.Now()
	m := pickerModel{loading: true, total: 2}

	next, cmd := m.Update(sessionBatchMsg{
		sessions: []*model.Session{{SessionID: "a", LastActivity: now.Add(-time.Hour)}},
		titles:   []string{"A"},
	})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("a batch message must not quit the program")
	}
	if len(m.sessions) != 1 || m.sessions[0].SessionID != "a" {
		t.Fatalf("sessions = %#v, want [a]", m.sessions)
	}
	if m.doneCount != 1 {
		t.Fatalf("doneCount = %d, want 1", m.doneCount)
	}
	if !m.loading {
		t.Fatal("model must still be loading before a streamDoneMsg arrives")
	}

	// A second, more recent batch: the merged list must end up sorted even
	// though it arrived after the older one.
	next, _ = m.Update(sessionBatchMsg{
		sessions: []*model.Session{{SessionID: "b", LastActivity: now}},
		titles:   []string{"B"},
	})
	m = next.(pickerModel)
	if len(m.sessions) != 2 || m.sessions[0].SessionID != "b" || m.sessions[1].SessionID != "a" {
		t.Fatalf("sessions = %#v, want [b, a]", m.sessions)
	}
	if m.doneCount != 2 {
		t.Fatalf("doneCount = %d, want 2", m.doneCount)
	}
}

func TestPickerBatchMsgDoneCountNeverExceedsTotal(t *testing.T) {
	m := pickerModel{loading: true, total: 1}
	next, _ := m.Update(sessionBatchMsg{sessions: []*model.Session{{SessionID: "a"}}, titles: []string{"A"}})
	m = next.(pickerModel)
	next, _ = m.Update(sessionBatchMsg{sessions: []*model.Session{{SessionID: "b"}}, titles: []string{"B"}})
	m = next.(pickerModel)
	if m.doneCount != 1 {
		t.Fatalf("doneCount = %d, want 1 (bounded by total)", m.doneCount)
	}
}

func TestPickerStreamDoneWithZeroSessionsQuitsWithEmptyAction(t *testing.T) {
	m := pickerModel{loading: true, total: 1}
	next, cmd := m.Update(streamDoneMsg{})
	m = next.(pickerModel)
	if m.action != actionEmpty {
		t.Fatalf("action = %v, want actionEmpty", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit when the stream finishes with zero sessions")
	}
	if m.loading {
		t.Fatal("loading must be false once the stream is done")
	}
}

func TestPickerStreamDoneSwitchesFooterWhenNonEmpty(t *testing.T) {
	m := pickerModel{
		loading:  true,
		total:    1,
		sessions: []*model.Session{{Agent: "claude", SessionID: "a"}},
		titles:   []string{"one"},
	}
	next, cmd := m.Update(streamDoneMsg{})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("a non-empty completion must not quit the program")
	}
	if m.loading {
		t.Fatal("loading must be false once the stream is done")
	}
	if m.action == actionEmpty {
		t.Fatal("action must not be actionEmpty when sessions were found")
	}
	if strings.Contains(m.View(), "loading agents") {
		t.Error("footer must switch away from the loading text once done")
	}
}

func TestPickerLoadingFooterShowsProgress(t *testing.T) {
	m := pickerModel{loading: true, total: 3, doneCount: 1}
	view := m.View()
	if !strings.Contains(view, "loading agents… (1/3)") {
		t.Errorf("View() = %q, want it to contain the loading progress footer", view)
	}
}

func TestPickerEnterDuringLoadingActsOnCurrentRow(t *testing.T) {
	m := pickerModel{
		loading:  true,
		total:    2,
		sessions: []*model.Session{{Agent: "claude", SessionID: "a"}},
		titles:   []string{"one"},
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if m.action != actionOpen {
		t.Fatalf("action = %v, want actionOpen", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit after opening a row while still loading")
	}
}

func TestPickerQuitDuringLoadingWorks(t *testing.T) {
	m := pickerModel{loading: true, total: 2}
	next, cmd := m.Update(keyRune('q'))
	m = next.(pickerModel)
	if m.action != actionQuit {
		t.Fatalf("action = %v, want actionQuit (explicit quit, not actionEmpty)", m.action)
	}
	if cmd == nil {
		t.Fatal("expected tea.Quit")
	}
}

func TestPickerEnterOnEmptyListDuringLoadingIsNoop(t *testing.T) {
	m := pickerModel{loading: true, total: 2}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("enter on an empty (still loading) list must not quit")
	}
	if m.action != actionQuit {
		t.Fatalf("action = %v, want the zero value actionQuit (no-op)", m.action)
	}
}

func TestPickerCKeyOnEmptyListDuringLoadingIsNoop(t *testing.T) {
	m := pickerModel{loading: true, total: 2}
	next, cmd := m.Update(keyRune('c'))
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("c on an empty (still loading) list must not quit")
	}
	if m.action != actionQuit {
		t.Fatalf("action = %v, want the zero value actionQuit (no-op)", m.action)
	}
}

// --- decisive-key vs concurrent-batch race ---
//
// A batch can be in flight on p.msgs at the exact moment the user presses
// enter/c: both a sessionBatchMsg and the QuitMsg produced by tea.Quit
// travel through the same channel, and Update processes messages strictly
// in receive order, so a batch that was sent before the key but read after
// it (or vice versa) is a legitimate interleaving, not a bug in bubbletea.
// If a late batch re-sorts the list and relocateCursor pins cursor 0 to a
// brand new top row, re-deriving the chosen session from sessions[cursor]
// after the fact returns a DIFFERENT session than the one the user actually
// acted on. These tests drive Update with the exact ordering: a batch
// lands, the user decides on the row it's now looking at, then a second,
// newer batch that would displace that row from the top arrives. The
// decision must survive untouched.

func TestPickerEnterFreezesSelectionAgainstLaterBatch(t *testing.T) {
	now := time.Now()
	m := pickerModel{loading: true, total: 2}

	// Batch A lands: session "a" is the only (so top) row.
	next, _ := m.Update(sessionBatchMsg{
		sessions: []*model.Session{{Agent: "claude", SessionID: "a", LastActivity: now.Add(-time.Hour)}},
		titles:   []string{"A"},
	})
	m = next.(pickerModel)

	// The user decides on the row they're looking at (session "a", cursor 0).
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(pickerModel)
	if cmd == nil {
		t.Fatal("expected tea.Quit after opening")
	}
	if m.action != actionOpen {
		t.Fatalf("action = %v, want actionOpen", m.action)
	}
	if m.chosen == nil || m.chosen.SessionID != "a" {
		t.Fatalf("chosen = %v, want session a", m.chosen)
	}

	// Batch B "arrives late": a NEWER session that would sort ahead of "a",
	// displacing it from row 0.
	next, cmd = m.Update(sessionBatchMsg{
		sessions: []*model.Session{{Agent: "codex", SessionID: "b", LastActivity: now}},
		titles:   []string{"B"},
	})
	m = next.(pickerModel)

	if cmd != nil {
		t.Fatal("a batch arriving after a terminal decision must not produce a command")
	}
	if m.chosen == nil || m.chosen.SessionID != "a" {
		t.Fatalf("chosen after the late batch = %v, want still session a", m.chosen)
	}
	if len(m.sessions) != 1 || m.sessions[0].SessionID != "a" {
		t.Fatalf("sessions must not be re-merged after a terminal decision, got %#v", m.sessions)
	}
}

func TestPickerCKeyFreezesSelectionAgainstLaterBatch(t *testing.T) {
	now := time.Now()
	m := pickerModel{loading: true, total: 2}

	// Batch A: an opencode session is the only, so top, row.
	next, _ := m.Update(sessionBatchMsg{
		sessions: []*model.Session{{Agent: "opencode", SessionID: "a", LastActivity: now.Add(-time.Hour)}},
		titles:   []string{"A"},
	})
	m = next.(pickerModel)

	next, cmd := m.Update(keyRune('c'))
	m = next.(pickerModel)
	if cmd == nil {
		t.Fatal("expected tea.Quit after copy")
	}
	if m.action != actionCopy {
		t.Fatalf("action = %v, want actionCopy", m.action)
	}
	if m.chosen == nil || m.chosen.SessionID != "a" {
		t.Fatalf("chosen = %v, want session a", m.chosen)
	}

	next, cmd = m.Update(sessionBatchMsg{
		sessions: []*model.Session{{Agent: "codex", SessionID: "b", LastActivity: now}},
		titles:   []string{"B"},
	})
	m = next.(pickerModel)

	if cmd != nil {
		t.Fatal("a batch arriving after a terminal decision must not produce a command")
	}
	if m.chosen == nil || m.chosen.SessionID != "a" {
		t.Fatalf("chosen after the late batch = %v, want still session a", m.chosen)
	}
}

func TestPickerQuitFreezesAgainstLaterBatch(t *testing.T) {
	m := pickerModel{loading: true, total: 2}
	next, cmd := m.Update(keyRune('q'))
	m = next.(pickerModel)
	if cmd == nil || m.action != actionQuit {
		t.Fatalf("expected actionQuit + tea.Quit, got action=%v cmd=%v", m.action, cmd)
	}

	next, cmd = m.Update(sessionBatchMsg{sessions: []*model.Session{{SessionID: "z"}}, titles: []string{"Z"}})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("a batch arriving after quit must not produce a command")
	}
	if len(m.sessions) != 0 {
		t.Fatalf("sessions must stay empty once quit was decided, got %#v", m.sessions)
	}
}

func TestPickerStreamDoneFreezesAfterDecision(t *testing.T) {
	m := pickerModel{loading: true, total: 1}
	next, cmd := m.Update(keyRune('q'))
	m = next.(pickerModel)
	if cmd == nil {
		t.Fatal("expected tea.Quit")
	}

	next, cmd = m.Update(streamDoneMsg{})
	m = next.(pickerModel)
	if cmd != nil {
		t.Fatal("streamDoneMsg arriving after a decision must not produce a command")
	}
	if m.action != actionQuit {
		t.Fatalf("action must stay actionQuit, got %v (must not flip to actionEmpty)", m.action)
	}
}

// --- memberCount (pure) ---

// noopProvider is a minimal core.SessionProvider that does nothing; used
// only to stand in as a "single, non-multi" provider for memberCount.
type noopProvider struct{}

func (noopProvider) DiscoverSessions() ([]*model.Session, error) { return nil, nil }
func (noopProvider) UseWatcher() bool                            { return false }
func (noopProvider) RefreshInterval() time.Duration              { return 0 }
func (noopProvider) WatchDirs() []string                         { return nil }

func TestMemberCountSingleNonMultiProviderIsOne(t *testing.T) {
	if got := memberCount(noopProvider{}); got != 1 {
		t.Errorf("memberCount(noopProvider{}) = %d, want 1", got)
	}
}

func TestMemberCountMultiProviderIsProviderCount(t *testing.T) {
	mp := core.MultiProvider{Providers: []core.SessionProvider{noopProvider{}, noopProvider{}, noopProvider{}}}
	if got := memberCount(mp); got != 3 {
		t.Errorf("memberCount(3-provider MultiProvider) = %d, want 3", got)
	}
}

func TestMemberCountEmptyMultiProviderIsZero(t *testing.T) {
	if got := memberCount(core.MultiProvider{}); got != 0 {
		t.Errorf("memberCount(empty MultiProvider) = %d, want 0", got)
	}
}
