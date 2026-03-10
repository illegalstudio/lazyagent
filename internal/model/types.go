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
	Role      string    // "user" or "assistant"
	Text      string    // first text block, truncated to 300 chars
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
	Agent string // "claude" or "pi" — which coding agent produced this session
	Name  string // session display name (from pi session_info or custom)

	// Desktop metadata (non-nil if session was started via Claude Desktop)
	Desktop *DesktopMeta
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
	session *Session
}

// NewSessionCache creates an empty session cache.
func NewSessionCache() *SessionCache {
	return &SessionCache{entries: make(map[string]sessionCacheEntry)}
}

// Get returns a cached session if the file mtime hasn't changed.
// Returns (session, mtime) on hit, (nil, mtime) on miss.
// The returned mtime should be passed to Put to avoid TOCTOU races.
func (c *SessionCache) Get(path string) (*Session, time.Time) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, time.Time{}
	}
	mtime := info.ModTime()
	c.mu.Lock()
	defer c.mu.Unlock()
	if e, ok := c.entries[path]; ok && e.mtime.Equal(mtime) {
		return e.session, mtime
	}
	return nil, mtime
}

// Put stores a session in the cache with the given mtime (from a prior Get call).
func (c *SessionCache) Put(path string, mtime time.Time, s *Session) {
	c.mu.Lock()
	c.entries[path] = sessionCacheEntry{mtime: mtime, session: s}
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
