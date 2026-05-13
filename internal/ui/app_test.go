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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	theme := DarkTheme()
	manager := core.NewSessionManager(60, testProvider{sessions: sessions})
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
