//go:build !notray

package tray

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/apiauth"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/demo"
	"github.com/illegalstudio/lazyagent/internal/limits"
	"github.com/illegalstudio/lazyagent/internal/model"
	"github.com/illegalstudio/lazyagent/internal/sessions"
	"github.com/illegalstudio/lazyagent/internal/version"
	"github.com/illegalstudio/lazyagent/internal/webhook"
	"github.com/pkg/browser"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// SessionService is the Go service exposed to the Svelte frontend via Wails bindings.
type SessionService struct {
	manager        *core.SessionManager
	app            *application.App
	ctx            context.Context
	demoMode       bool
	provider       core.SessionProvider // if set, used instead of demoMode logic
	updateMu       sync.RWMutex
	updateVersion  string // newer version available, empty if up-to-date
	panelWindow    *application.WebviewWindow
	detachedWindow *application.WebviewWindow
	detachMu       sync.RWMutex
	detached       bool
	pinned         bool
	desktopOnce    sync.Once // one-time desktop setup: app icon + native menu
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
			provider = core.NewLiveProvider(cfg.ClaudeDirs)
		}
	}
	s.manager = core.NewSessionManager(cfg.WindowMinutes, provider)
	s.manager.SetExcludeCWDSubstrings(cfg.ExcludeCWDSubstrings)
	if dir, ok := sessions.ResolveCacheDir(); ok {
		s.manager.EnableCachePersistence(dir)
	}
	if len(cfg.ValidWebhooks()) > 0 {
		bus := core.NewEventBus()
		s.manager.SetEventBus(bus)
		httpClient := &http.Client{Timeout: 10 * time.Second}
		d := webhook.New(bus, &webhook.ConfigAdapter{Cfg: cfg}, httpClient, func() string { return "" })
		go func() { _ = d.Start(s.ctx) }()
	}
	if err := s.manager.StartWatcher(); err != nil {
		return err
	}

	// Progressive first load: BeginReloadStreaming synchronously marks the
	// stream as in-flight (core.SessionManager's streamInFlight guard)
	// BEFORE watchLoop starts below -- watchLoop's ticker/event cases call
	// Reload/UpdateActivities unconditionally on every tick or file event,
	// so this ordering is what actually prevents a race: a bare
	// `go s.manager.ReloadStreaming(...)` followed immediately by
	// `go s.watchLoop()` could not otherwise guarantee the guard is active
	// before watchLoop's first tick or an already-queued watcher event (the
	// watcher was started above, so its channel can already have one
	// buffered) gets processed. The actual (potentially slow) discovery
	// work still runs in the background so ServiceStartup returns
	// immediately. Each batch calls emitUpdate, so the frontend's existing
	// "sessions:updated" listener (App.svelte) re-fetches via GetSessions
	// and the list grows in place — no frontend change needed, since
	// GetSessions already just returns whatever's currently in the
	// manager. Subsequent reloads (watchLoop below) stay synchronous.
	run := s.manager.BeginReloadStreaming()
	go run(s.emitUpdate)

	// Background goroutine: watch for file changes + periodic refresh
	go s.watchLoop()

	// Background update check
	go func() {
		if v := version.CheckLatest(); v != "" {
			s.updateMu.Lock()
			s.updateVersion = v
			s.updateMu.Unlock()
		}
	}()

	return nil
}

// ServiceShutdown is called by Wails when the app stops.
func (s *SessionService) ServiceShutdown() error {
	s.manager.Close()
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
	SessionID           string    `json:"sessionId"`
	Agent               string    `json:"agent"`
	Source              string    `json:"source"`
	CWD                 string    `json:"cwd"`
	ShortName           string    `json:"shortName"`
	AgentName           string    `json:"agentName"`
	CustomName          string    `json:"customName"`
	Activity            string    `json:"activity"`
	IsActive            bool      `json:"isActive"`
	Model               string    `json:"model"`
	GitBranch           string    `json:"gitBranch"`
	CostUSD             float64   `json:"costUsd"`
	LastActivity        time.Time `json:"lastActivity"`
	TotalMessages       int       `json:"totalMessages"`
	SparklineData       []int     `json:"sparklineData"`
	CurrentTool         string    `json:"currentTool"`
	LastMessage         string    `json:"lastMessage"`
	ResumeAvailable     bool      `json:"resumeAvailable"`
	YoloResumeAvailable bool      `json:"yoloResumeAvailable"`
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
	LastFileWrite       string             `json:"lastFileWrite"`
	LastFileWriteAt     time.Time          `json:"lastFileWriteAt"`
	RecentTools         []ToolItem         `json:"recentTools"`
	RecentMessages      []ConversationItem `json:"recentMessages"`
	DesktopTitle        string             `json:"desktopTitle,omitempty"`
	DesktopID           string             `json:"desktopId,omitempty"`
	PermissionMode      string             `json:"permissionMode,omitempty"`
	RemoteURL           string             `json:"remoteUrl,omitempty"`
	ResumeCommand       string             `json:"resumeCommand,omitempty"`
	ResumeCommandYolo   string             `json:"resumeCommandYolo,omitempty"`
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
		SessionID:           sess.SessionID,
		Agent:               sess.Agent,
		Source:              source,
		CWD:                 sess.CWD,
		ShortName:           core.ShortName(sess.CWD, nameLen),
		AgentName:           sess.Name,
		CustomName:          s.manager.SessionName(sess.SessionID),
		Activity:            string(activity),
		IsActive:            core.IsActiveActivity(activity),
		Model:               sess.Model,
		GitBranch:           sess.GitBranch,
		CostUSD:             core.EffectiveCost(sess.Model, sess.CostUSD, sess.InputTokens, sess.OutputTokens, sess.CacheCreationTokens, sess.CacheReadTokens),
		LastActivity:        sess.LastActivity,
		TotalMessages:       sess.TotalMessages,
		SparklineData:       core.BucketTimestamps(sess.EntryTimestamps, time.Duration(wm)*time.Minute, 20),
		CurrentTool:         sess.CurrentTool,
		LastMessage:         lastMessageSnippet(sess),
		ResumeAvailable:     core.ResumeArgv(sess.Agent, sess.SessionID) != nil,
		YoloResumeAvailable: core.YoloResumeArgv(sess.Agent, sess.SessionID) != nil,
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
		ago := ""
		// Only timestamped tool calls get a relative age — agents like Grok
		// record a per-tool time only for the most recent call.
		if !t.Timestamp.IsZero() {
			ago = core.FormatDuration(time.Since(t.Timestamp))
		}
		tools = append(tools, ToolItem{
			Name:      t.Name,
			Timestamp: t.Timestamp,
			Ago:       ago,
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
	full.RemoteURL = sess.RemoteURL
	full.ResumeCommand = core.ResumeCommand(sess.Agent, sess.SessionID)
	full.ResumeCommandYolo = core.YoloResumeCommand(sess.Agent, sess.SessionID)
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

// GetLimits fetches all supported providers and returns the computed limits
// view. The GUI calls it when the limits page opens and on manual refresh; it
// does not poll. Missing/errored agents are omitted.
func (s *SessionService) GetLimits() limits.View {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return limits.BuildView(limits.FetchAll(ctx), time.Now())
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
// $EDITOR is a terminal editor (opened inside the configured terminal).
// The config "editor" field is treated as VISUAL (GUI) for backward compatibility.
func (s *SessionService) OpenInEditor(cwd, agent string) {
	if cwd == "" {
		return
	}

	// Cursor sessions open in Cursor IDE if available.
	if agent == "cursor" {
		if _, err := exec.LookPath("cursor"); err == nil {
			launchGUI("cursor", cwd)
			return
		}
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

// launchInTerminal opens a terminal editor inside a new terminal window.
func launchInTerminal(editor, cwd string) {
	launchCommandInTerminal([]string{editor}, cwd)
}

// launchCommandInTerminal opens a new window of the configured terminal
// emulator (config key "terminal") in cwd and runs argv, shell-quoting
// every element.
func launchCommandInTerminal(argv []string, cwd string) {
	term := core.NormalizeTerminal(core.LoadConfig().Terminal)
	full := terminalCommand(term, cwd, argv)
	if len(full) == 0 {
		return
	}
	c := exec.Command(full[0], full[1:]...)
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	if err := c.Start(); err != nil {
		return
	}
	terminalStarted(c, term)
}

// Refresh forces a session reload. Bound to the toolbar refresh button and
// the View menu.
func (s *SessionService) Refresh() {
	_ = s.manager.Reload()
	s.emitUpdate()
}

// ResumeInTerminal opens a new terminal window in the session's working
// directory running either the normal or agent-specific YOLO resume command.
func (s *SessionService) ResumeInTerminal(sessionID string, yolo bool) {
	detail := s.manager.SessionDetail(sessionID)
	if detail == nil {
		return
	}
	mode := core.ResumeNormal
	if yolo {
		mode = core.ResumeYolo
	}
	argv := core.ResumeArgvWithMode(detail.Session.Agent, sessionID, mode)
	if argv == nil || detail.Session.CWD == "" {
		return
	}
	launchCommandInTerminal(argv, detail.Session.CWD)
}

// shellQuote returns a single-quoted string safe for embedding in shell commands.
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

// GetUpdateVersion returns the newer version available, or empty if up-to-date.
func (s *SessionService) GetUpdateVersion() string {
	s.updateMu.RLock()
	defer s.updateMu.RUnlock()
	return s.updateVersion
}

// OpenReleases opens the GitHub releases page in the system browser.
func (s *SessionService) OpenReleases() {
	browser.OpenURL("https://github.com/illegalstudio/lazyagent/releases")
}

// GetConfig returns the current config. The API passphrase is scrubbed
// before returning so the secret never crosses the Wails IPC boundary into
// the frontend (mirrors what /api/config does for HTTP callers).
func (s *SessionService) GetConfig() core.Config {
	cfg := core.LoadConfig()
	cfg.APIPassphrase = ""
	return cfg
}

// GetCardDensity returns the persisted desktop card density. Missing or
// invalid values fall back to "live".
func (s *SessionService) GetCardDensity() string {
	return core.NormalizeCardDensity(core.LoadConfig().CardDensity)
}

// SetCardDensity persists the desktop card density choice.
// core.UpdateConfig holds a file lock for the whole read-modify-write, so
// concurrent writers (this process or another lazyagent process) cannot
// clobber each other's config fields.
func (s *SessionService) SetCardDensity(d string) error {
	if core.NormalizeCardDensity(d) != d {
		return fmt.Errorf("invalid card density %q", d)
	}
	return core.UpdateConfig(func(c *core.Config) { c.CardDensity = d })
}

// GetDetailWidth returns the persisted desktop detail-panel width in
// pixels. Missing or out-of-range values fall back to 400.
func (s *SessionService) GetDetailWidth() int {
	return core.NormalizeDetailWidth(core.LoadConfig().DetailWidth)
}

// SetDetailWidth persists the desktop detail-panel width. Unlike the
// density setter it clamps instead of rejecting: the value comes from a
// drag gesture, not typed input.
func (s *SessionService) SetDetailWidth(w int) error {
	return core.UpdateConfig(func(c *core.Config) {
		c.DetailWidth = core.NormalizeDetailWidth(w)
	})
}

// Settings is the GUI-editable subset of the config.
type Settings struct {
	Terminal             string          `json:"terminal"`
	Editor               string          `json:"editor"`
	Agents               map[string]bool `json:"agents"`
	ExcludeCWDSubstrings []string        `json:"excludeCwdSubstrings"`
}

// GetSettings returns the GUI-editable settings.
func (s *SessionService) GetSettings() Settings {
	cfg := core.LoadConfig()
	return Settings{
		Terminal:             core.NormalizeTerminal(cfg.Terminal),
		Editor:               cfg.Editor,
		Agents:               cfg.Agents,
		ExcludeCWDSubstrings: cfg.ExcludeCWDSubstrings,
	}
}

// SaveSettings persists the GUI-editable settings. Exclude filters apply
// immediately; agent toggles take effect at the next launch (the session
// provider is built at startup).
func (s *SessionService) SaveSettings(st Settings) error {
	if err := core.UpdateConfig(func(c *core.Config) {
		c.Terminal = core.NormalizeTerminal(st.Terminal)
		c.Editor = strings.TrimSpace(st.Editor)
		if st.Agents != nil {
			c.Agents = st.Agents
		}
		c.ExcludeCWDSubstrings = st.ExcludeCWDSubstrings
	}); err != nil {
		return err
	}
	s.manager.SetExcludeCWDSubstrings(st.ExcludeCWDSubstrings)
	_ = s.manager.Reload()
	s.emitUpdate()
	return nil
}

// IsAPIConfigured reports whether an API passphrase is set. The passphrase
// itself never crosses the IPC boundary (GetConfig scrubs it).
func (s *SessionService) IsAPIConfigured() bool {
	return core.LoadConfig().APIPassphrase != ""
}

// SetAPIPassphrase sets — or, with an empty string, clears — the API
// passphrase, ensuring a salt exists. A running --api server picks the
// change up at its next restart.
func (s *SessionService) SetAPIPassphrase(p string) error {
	return core.UpdateConfig(func(c *core.Config) {
		c.APIPassphrase = strings.TrimSpace(p)
		if c.APIPassphrase != "" {
			core.EnsureAPISalt(c)
		}
	})
}

// GetAPIToken derives and returns the API bearer token. It exists for the
// explicit "copy token" action in Settings; empty when unconfigured.
func (s *SessionService) GetAPIToken() string {
	cfg := core.LoadConfig()
	if cfg.APIPassphrase == "" {
		return ""
	}
	return apiauth.DeriveToken(cfg.APIPassphrase, cfg.APISalt)
}

// Detach switches from tray panel to a normal detached window.
func (s *SessionService) Detach() {
	s.detachMu.Lock()
	if s.detached {
		s.detachMu.Unlock()
		return
	}
	s.detached = true
	s.detachMu.Unlock()
	s.panelWindow.Hide()
	s.desktopOnce.Do(func() {
		s.app.SetIcon(appIcon)
		installAppMenu(s.app, s)
	})
	// Load-bearing order: the activation policy switch is dispatched to the
	// main queue before Show(), so the window appears as a normal desktop-app
	// window (Dock icon, Cmd-Tab) rather than briefly showing as an accessory.
	setDesktopActivation(true)
	s.detachedWindow.Show().Focus()
	s.detachedWindow.Center()
	s.emitDetachChanged()
}

// Attach switches from detached window back to tray panel mode.
func (s *SessionService) Attach() {
	s.detachMu.Lock()
	if !s.detached {
		s.detachMu.Unlock()
		return
	}
	s.detached = false
	resetPin := s.pinned
	s.pinned = false
	s.detachMu.Unlock()
	if resetPin {
		s.detachedWindow.SetAlwaysOnTop(false)
	}
	s.detachedWindow.Hide()
	setDesktopActivation(false)
	s.emitDetachChanged()
}

// IsDetached returns whether the app is currently in detached window mode.
func (s *SessionService) IsDetached() bool {
	s.detachMu.RLock()
	defer s.detachMu.RUnlock()
	return s.detached
}

// TogglePin toggles always-on-top for the detached window.
func (s *SessionService) TogglePin() {
	s.detachMu.Lock()
	if !s.detached {
		s.detachMu.Unlock()
		return
	}
	s.pinned = !s.pinned
	pinned := s.pinned
	s.detachMu.Unlock()
	s.detachedWindow.SetAlwaysOnTop(pinned)
	s.emitDetachChanged()
}

// IsPinned returns whether the detached window is always on top.
func (s *SessionService) IsPinned() bool {
	s.detachMu.RLock()
	defer s.detachMu.RUnlock()
	return s.pinned
}

func (s *SessionService) emitDetachChanged() {
	if s.app != nil {
		s.app.Event.Emit("detach:changed")
	}
}

// lastMessageSnippet returns a short single-line snippet of the newest
// recent message, shown on the desktop "live" card. RecentMessages is
// chronological, so the newest entry is the last one.
func lastMessageSnippet(sess *model.Session) string {
	if len(sess.RecentMessages) == 0 {
		return ""
	}
	text := sess.RecentMessages[len(sess.RecentMessages)-1].Text
	text = strings.Join(strings.Fields(text), " ")
	return core.TruncateRunes(text, 140)
}
