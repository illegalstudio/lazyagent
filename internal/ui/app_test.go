package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/model"
)

type testProvider struct {
	sessions []*model.Session
}

func (p testProvider) DiscoverSessions() ([]*model.Session, error) {
	return p.sessions, nil
}
func (p testProvider) UseWatcher() bool               { return false }
func (p testProvider) RefreshInterval() time.Duration { return 0 }
func (p testProvider) WatchDirs() []string            { return nil }

func testModel(t *testing.T, sessions ...*model.Session) Model {
	t.Helper()
	return testModelWithProvider(t, testProvider{sessions: sessions})
}

func testModelWithProvider(t *testing.T, provider core.SessionProvider) Model {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	theme := DarkTheme()
	manager := core.NewSessionManager(60, provider)
	if err := manager.Reload(); err != nil {
		t.Fatalf("Reload failed: %v", err)
	}
	return Model{
		theme:     theme,
		sty:       newStyles(theme),
		actColors: activityColorMap(theme),
		manager:   manager,
		visible:   manager.VisibleSessions(),
	}
}

func TestRenderListRow_UsesCellWidthForChineseName(t *testing.T) {
	session := &model.Session{
		SessionID:    "s1",
		Name:         "项目管理控制台",
		LastActivity: time.Now(),
	}
	m := testModel(t, session)

	const nameW = 8
	row := m.renderListRow(session, nameW, 0, false)
	if width := lipgloss.Width(row); width != nameW+statusColW {
		t.Fatalf("renderListRow width = %d, want %d (%q)", width, nameW+statusColW, row)
	}
	if !strings.Contains(row, "…") {
		t.Fatalf("renderListRow should truncate with ellipsis, got %q", row)
	}
}

func TestBuildDetailLines_TruncatesChineseConversationByCells(t *testing.T) {
	session := &model.Session{
		SessionID:    "s1",
		CWD:          "/tmp/project",
		LastActivity: time.Now(),
		RecentMessages: []model.ConversationMessage{
			{Role: "user", Text: "你好世界你好世界你好世界", Timestamp: time.Now()},
		},
	}
	m := testModel(t, session)

	lines := m.buildDetailLines(session, 20)
	var conversationLine string
	for _, line := range lines {
		if strings.Contains(line, "User") {
			conversationLine = line
			break
		}
	}
	if conversationLine == "" {
		t.Fatal("conversation line not found")
	}
	if !utf8.ValidString(conversationLine) {
		t.Fatalf("conversation line is not valid UTF-8: %q", conversationLine)
	}
	if width := lipgloss.Width(conversationLine); width > 20 {
		t.Fatalf("conversation line width = %d, want <= 20 (%q)", width, conversationLine)
	}
}

func TestBuildDetailLinesIncludesYoloResume(t *testing.T) {
	session := &model.Session{
		Agent: "codex", SessionID: "abc", CWD: "/tmp/project", LastActivity: time.Now(),
	}
	m := testModel(t, session)
	lines := strings.Join(m.buildDetailLines(session, 120), "\n")
	if !strings.Contains(lines, "codex resume abc") {
		t.Fatal("normal resume command missing from details")
	}
	if !strings.Contains(lines, "codex --yolo resume abc") {
		t.Fatal("YOLO resume command missing from details")
	}
}

func TestSearchBackspaceDeletesWholeChineseRune(t *testing.T) {
	m := testModel(t)
	m.searchMode = true
	m.searchQuery = "中文"

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	got := updated.(Model).searchQuery
	if got != "中" {
		t.Fatalf("searchQuery = %q, want %q", got, "中")
	}
}

func TestCloudRowDetailsAndActions(t *testing.T) {
	s := &model.Session{SessionID: "cloud:cursor:one", Agent: "cursor", Name: "Remote run", LastActivity: time.Now(), Cloud: &model.CloudSession{ID: "one", URL: "https://cursor.com/agents/one", Repo: "https://github.com/org/project", Status: "working"}}
	m := testModel(t, s)
	defer m.manager.Close()
	if row := m.renderListRow(s, 40, 0, false); !strings.Contains(row, "C cloud") {
		t.Fatalf("missing cloud badge: %q", row)
	}
	detail := strings.Join(m.buildDetailLines(s, 120), "\n")
	for _, want := range []string{"Cursor cloud", "https://github.com/org/project", "https://cursor.com/agents/one", "o open in Cursor"} {
		if !strings.Contains(detail, want) {
			t.Errorf("missing %q in cloud details", want)
		}
	}
	for _, localOnly := range []string{"cursor-agent resume", "Tokens", "Conversation"} {
		if strings.Contains(detail, localOnly) {
			t.Errorf("local data leaked into cloud details: %s", localOnly)
		}
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	if cmd == nil || updated.(Model).editorPicker {
		t.Fatal("cloud open must return a browser command without editor picker")
	}
	updated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	if cmd != nil || !strings.Contains(updated.(Model).flashMsg, "open this agent") {
		t.Fatal("cloud YOLO action must be disabled")
	}
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if !updated.(Model).manager.CloudStatus().ShowIdle {
		t.Fatal("a must toggle idle cloud history")
	}
	invalid := openCloudCmd("javascript:alert(1)")().(cloudActionMsg)
	if invalid.err == nil {
		t.Fatal("browser command accepted unsafe URL")
	}
}

func TestUserFiltersKeepChosenRowAcrossRenderTicks(t *testing.T) {
	for _, tc := range []struct {
		name   string
		search bool
		query  string
		key    tea.KeyMsg
	}{
		{"filter", false, "", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}},
		{"search typing", true, "", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}},
		{"search backspace", true, "s", tea.KeyMsg{Type: tea.KeyBackspace}},
		{"search escape", true, "s", tea.KeyMsg{Type: tea.KeyEsc}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			m := testModel(t,
				&model.Session{SessionID: "first", Name: "shared first", LastActivity: now},
				&model.Session{SessionID: "second", Name: "shared second", LastActivity: now.Add(-time.Second)},
			)
			defer m.manager.Close()
			m.cursor, m.selectedID = 1, "second"
			m.searchMode, m.searchQuery = tc.search, tc.query
			updated, _ := m.Update(tc.key)
			m = updated.(Model)
			if len(m.visible) != 2 || m.cursor != 0 || m.selectedID != "first" {
				t.Fatalf("filter did not remember its chosen row: cursor %d, selected %q", m.cursor, m.selectedID)
			}
			updated, _ = m.Update(renderTickMsg(now))
			m = updated.(Model)
			if m.cursor != 0 || m.selectedID != "first" || m.visible[m.cursor].SessionID != "first" {
				t.Fatalf("render tick restored stale selection: cursor %d, selected %q", m.cursor, m.selectedID)
			}
		})
	}
}

func TestWindowAdjustmentPreservesSelectionWhenSessionsArrive(t *testing.T) {
	for _, key := range []string{"+", "-"} {
		t.Run(key, func(t *testing.T) {
			now := time.Now()
			provider := &testProvider{sessions: []*model.Session{
				{SessionID: "first", LastActivity: now},
				{SessionID: "selected", LastActivity: now.Add(-time.Second)},
			}}
			m := testModelWithProvider(t, provider)
			defer m.manager.Close()
			m.cursor, m.selectedID = 1, "selected"
			// Discovery has a new row before the UI samples the next snapshot.
			provider.sessions = append(provider.sessions, &model.Session{SessionID: "new", LastActivity: now.Add(time.Second)})
			if err := m.manager.Reload(); err != nil {
				t.Fatal(err)
			}
			updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
			m = updated.(Model)
			if m.selectedID != "selected" || m.cursor != 2 || m.visible[m.cursor].SessionID != "selected" {
				t.Fatalf("window adjustment changed the selected session: cursor %d, id %q", m.cursor, m.selectedID)
			}
			updated, _ = m.Update(renderTickMsg(now))
			m = updated.(Model)
			if m.selectedID != "selected" || m.visible[m.cursor].SessionID != "selected" {
				t.Fatal("render tick changed selection after window adjustment")
			}
		})
	}
}
