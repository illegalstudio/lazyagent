//go:build !notray

package tray

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/nahime0/lazyagent/internal/model"
	"github.com/nahime0/lazyagent/internal/core"
	"github.com/nahime0/lazyagent/internal/demo"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SessionService is the Go service exposed to the Svelte frontend via Wails bindings.
type SessionService struct {
	manager  *core.SessionManager
	app      *application.App
	ctx      context.Context
	demoMode bool
	provider core.SessionProvider // if set, used instead of demoMode logic
}

// ServiceStartup is called by Wails when the app starts.
func (s *SessionService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.ctx = ctx
	cfg := core.LoadConfig()
	provider := s.provider
	if provider == nil {
		if s.demoMode {
			provider = demo.Provider{}
		} else {
			provider = core.NewLiveProvider()
		}
	}
	s.manager = core.NewSessionManager(cfg.WindowMinutes, provider)
	if err := s.manager.StartWatcher(); err != nil {
		return err
	}

	// Initial load
	_ = s.manager.Reload()

	// Background goroutine: watch for file changes + periodic refresh
	go s.watchLoop()

	return nil
}

// ServiceShutdown is called by Wails when the app stops.
func (s *SessionService) ServiceShutdown() error {
	s.manager.StopWatcher()
	return nil
}

func (s *SessionService) watchLoop() {
	events := s.manager.WatcherEvents()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// Only use a periodic reload ticker when no file watcher is available.
	var reloadC <-chan time.Time
	if events == nil {
		reloadTicker := time.NewTicker(30 * time.Second)
		defer reloadTicker.Stop()
		reloadC = reloadTicker.C
	}

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-events:
			_ = s.manager.Reload()
			s.emitUpdate()
		case <-ticker.C:
			if s.manager.UpdateActivities() {
				s.emitUpdate()
			}
		case <-reloadC:
			_ = s.manager.Reload()
			s.emitUpdate()
		}
	}
}

func (s *SessionService) emitUpdate() {
	if s.app != nil {
		s.app.Event.Emit("sessions:updated")
	}
}

// SessionItem is a lightweight session representation for the list view.
type SessionItem struct {
	SessionID     string    `json:"sessionId"`
	Agent         string    `json:"agent"`
	Source        string    `json:"source"`
	CWD           string    `json:"cwd"`
	ShortName     string    `json:"shortName"`
	AgentName     string    `json:"agentName"`
	CustomName    string    `json:"customName"`
	Activity      string    `json:"activity"`
	IsActive      bool      `json:"isActive"`
	Model         string    `json:"model"`
	GitBranch     string    `json:"gitBranch"`
	CostUSD       float64   `json:"costUsd"`
	LastActivity  time.Time `json:"lastActivity"`
	TotalMessages int       `json:"totalMessages"`
	SparklineData []int     `json:"sparklineData"`
}

// SessionFull is the detailed session representation.
type SessionFull struct {
	SessionItem
	Version             string             `json:"version"`
	IsWorktree          bool               `json:"isWorktree"`
	MainRepo            string             `json:"mainRepo"`
	InputTokens         int                `json:"inputTokens"`
	OutputTokens        int                `json:"outputTokens"`
	CacheCreationTokens int                `json:"cacheCreationTokens"`
	CacheReadTokens     int                `json:"cacheReadTokens"`
	UserMessages        int                `json:"userMessages"`
	AssistantMessages   int                `json:"assistantMessages"`
	CurrentTool         string             `json:"currentTool"`
	LastFileWrite       string             `json:"lastFileWrite"`
	LastFileWriteAt     time.Time          `json:"lastFileWriteAt"`
	RecentTools         []ToolItem         `json:"recentTools"`
	RecentMessages      []ConversationItem `json:"recentMessages"`
	DesktopTitle        string             `json:"desktopTitle,omitempty"`
	DesktopID           string             `json:"desktopId,omitempty"`
	PermissionMode      string             `json:"permissionMode,omitempty"`
}

// ToolItem is a tool call for the detail view.
type ToolItem struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
	Ago       string    `json:"ago"`
}

// ConversationItem is a conversation message for the detail view.
type ConversationItem struct {
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

func (s *SessionService) buildSessionItem(sess *model.Session, activity core.ActivityKind, wm int, nameLen int) SessionItem {
	source := "cli"
	if sess.Desktop != nil {
		source = "desktop"
	}
	return SessionItem{
		SessionID:     sess.SessionID,
		Agent:         sess.Agent,
		Source:        source,
		CWD:           sess.CWD,
		ShortName:     core.ShortName(sess.CWD, nameLen),
		AgentName:     sess.Name,
		CustomName:    s.manager.SessionName(sess.SessionID),
		Activity:      string(activity),
		IsActive:      core.IsActiveActivity(activity),
		Model:         sess.Model,
		GitBranch:     sess.GitBranch,
		CostUSD:       core.EffectiveCost(sess.Model, sess.CostUSD, sess.InputTokens, sess.OutputTokens, sess.CacheCreationTokens, sess.CacheReadTokens),
		LastActivity:  sess.LastActivity,
		TotalMessages: sess.TotalMessages,
		SparklineData: core.BucketTimestamps(sess.EntryTimestamps, time.Duration(wm)*time.Minute, 20),
	}
}

// GetSessions returns all visible sessions for the list view.
func (s *SessionService) GetSessions() []SessionItem {
	visible := s.manager.VisibleSessions()
	items := make([]SessionItem, 0, len(visible))
	wm := s.manager.WindowMinutes()
	for _, sess := range visible {
		activity := s.manager.ActivityFor(sess.SessionID)
		items = append(items, s.buildSessionItem(sess, activity, wm, 40))
	}
	return items
}

// GetSessionDetail returns full detail for a session.
func (s *SessionService) GetSessionDetail(id string) *SessionFull {
	detail := s.manager.SessionDetail(id)
	if detail == nil {
		return nil
	}
	sess := &detail.Session
	wm := s.manager.WindowMinutes()

	tools := make([]ToolItem, 0, len(sess.RecentTools))
	for _, t := range sess.RecentTools {
		tools = append(tools, ToolItem{
			Name:      t.Name,
			Timestamp: t.Timestamp,
			Ago:       core.FormatDuration(time.Since(t.Timestamp)),
		})
	}

	msgs := make([]ConversationItem, 0, len(sess.RecentMessages))
	for _, m := range sess.RecentMessages {
		msgs = append(msgs, ConversationItem{
			Role:      m.Role,
			Text:      m.Text,
			Timestamp: m.Timestamp,
		})
	}

	full := &SessionFull{
		SessionItem:         s.buildSessionItem(sess, detail.Activity, wm, 60),
		Version:             sess.Version,
		IsWorktree:          sess.IsWorktree,
		MainRepo:            sess.MainRepo,
		InputTokens:         sess.InputTokens,
		OutputTokens:        sess.OutputTokens,
		CacheCreationTokens: sess.CacheCreationTokens,
		CacheReadTokens:     sess.CacheReadTokens,
		UserMessages:        sess.UserMessages,
		AssistantMessages:   sess.AssistantMessages,
		CurrentTool:         sess.CurrentTool,
		LastFileWrite:       sess.LastFileWrite,
		LastFileWriteAt:     sess.LastFileWriteAt,
		RecentTools:         tools,
		RecentMessages:      msgs,
	}
	if sess.Desktop != nil {
		full.DesktopTitle = sess.Desktop.Title
		full.DesktopID = sess.Desktop.DesktopID
		full.PermissionMode = sess.Desktop.PermissionMode
	}
	return full
}

// GetActiveCount returns the number of sessions with active work.
func (s *SessionService) GetActiveCount() int {
	count := 0
	for _, sess := range s.manager.VisibleSessions() {
		if core.IsActiveActivity(s.manager.ActivityFor(sess.SessionID)) {
			count++
		}
	}
	return count
}

// GetWindowMinutes returns the current time window in minutes.
func (s *SessionService) GetWindowMinutes() int {
	return s.manager.WindowMinutes()
}

// SetWindowMinutes updates the time window.
func (s *SessionService) SetWindowMinutes(m int) {
	s.manager.SetWindowMinutes(m)
	s.emitUpdate()
}

// SetActivityFilter sets the activity filter.
func (s *SessionService) SetActivityFilter(f string) {
	s.manager.SetActivityFilter(core.ActivityKind(f))
	s.emitUpdate()
}

// SetSearchQuery sets the search query.
func (s *SessionService) SetSearchQuery(q string) {
	s.manager.SetSearchQuery(q)
	s.emitUpdate()
}

// OpenInEditor opens a directory in the user's editor.
// For Cursor sessions, it opens Cursor IDE directly.
// Otherwise it follows POSIX semantics: $VISUAL is a GUI editor (launched directly),
// $EDITOR is a terminal editor (opened inside a Terminal.app window).
// The config "editor" field is treated as VISUAL (GUI) for backward compatibility.
func (s *SessionService) OpenInEditor(cwd, agent string) {
	if cwd == "" {
		return
	}

	// Cursor sessions open in Cursor IDE.
	if agent == "cursor" {
		launchGUI("cursor", cwd)
		return
	}

	cfg := core.LoadConfig()

	// Config editor and VISUAL: launch directly (GUI editor).
	if editor := cfg.Editor; editor != "" {
		launchGUI(editor, cwd)
		return
	}
	if editor := os.Getenv("VISUAL"); editor != "" {
		launchGUI(editor, cwd)
		return
	}

	// EDITOR: open inside a terminal window.
	if editor := os.Getenv("EDITOR"); editor != "" {
		launchInTerminal(editor, cwd)
		return
	}
}

// launchGUI starts a GUI editor directly.
func launchGUI(editor, cwd string) {
	c := exec.Command(editor, cwd)
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	_ = c.Start()
}

// launchInTerminal opens a terminal editor inside a new macOS Terminal.app window.
func launchInTerminal(editor, cwd string) {
	script := fmt.Sprintf(`tell application "Terminal"
	activate
	do script "cd %s && %s"
end tell`, shellQuote(cwd), shellQuote(editor))
	c := exec.Command("osascript", "-e", script)
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	_ = c.Start()
}

// shellQuote returns a single-quoted string safe for embedding in AppleScript shell commands.
func shellQuote(s string) string {
	// Replace single quotes with '\'' (end quote, escaped quote, start quote).
	quoted := "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
	return quoted
}

// GetSessionName returns the custom name for a session.
func (s *SessionService) GetSessionName(sessionID string) string {
	return s.manager.SessionName(sessionID)
}

// SetSessionName stores a custom name for a session. Empty name resets it.
func (s *SessionService) SetSessionName(sessionID, name string) error {
	err := s.manager.SetSessionName(sessionID, name)
	if err == nil {
		s.emitUpdate()
	}
	return err
}

// GetConfig returns the current config.
func (s *SessionService) GetConfig() core.Config {
	return core.LoadConfig()
}
