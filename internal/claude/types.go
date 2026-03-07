package claude

import "time"

// SessionStatus represents the current activity of a Claude Code session.
type SessionStatus int

const (
	StatusUnknown          SessionStatus = iota
	StatusWaitingForUser                 // Claude responded, awaiting human input
	StatusThinking                       // Claude is generating a response
	StatusExecutingTool                  // Claude invoked a tool, waiting for result
	StatusProcessingResult               // Tool result received, Claude is thinking
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

// Session holds all observable information about a Claude Code instance.
type Session struct {
	// Identity
	SessionID string
	JSONLPath string

	// Runtime
	CWD         string // working directory
	Version     string // claude version
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
}
