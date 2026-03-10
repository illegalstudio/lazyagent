package model

import "time"

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
