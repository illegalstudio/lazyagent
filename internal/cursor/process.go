package cursor

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
	"github.com/nahime0/lazyagent/internal/model"
	_ "modernc.org/sqlite"
)

// recentWindow controls how far back we look for sessions.
const recentWindow = 48 * time.Hour

// SessionCache holds per-session cached data and WAL state for invalidation.
type SessionCache struct {
	mu      sync.Mutex
	entries map[string]*cachedSession

	lastWALMtime time.Time
	lastWALSize  int64
	// Cached composer summaries (cheap to re-fetch).
	composers []composerSummary
}

type cachedSession struct {
	bubbleCount int // number of bubbles when last parsed
	session     *model.Session
}

// composerSummary is the lightweight per-session info from composerData.
type composerSummary struct {
	sid        string
	count      int
	lastBubble string // UUID of last bubble
}

// NewSessionCache creates an empty Cursor session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{entries: make(map[string]*cachedSession)}
}

// stateDBPath returns the path to Cursor's global state.vscdb.
func stateDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Application Support", "Cursor", "User", "globalStorage", "state.vscdb")
}

// StateDBDir returns the directory containing state.vscdb for WatchDirs.
func StateDBDir() string {
	p := stateDBPath()
	if p == "" {
		return ""
	}
	return filepath.Dir(p)
}

// DiscoverSessions discovers recent Cursor sessions from state.vscdb.
//
// Strategy:
//  1. Read composerData entries (330ms) to get session IDs + bubble counts + last bubble ID.
//  2. For each session in 48h window, look up last bubble's createdAt (7ms each).
//  3. Only re-parse full bubble data when bubble count changes.
func DiscoverSessions(cache *SessionCache) ([]*model.Session, error) {
	dbPath := stateDBPath()
	if dbPath == "" {
		return nil, nil
	}
	if _, err := os.Stat(dbPath); err != nil {
		return nil, nil
	}

	// Quick WAL check for fast no-op when nothing changed at all.
	walMtime, walSize := walStats(dbPath)
	cache.mu.Lock()
	noChange := walMtime.Equal(cache.lastWALMtime) && walSize == cache.lastWALSize && len(cache.composers) > 0
	cache.mu.Unlock()

	if noChange {
		return cachedResults(cache), nil
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	// Step 1: Get composer summaries (session ID, bubble count, last bubble ID).
	composers, err := queryComposers(db)
	if err != nil {
		return nil, nil
	}

	// Step 2: For each composer, get last bubble timestamp and filter to recent.
	cutoff := time.Now().Add(-recentWindow)

	type wtInfo struct {
		isWorktree bool
		mainRepo   string
	}
	wtCache := make(map[string]wtInfo)
	var sessions []*model.Session

	for _, c := range composers {
		if c.count == 0 || c.lastBubble == "" {
			continue
		}

		lastAt := getExactBubbleTimestamp(db, c.sid, c.lastBubble)
		if lastAt.IsZero() || !lastAt.After(cutoff) {
			continue
		}

		// Check if we have a cached session with the same bubble count.
		cache.mu.Lock()
		if cached, ok := cache.entries[c.sid]; ok && cached.bubbleCount == c.count {
			if cached.session.LastActivity.Equal(lastAt) {
				sessions = append(sessions, cached.session)
			} else {
				// Timestamp changed — clone to avoid mutating a pointer shared with the manager.
				clone := *cached.session
				clone.LastActivity = lastAt
				cached.session = &clone
				sessions = append(sessions, &clone)
			}
			cache.mu.Unlock()
			continue
		}
		cache.mu.Unlock()

		// Need to (re-)parse this session's bubbles.
		session := &model.Session{
			SessionID:     c.sid,
			Agent:         "cursor",
			LastActivity:  lastAt,
			TotalMessages: c.count,
		}
		buildStateSession(db, c.sid, session)

		session.TotalMessages = session.UserMessages + session.AssistantMessages
		if session.TotalMessages == 0 {
			session.TotalMessages = c.count
		}

		// Git worktree detection.
		if session.CWD != "" {
			if _, ok := wtCache[session.CWD]; !ok {
				isWT, mainRepo := claude.IsWorktree(session.CWD)
				wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
			}
			wt := wtCache[session.CWD]
			session.IsWorktree = wt.isWorktree
			session.MainRepo = wt.mainRepo
		}

		sessions = append(sessions, session)

		cache.mu.Lock()
		cache.entries[c.sid] = &cachedSession{bubbleCount: c.count, session: session}
		cache.mu.Unlock()
	}

	// Prune stale cache entries not present in current results.
	activeIDs := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		activeIDs[s.SessionID] = struct{}{}
	}
	cache.mu.Lock()
	for id := range cache.entries {
		if _, ok := activeIDs[id]; !ok {
			delete(cache.entries, id)
		}
	}
	cache.lastWALMtime = walMtime
	cache.lastWALSize = walSize
	cache.composers = composers
	cache.mu.Unlock()

	return sessions, nil
}

func cachedResults(cache *SessionCache) []*model.Session {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	cutoff := time.Now().Add(-recentWindow)
	var sessions []*model.Session
	for _, c := range cache.entries {
		if c.session.LastActivity.After(cutoff) {
			sessions = append(sessions, c.session)
		}
	}
	return sessions
}

// walStats returns the WAL file's mtime and size for cache invalidation.
func walStats(dbPath string) (time.Time, int64) {
	info, err := os.Stat(dbPath + "-wal")
	if err != nil {
		// No WAL — fall back to main DB.
		if info, err := os.Stat(dbPath); err == nil {
			return info.ModTime(), info.Size()
		}
		return time.Time{}, 0
	}
	return info.ModTime(), info.Size()
}

// queryComposers reads composerData entries to get session summaries.
// Uses json_extract on the last element of fullConversationHeadersOnly.
func queryComposers(db *sql.DB) ([]composerSummary, error) {
	rows, err := db.Query(`
		SELECT json_extract(value, '$.composerId'),
		       json_array_length(json_extract(value, '$.fullConversationHeadersOnly')),
		       json_extract(value, '$.fullConversationHeadersOnly[#-1].bubbleId')
		FROM cursorDiskKV
		WHERE key LIKE 'composerData:%'
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []composerSummary
	for rows.Next() {
		var c composerSummary
		var lastBubble *string
		if err := rows.Scan(&c.sid, &c.count, &lastBubble); err != nil {
			continue
		}
		if lastBubble != nil {
			c.lastBubble = *lastBubble
		}
		result = append(result, c)
	}
	return result, rows.Err()
}

// getExactBubbleTimestamp looks up a specific bubble's createdAt by exact key.
func getExactBubbleTimestamp(db *sql.DB, sid, bubbleID string) time.Time {
	key := "bubbleId:" + sid + ":" + bubbleID
	var raw *string
	if err := db.QueryRow("SELECT json_extract(value, '$.createdAt') FROM cursorDiskKV WHERE key = ?", key).Scan(&raw); err != nil || raw == nil {
		return time.Time{}
	}
	return parseISO(*raw)
}

func parseISO(s string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, _ = time.Parse("2006-01-02T15:04:05.000Z", s)
	}
	return t
}

// buildStateSession populates session details from state.vscdb bubbleId entries.
// Reads bubbles in the order specified by composerData headers.
func buildStateSession(db *sql.DB, sid string, session *model.Session) {
	// Get the bubble order from composerData.
	var composerJSON string
	err := db.QueryRow(
		"SELECT value FROM cursorDiskKV WHERE key = ?",
		"composerData:"+sid,
	).Scan(&composerJSON)
	if err != nil {
		return
	}

	var composer struct {
		Headers []struct {
			BubbleID string `json:"bubbleId"`
			Type     int    `json:"type"`
		} `json:"fullConversationHeadersOnly"`
	}
	if err := json.Unmarshal([]byte(composerJSON), &composer); err != nil {
		return
	}

	if len(composer.Headers) == 0 {
		return
	}

	// Batch-fetch all bubbles for this session in one query.
	bubbleMap := fetchBubbles(db, sid)
	if len(bubbleMap) == 0 {
		return
	}

	var recentTools []model.ToolCall
	var recentMessages []model.ConversationMessage
	var lastBubbleType int
	var lastHadTool bool
	var fileURIs []string

	for _, header := range composer.Headers {
		raw, ok := bubbleMap[header.BubbleID]
		if !ok {
			continue
		}

		var bubble bubbleData
		if err := json.Unmarshal([]byte(raw), &bubble); err != nil {
			continue
		}

		lastBubbleType = bubble.Type
		lastHadTool = false

		var ts time.Time
		if bubble.CreatedAt != "" {
			ts = parseISO(bubble.CreatedAt)
		}

		if !ts.IsZero() {
			session.EntryTimestamps = append(session.EntryTimestamps, ts)
			if len(session.EntryTimestamps) > 500 {
				session.EntryTimestamps = session.EntryTimestamps[len(session.EntryTimestamps)-500:]
			}
		}

		// Extract workspace URI.
		if session.CWD == "" && len(bubble.WorkspaceUris) > 0 {
			if decoded, err := url.PathUnescape(strings.TrimPrefix(bubble.WorkspaceUris[0], "file://")); err == nil {
				session.CWD = decoded
			}
		}

		// Collect file URIs for CWD inference.
		if session.CWD == "" && len(fileURIs) < 10 {
			collectFileURIs(raw, &fileURIs)
		}

		// Token counting.
		session.InputTokens += bubble.TokenCount.InputTokens
		session.OutputTokens += bubble.TokenCount.OutputTokens

		switch bubble.Type {
		case 1: // user
			session.UserMessages++
			if bubble.Text != "" {
				recentMessages = append(recentMessages, model.ConversationMessage{
					Role: "user", Text: model.Truncate(bubble.Text, 300), Timestamp: ts,
				})
			}
		case 2: // assistant / tool result
			if bubble.ToolFormerData.Name != "" {
				toolName := normalizeToolName(bubble.ToolFormerData.Name)
				recentTools = append(recentTools, model.ToolCall{Name: toolName, Timestamp: ts})
				lastHadTool = true
			} else if bubble.Text != "" {
				session.AssistantMessages++
				recentMessages = append(recentMessages, model.ConversationMessage{
					Role: "assistant", Text: model.Truncate(bubble.Text, 300), Timestamp: ts,
				})
			}
		}

		if len(recentTools) > 40 {
			recentTools = recentTools[len(recentTools)-20:]
		}
		if len(recentMessages) > 20 {
			recentMessages = recentMessages[len(recentMessages)-10:]
		}
	}

	// Infer CWD from file URIs if not set.
	if session.CWD == "" && len(fileURIs) > 0 {
		session.CWD = inferWorkspace(fileURIs)
	}

	if len(recentTools) > 20 {
		recentTools = recentTools[len(recentTools)-20:]
	}
	session.RecentTools = recentTools

	if len(recentMessages) > 10 {
		recentMessages = recentMessages[len(recentMessages)-10:]
	}
	session.RecentMessages = recentMessages

	session.Status = determineBubbleStatus(lastBubbleType, lastHadTool)
	if session.Status == model.StatusExecutingTool && len(recentTools) > 0 {
		session.CurrentTool = recentTools[len(recentTools)-1].Name
	}
}

// fetchBubbles fetches all bubble values for a session in a single query.
// Returns a map of bubbleID → raw JSON value.
func fetchBubbles(db *sql.DB, sid string) map[string]string {
	rows, err := db.Query(
		"SELECT key, value FROM cursorDiskKV WHERE key LIKE ?",
		"bubbleId:"+sid+":%",
	)
	if err != nil {
		return nil
	}
	defer rows.Close()

	prefix := "bubbleId:" + sid + ":"
	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		bubbleID := strings.TrimPrefix(key, prefix)
		result[bubbleID] = value
	}
	return result
}

// fileURIRe matches file:///path URIs in JSON strings.
var fileURIRe = regexp.MustCompile(`file:///[^\s"\\]+`)

// collectFileURIs extracts file:// URIs from raw JSON for workspace inference.
func collectFileURIs(raw string, uris *[]string) {
	matches := fileURIRe.FindAllString(raw, 5)
	for _, m := range matches {
		decoded, err := url.PathUnescape(strings.TrimPrefix(m, "file://"))
		if err == nil {
			*uris = append(*uris, decoded)
		}
	}
}

// inferWorkspace finds a project root from collected file paths.
func inferWorkspace(paths []string) string {
	if len(paths) == 0 {
		return ""
	}

	dirSet := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		dirSet[filepath.Dir(p)] = struct{}{}
	}
	var dirs []string
	for d := range dirSet {
		dirs = append(dirs, d)
	}

	if len(dirs) == 1 {
		return findProjectRoot(dirs[0] + "/dummy")
	}

	parts := strings.Split(dirs[0], "/")
	for _, p := range dirs[1:] {
		pp := strings.Split(p, "/")
		n := len(parts)
		if len(pp) < n {
			n = len(pp)
		}
		for i := 0; i < n; i++ {
			if parts[i] != pp[i] {
				parts = parts[:i]
				break
			}
		}
		if len(parts) > n {
			parts = parts[:n]
		}
	}
	result := strings.Join(parts, "/")
	if len(strings.Split(result, "/")) < 4 {
		return ""
	}
	if root := findProjectRoot(result + "/dummy"); root != "" {
		return root
	}
	return result
}

// findProjectRoot walks up from a path looking for a project root.
func findProjectRoot(path string) string {
	dir := filepath.Dir(path)
	for {
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		for _, marker := range []string{".git", "package.json", "go.mod", "Cargo.toml", "pyproject.toml"} {
			if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
				return dir
			}
		}
		dir = parent
		if len(strings.Split(dir, "/")) < 4 {
			return ""
		}
	}
}

func determineBubbleStatus(lastType int, lastHadTool bool) model.SessionStatus {
	switch lastType {
	case 1:
		return model.StatusThinking
	case 2:
		if lastHadTool {
			return model.StatusExecutingTool
		}
		return model.StatusWaitingForUser
	}
	return model.StatusUnknown
}

func normalizeToolName(name string) string {
	switch name {
	case "Shell":
		return "Bash"
	case "read_file", "Read_file_v2":
		return "Read"
	case "edit_file", "Edit_file_v2":
		return "Edit"
	case "write_to_file", "Write_to_file_v2":
		return "Write"
	case "Glob", "glob", "Glob_file_search":
		return "Glob"
	case "Grep", "grep", "Grep_search":
		return "Grep"
	case "codebase_search", "Codebase_search":
		return "Grep"
	case "web_search":
		return "WebSearch"
	default:
		if len(name) > 0 {
			return strings.ToUpper(name[:1]) + name[1:]
		}
		return name
	}
}
