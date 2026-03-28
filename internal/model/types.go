package model

import (
	"os"
	"sync"
	"time"
)

// SessionStatus represents the current activity of a coding agent session.
type SessionStatus int

const (
	StatusUnknown          SessionStatus = iota
	StatusWaitingForUser                 // Agent responded, awaiting human input
	StatusThinking                       // Agent is generating a response
	StatusExecutingTool                  // Agent invoked a tool, waiting for result
	StatusProcessingResult               // Tool result received, agent is thinking
	StatusIdle                           // Session file exists but process not running
)

func (s SessionStatus) String() string {
	switch s {
	case StatusWaitingForUser:
		return "waiting"
	case StatusThinking:
		return "thinking"
	case StatusExecutingTool:
		return "tool"
	case StatusProcessingResult:
		return "processing"
	case StatusIdle:
		return "idle"
	default:
		return "unknown"
	}
}

// ToolCall holds info about a single tool invocation.
type ToolCall struct {
	Name      string
	Timestamp time.Time
}

// ConversationMessage holds a single human-readable message from the conversation.
type ConversationMessage struct {
	Role      string // "user" or "assistant"
	Text      string // first text block, truncated to 300 chars
	Timestamp time.Time
}

// Session holds all observable information about a coding agent instance.
type Session struct {
	// Identity
	SessionID string
	JSONLPath string

	// Runtime
	CWD         string // working directory
	Version     string // agent version
	Model       string // model being used
	GitBranch   string
	IsSidechain bool // true = sub-agent spawned by another session

	// Git / worktree
	IsWorktree bool
	MainRepo   string // main repo path if worktree

	// Status
	Status        SessionStatus
	CurrentTool   string    // name of tool currently executing, if any
	LastActivity  time.Time // timestamp of last JSONL entry
	LastSummaryAt time.Time // timestamp of last compaction summary entry
	TotalMessages int
	RecentTools   []ToolCall // last N tool calls

	// Conversation preview
	RecentMessages []ConversationMessage // last 10 human-readable messages

	// Last file written/edited
	LastFileWrite   string    // absolute path of most recent Write/Edit/NotebookEdit
	LastFileWriteAt time.Time // timestamp of that tool call

	// Counts
	UserMessages      int
	AssistantMessages int

	// Activity timeline (for sparkline)
	EntryTimestamps []time.Time

	// Cost tracking
	CostUSD             float64 // cumulative cost from JSONL entries
	InputTokens         int
	OutputTokens        int
	CacheCreationTokens int
	CacheReadTokens     int

	// Agent identity
	Agent string // e.g. "claude", "pi", "codex" — which coding agent produced this session
	Name  string // session display name (from pi session_info or custom)

	// Desktop metadata (non-nil if session was started via Claude Desktop)
	Desktop *DesktopMeta
}

// Clone returns a deep copy of the Session suitable for use as a base
// in incremental parsing. Slice fields are copied so the original is not mutated.
func (s *Session) Clone() *Session {
	cp := *s
	if len(s.RecentTools) > 0 {
		cp.RecentTools = make([]ToolCall, len(s.RecentTools))
		copy(cp.RecentTools, s.RecentTools)
	}
	if len(s.RecentMessages) > 0 {
		cp.RecentMessages = make([]ConversationMessage, len(s.RecentMessages))
		copy(cp.RecentMessages, s.RecentMessages)
	}
	if len(s.EntryTimestamps) > 0 {
		cp.EntryTimestamps = make([]time.Time, len(s.EntryTimestamps))
		copy(cp.EntryTimestamps, s.EntryTimestamps)
	}
	return &cp
}

// DesktopMeta holds metadata from Claude Desktop's session JSON files.
type DesktopMeta struct {
	Title          string
	DesktopID      string // Desktop's own session ID (distinct from cliSessionId)
	PermissionMode string
	IsArchived     bool
	CreatedAt      time.Time
}

// SessionCache caches parsed JSONL sessions keyed by file path.
// On subsequent calls, only files whose mtime changed are re-parsed.
// Shared by all agent providers (Claude, pi, etc.).
type SessionCache struct {
	mu      sync.Mutex
	entries map[string]sessionCacheEntry
}

type sessionCacheEntry struct {
	mtime   time.Time
	size    int64 // byte offset of last fully parsed line
	session *Session
}

// NewSessionCache creates an empty session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{entries: make(map[string]sessionCacheEntry)}
}

// GetIncremental returns the cached session and byte offset for incremental parsing.
// If the file is unchanged, returns (session, 0, mtime) — a full cache hit.
// If the file has changed and a previous entry exists, returns (session, offset, mtime)
// where session is a Clone of the previous state and offset is the byte position to resume from.
// If no previous entry exists, returns (nil, 0, mtime) — a full miss.
func (c *SessionCache) GetIncremental(path string) (*Session, int64, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, time.Time{}
	}
	mtime := info.ModTime()
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[path]; ok {
		if e.mtime.Equal(mtime) {
			// Full cache hit — file unchanged.
			return e.session, 0, mtime
		}
		// File didn't grow (same size rewrite or shrunk) or previous parse
		// consumed nothing (empty file) — force full re-parse.
		if info.Size() <= e.size || e.size == 0 {
			return nil, 0, mtime
		}
		// File grew — return clone + offset for incremental parse.
		return e.session.Clone(), e.size, mtime
	}
	return nil, 0, mtime
}

// Put stores a session in the cache with the given mtime and byte offset.
func (c *SessionCache) Put(path string, mtime time.Time, size int64, s *Session) {
	c.mu.Lock()
	c.entries[path] = sessionCacheEntry{mtime: mtime, size: size, session: s}
	c.mu.Unlock()
}

// Prune removes cache entries for files no longer present in the seen set.
func (c *SessionCache) Prune(seen map[string]struct{}) {
	c.mu.Lock()
	for k := range c.entries {
		if _, ok := seen[k]; !ok {
			delete(c.entries, k)
		}
	}
	c.mu.Unlock()
}

// Truncate returns the first n runes of s, or s unchanged if shorter.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
