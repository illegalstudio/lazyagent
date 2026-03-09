package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
	"github.com/nahime0/lazyagent/internal/core"
	"github.com/nahime0/lazyagent/internal/demo"
)

// DefaultPort is the preferred port. If busy, the server tries sequential ports.
const DefaultPort = 7421

// maxPortAttempts is how many ports to try before giving up.
const maxPortAttempts = 10

// Server is the lazyagent HTTP API server.
type Server struct {
	manager *core.SessionManager
	mux     *http.ServeMux
	srv     *http.Server
	ln      net.Listener

	// SSE subscribers.
	ssemu   sync.Mutex
	sseSubs map[chan struct{}]struct{}
}

// New creates a new API server.
// If host is non-empty it binds to that address (e.g. ":7421" or "0.0.0.0:7421").
// If host is empty it binds to 127.0.0.1 on DefaultPort with fallback.
func New(host string, demoMode bool) (*Server, error) {
	cfg := core.LoadConfig()
	var provider core.SessionProvider
	if demoMode {
		provider = demo.Provider{}
	} else {
		provider = core.LiveProvider{}
	}
	manager := core.NewSessionManager(cfg.WindowMinutes, provider)

	s := &Server{
		manager: manager,
		sseSubs: make(map[chan struct{}]struct{}),
	}
	s.mux = http.NewServeMux()
	s.routes()
	s.srv = &http.Server{
		Handler:           corsMiddleware(s.mux),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MiB
	}

	ln, err := listen(host)
	if err != nil {
		return nil, err
	}
	s.ln = ln
	log.Printf("API server listening on http://%s", ln.Addr())
	return s, nil
}

// listen resolves the listener. Explicit host binds directly; empty host tries
// DefaultPort..DefaultPort+maxPortAttempts on 127.0.0.1.
func listen(host string) (net.Listener, error) {
	if host != "" {
		ln, err := net.Listen("tcp", host)
		if err != nil {
			return nil, fmt.Errorf("listen %s: %w", host, err)
		}
		return ln, nil
	}

	for i := range maxPortAttempts {
		addr := fmt.Sprintf("127.0.0.1:%d", DefaultPort+i)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			return ln, nil
		}
	}
	return nil, fmt.Errorf("no available port in range %d–%d", DefaultPort, DefaultPort+maxPortAttempts-1)
}

// Addr returns the address the server is listening on.
func (s *Server) Addr() net.Addr {
	return s.ln.Addr()
}

// Run starts the watcher, background loops, and serves HTTP. Blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	if err := s.manager.StartWatcher(); err != nil {
		log.Printf("Warning: file watcher unavailable: %v", err)
	}
	defer s.manager.StopWatcher()

	if err := s.manager.Reload(); err != nil {
		log.Printf("Warning: initial reload failed: %v", err)
	}

	go s.watchLoop(ctx)

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.srv.Serve(s.ln)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s.srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func (s *Server) watchLoop(ctx context.Context) {
	events := s.manager.WatcherEvents()
	activityTicker := time.NewTicker(1 * time.Second)
	defer activityTicker.Stop()
	reloadTicker := time.NewTicker(30 * time.Second)
	defer reloadTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-events:
			_ = s.manager.Reload()
			s.notifySSE()
		case <-activityTicker.C:
			if s.manager.UpdateActivities() {
				s.notifySSE()
			}
		case <-reloadTicker.C:
			_ = s.manager.Reload()
			s.notifySSE()
		}
	}
}

// --- SSE ---

func (s *Server) addSSESub(ch chan struct{}) {
	s.ssemu.Lock()
	s.sseSubs[ch] = struct{}{}
	s.ssemu.Unlock()
}

func (s *Server) removeSSESub(ch chan struct{}) {
	s.ssemu.Lock()
	delete(s.sseSubs, ch)
	s.ssemu.Unlock()
}

func (s *Server) notifySSE() {
	s.ssemu.Lock()
	defer s.ssemu.Unlock()
	for ch := range s.sseSubs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// --- CORS ---

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Routes ---

func (s *Server) routes() {
	s.mux.HandleFunc("GET /api", s.handlePlayground)
	s.mux.HandleFunc("GET /api/sessions", s.handleGetSessions)
	s.mux.HandleFunc("GET /api/sessions/{id}", s.handleGetSession)
	s.mux.HandleFunc("PUT /api/sessions/{id}/name", s.handleSetSessionName)
	s.mux.HandleFunc("DELETE /api/sessions/{id}/name", s.handleDeleteSessionName)
	s.mux.HandleFunc("GET /api/stats", s.handleGetStats)
	s.mux.HandleFunc("GET /api/config", s.handleGetConfig)
	s.mux.HandleFunc("GET /api/events", s.handleSSE)
}

// --- Handlers ---

func (s *Server) handleGetSessions(w http.ResponseWriter, r *http.Request) {
	search := r.URL.Query().Get("search")
	filter := core.ActivityKind(r.URL.Query().Get("filter"))

	visible := s.manager.QuerySessions(search, filter)
	items := make([]SessionItem, 0, len(visible))
	for _, sess := range visible {
		activity := s.manager.ActivityFor(sess.SessionID)
		items = append(items, s.buildSessionItem(sess, activity))
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleGetSession(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	detail := s.manager.SessionDetail(id)
	if detail == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	writeJSON(w, http.StatusOK, s.buildSessionFull(detail))
}

func (s *Server) handleSetSessionName(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if err := s.manager.SetSessionName(id, name); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.notifySSE()
	writeJSON(w, http.StatusOK, map[string]string{"session_id": id, "custom_name": name})
}

func (s *Server) handleDeleteSessionName(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.manager.SetSessionName(id, ""); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.notifySSE()
	writeJSON(w, http.StatusOK, map[string]string{"session_id": id, "custom_name": ""})
}

func (s *Server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	visible := s.manager.QuerySessions("", "")
	activeCount := 0
	for _, sess := range visible {
		if core.IsActiveActivity(s.manager.ActivityFor(sess.SessionID)) {
			activeCount++
		}
	}
	writeJSON(w, http.StatusOK, StatsResponse{
		TotalSessions:  len(visible),
		ActiveSessions: activeCount,
		WindowMinutes:  s.manager.WindowMinutes(),
	})
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, core.LoadConfig())
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := make(chan struct{}, 1)
	s.addSSESub(ch)
	defer s.removeSSESub(ch)

	// Send initial snapshot immediately.
	s.writeSSEFrame(w, flusher)

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ch:
			s.writeSSEFrame(w, flusher)
		}
	}
}

func (s *Server) writeSSEFrame(w http.ResponseWriter, flusher http.Flusher) {
	visible := s.manager.QuerySessions("", "")

	items := make([]SessionItem, 0, len(visible))
	activeCount := 0
	for _, sess := range visible {
		activity := s.manager.ActivityFor(sess.SessionID)
		items = append(items, s.buildSessionItem(sess, activity))
		if core.IsActiveActivity(activity) {
			activeCount++
		}
	}

	payload := SSEPayload{
		Sessions: items,
		Stats: StatsResponse{
			TotalSessions:  len(visible),
			ActiveSessions: activeCount,
			WindowMinutes:  s.manager.WindowMinutes(),
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("SSE: marshal error: %v", err)
		return
	}
	fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)
	flusher.Flush()
}

// --- Types ---

// SessionItem is a lightweight session for list views.
type SessionItem struct {
	SessionID     string    `json:"session_id"`
	CWD           string    `json:"cwd"`
	ShortName     string    `json:"short_name"`
	CustomName    string    `json:"custom_name,omitempty"`
	Activity      string    `json:"activity"`
	IsActive      bool      `json:"is_active"`
	Model         string    `json:"model"`
	GitBranch     string    `json:"git_branch"`
	CostUSD       float64   `json:"cost_usd"`
	LastActivity  time.Time `json:"last_activity"`
	TotalMessages int       `json:"total_messages"`
}

// SessionFull is the detailed session representation.
type SessionFull struct {
	SessionItem
	Version             string             `json:"version"`
	IsWorktree          bool               `json:"is_worktree"`
	MainRepo            string             `json:"main_repo"`
	InputTokens         int                `json:"input_tokens"`
	OutputTokens        int                `json:"output_tokens"`
	CacheCreationTokens int                `json:"cache_creation_tokens"`
	CacheReadTokens     int                `json:"cache_read_tokens"`
	UserMessages        int                `json:"user_messages"`
	AssistantMessages   int                `json:"assistant_messages"`
	CurrentTool         string             `json:"current_tool"`
	LastFileWrite       string             `json:"last_file_write"`
	LastFileWriteAt     time.Time          `json:"last_file_write_at"`
	RecentTools         []ToolItem         `json:"recent_tools"`
	RecentMessages      []ConversationItem `json:"recent_messages"`
}

// ToolItem represents a recent tool call.
type ToolItem struct {
	Name      string    `json:"name"`
	Timestamp time.Time `json:"timestamp"`
}

// ConversationItem represents a conversation message.
type ConversationItem struct {
	Role      string    `json:"role"`
	Text      string    `json:"text"`
	Timestamp time.Time `json:"timestamp"`
}

// StatsResponse is returned by GET /api/stats and included in SSE frames.
type StatsResponse struct {
	TotalSessions  int `json:"total_sessions"`
	ActiveSessions int `json:"active_sessions"`
	WindowMinutes  int `json:"window_minutes"`
}

// SSEPayload is the data pushed on each SSE "update" event.
type SSEPayload struct {
	Sessions []SessionItem `json:"sessions"`
	Stats    StatsResponse `json:"stats"`
}

// --- Builders ---

func (s *Server) buildSessionItem(sess *claude.Session, activity core.ActivityKind) SessionItem {
	return SessionItem{
		SessionID:     sess.SessionID,
		CWD:           sess.CWD,
		ShortName:     core.ShortName(sess.CWD, 60),
		CustomName:    s.manager.SessionName(sess.SessionID),
		Activity:      string(activity),
		IsActive:      core.IsActiveActivity(activity),
		Model:         sess.Model,
		GitBranch:     sess.GitBranch,
		CostUSD:       core.EffectiveCost(sess.Model, sess.CostUSD, sess.InputTokens, sess.OutputTokens, sess.CacheCreationTokens, sess.CacheReadTokens),
		LastActivity:  sess.LastActivity,
		TotalMessages: sess.TotalMessages,
	}
}

func (s *Server) buildSessionFull(detail *core.SessionDetailView) SessionFull {
	sess := &detail.Session
	tools := make([]ToolItem, 0, len(sess.RecentTools))
	for _, t := range sess.RecentTools {
		tools = append(tools, ToolItem{
			Name:      t.Name,
			Timestamp: t.Timestamp,
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
	return SessionFull{
		SessionItem:         s.buildSessionItem(sess, detail.Activity),
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
}

// --- Helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
