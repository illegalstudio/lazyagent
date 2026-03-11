package opencode

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
	"github.com/nahime0/lazyagent/internal/model"
	_ "modernc.org/sqlite"
)

// SessionCache caches OpenCode sessions keyed by session ID.
// Uses time_updated from the DB as the staleness check.
type SessionCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
}

type cacheEntry struct {
	timeUpdated int64
	session     *model.Session
}

// NewSessionCache creates an empty OpenCode session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{entries: make(map[string]cacheEntry)}
}

// OpenCodeDataDir returns the path to the OpenCode data directory.
func OpenCodeDataDir() string {
	if d := os.Getenv("OPENCODE_DATA_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "opencode")
}

// DBPath returns the path to the OpenCode SQLite database.
func DBPath() string {
	d := OpenCodeDataDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "opencode.db")
}

// DiscoverSessions reads the OpenCode SQLite database and returns sessions.
func DiscoverSessions(cache *SessionCache) ([]*model.Session, error) {
	dbPath := DBPath()
	if dbPath == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, nil // OpenCode not installed
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, fmt.Errorf("open opencode db: %w", err)
	}
	defer db.Close()

	dbSessions, err := querySessions(db)
	if err != nil {
		return nil, err
	}

	type wtInfo struct {
		isWorktree bool
		mainRepo   string
	}
	wtCache := make(map[string]wtInfo)

	seen := make(map[string]struct{})
	var sessions []*model.Session
	for _, ds := range dbSessions {
		seen[ds.ID] = struct{}{}

		// Check cache using time_updated as staleness indicator.
		cache.mu.Lock()
		if e, ok := cache.entries[ds.ID]; ok && e.timeUpdated == ds.TimeUpdated {
			cache.mu.Unlock()
			sessions = append(sessions, e.session)
			continue
		}
		cache.mu.Unlock()

		session, err := buildSession(db, ds)
		if err != nil {
			continue
		}

		// Git worktree detection
		if session.CWD != "" {
			if _, ok := wtCache[session.CWD]; !ok {
				isWT, mainRepo := claude.IsWorktree(session.CWD)
				wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
			}
			wt := wtCache[session.CWD]
			session.IsWorktree = wt.isWorktree
			session.MainRepo = wt.mainRepo
		}

		cache.mu.Lock()
		cache.entries[ds.ID] = cacheEntry{timeUpdated: ds.TimeUpdated, session: session}
		cache.mu.Unlock()

		sessions = append(sessions, session)
	}

	// Prune sessions no longer in the DB.
	cache.mu.Lock()
	for id := range cache.entries {
		if _, ok := seen[id]; !ok {
			delete(cache.entries, id)
		}
	}
	cache.mu.Unlock()

	return sessions, nil
}

func querySessions(db *sql.DB) ([]dbSession, error) {
	rows, err := db.Query(`
		SELECT id, project_id, parent_id, directory, title, version,
		       time_created, time_updated, time_compacting, time_archived
		FROM session
		ORDER BY time_updated DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("query sessions: %w", err)
	}
	defer rows.Close()

	var sessions []dbSession
	for rows.Next() {
		var s dbSession
		if err := rows.Scan(&s.ID, &s.ProjectID, &s.ParentID, &s.Directory,
			&s.Title, &s.Version, &s.TimeCreated, &s.TimeUpdated,
			&s.TimeCompacting, &s.TimeArchived); err != nil {
			continue
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func buildSession(db *sql.DB, ds dbSession) (*model.Session, error) {
	session := &model.Session{
		SessionID:    ds.ID,
		CWD:          ds.Directory,
		Name:         ds.Title,
		Version:      ds.Version,
		Agent:        "opencode",
		LastActivity: time.UnixMilli(ds.TimeUpdated),
		IsSidechain:  ds.ParentID != nil && *ds.ParentID != "",
	}

	if ds.TimeCompacting != nil && *ds.TimeCompacting > 0 {
		session.LastSummaryAt = time.UnixMilli(*ds.TimeCompacting)
	}

	// Query messages with their parts in a single joined query.
	rows, err := db.Query(`
		SELECT m.id, m.data, p.data
		FROM message m
		LEFT JOIN part p ON p.message_id = m.id
		WHERE m.session_id = ?
		ORDER BY m.time_created ASC, p.time_created ASC
	`, ds.ID)
	if err != nil {
		return nil, fmt.Errorf("query messages: %w", err)
	}
	defer rows.Close()

	var recentTools []model.ToolCall
	var recentMessages []model.ConversationMessage
	var entryTimestamps []time.Time
	var lastMsgData *messageData
	var lastMsgParts []partData
	lastMsgID := ""

	for rows.Next() {
		var msgID, msgDataRaw string
		var partDataRaw *string
		if err := rows.Scan(&msgID, &msgDataRaw, &partDataRaw); err != nil {
			continue
		}

		// Parse message data (only once per message).
		if msgID != lastMsgID {
			// Finalize previous message's status tracking.
			if lastMsgID != "" {
				lastMsgParts = nil
			}
			lastMsgID = msgID
			lastMsgData = nil

			var md messageData
			if err := json.Unmarshal([]byte(msgDataRaw), &md); err != nil {
				continue
			}
			lastMsgData = &md

			var ts time.Time
			if md.Time != nil && md.Time.Created > 0 {
				ts = time.UnixMilli(md.Time.Created)
			}

			if !ts.IsZero() {
				entryTimestamps = append(entryTimestamps, ts)
				if len(entryTimestamps) > 500 {
					trimmed := make([]time.Time, 500)
					copy(trimmed, entryTimestamps[len(entryTimestamps)-500:])
					entryTimestamps = trimmed
				}
			}

			if md.ModelID != "" {
				session.Model = md.ModelID
			}

			// Accumulate tokens and cost.
			session.CostUSD += md.Cost
			if md.Tokens != nil {
				session.InputTokens += md.Tokens.Input
				session.OutputTokens += md.Tokens.Output
				session.CacheReadTokens += md.Tokens.Cache.Read
				session.CacheCreationTokens += md.Tokens.Cache.Write
			}

			switch md.Role {
			case "user":
				session.UserMessages++
			case "assistant":
				session.AssistantMessages++
			}
		}

		// Parse part data.
		if partDataRaw != nil {
			var pd partData
			if err := json.Unmarshal([]byte(*partDataRaw), &pd); err == nil {
				lastMsgParts = append(lastMsgParts, pd)

				var ts time.Time
				if lastMsgData != nil && lastMsgData.Time != nil && lastMsgData.Time.Created > 0 {
					ts = time.UnixMilli(lastMsgData.Time.Created)
				}

				switch pd.Type {
				case "text":
					if pd.Text != "" && lastMsgData != nil {
						recentMessages = append(recentMessages, model.ConversationMessage{
							Role: lastMsgData.Role, Text: model.Truncate(pd.Text, 300), Timestamp: ts,
						})
						if len(recentMessages) > 20 {
							recentMessages = recentMessages[len(recentMessages)-10:]
						}
					}
				case "tool":
					toolName := normalizeToolName(pd.Tool)
					recentTools = append(recentTools, model.ToolCall{
						Name: toolName, Timestamp: ts,
					})
					if len(recentTools) > 40 {
						recentTools = recentTools[len(recentTools)-20:]
					}
					if toolName == "Write" || toolName == "Edit" {
						if fp := extractToolFilePath(pd.State); fp != "" {
							session.LastFileWrite = fp
							session.LastFileWriteAt = ts
						}
					}
				}
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate messages: %w", err)
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages

	if len(recentTools) > 20 {
		recentTools = recentTools[len(recentTools)-20:]
	}
	session.RecentTools = recentTools

	if len(recentMessages) > 10 {
		recentMessages = recentMessages[len(recentMessages)-10:]
	}
	session.RecentMessages = recentMessages
	session.EntryTimestamps = entryTimestamps

	// Determine status from the last message.
	session.Status = determineStatus(lastMsgData, lastMsgParts)
	if session.Status == model.StatusExecutingTool {
		for i := len(lastMsgParts) - 1; i >= 0; i-- {
			if lastMsgParts[i].Type == "tool" {
				session.CurrentTool = normalizeToolName(lastMsgParts[i].Tool)
				break
			}
		}
	}

	return session, nil
}

func determineStatus(msg *messageData, parts []partData) model.SessionStatus {
	if msg == nil {
		return model.StatusUnknown
	}
	switch msg.Role {
	case "assistant":
		if msg.Finish == "tool-calls" {
			return model.StatusExecutingTool
		}
		for _, p := range parts {
			if p.Type == "tool" {
				return model.StatusExecutingTool
			}
		}
		return model.StatusWaitingForUser
	case "user":
		return model.StatusThinking
	}
	return model.StatusUnknown
}

// normalizeToolName maps OpenCode tool names to PascalCase names
// matching the activity tracker expectations.
func normalizeToolName(name string) string {
	switch name {
	case "bash":
		return "Bash"
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "glob":
		return "Glob"
	case "grep":
		return "Grep"
	case "task":
		return "Agent"
	case "web_search":
		return "WebSearch"
	case "web_fetch":
		return "WebFetch"
	default:
		if len(name) > 0 {
			return strings.ToUpper(name[:1]) + name[1:]
		}
		return name
	}
}

// extractToolFilePath extracts a file path from a tool's state JSON.
func extractToolFilePath(state json.RawMessage) string {
	if len(state) == 0 {
		return ""
	}
	var s partState
	if json.Unmarshal(state, &s) != nil {
		return ""
	}
	if len(s.Input) == 0 {
		return ""
	}
	var input struct {
		FilePath string `json:"file_path"`
		Path     string `json:"path"`
	}
	if json.Unmarshal(s.Input, &input) != nil {
		return ""
	}
	if input.FilePath != "" {
		return input.FilePath
	}
	return input.Path
}

