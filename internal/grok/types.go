package grok

import "encoding/json"

// grokSummary mirrors the subset of summary.json this package reads.
// Timestamps are RFC3339 UTC strings with microseconds and a trailing Z.
type grokSummary struct {
	Info struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
	SessionSummary    string `json:"session_summary"`
	GeneratedTitle    string `json:"generated_title"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	LastActiveAt      string `json:"last_active_at"`
	NumChatMessages   int    `json:"num_chat_messages"`
	CurrentModelID    string `json:"current_model_id"`
	GitRootDir        string `json:"git_root_dir"`
	HeadBranch        string `json:"head_branch"`
	ChatFormatVersion int    `json:"chat_format_version"`
	// SessionKind is present ONLY on subagent sessions ("subagent").
	SessionKind string `json:"session_kind"`
}

// grokChatEntry is one line of chat_history.jsonl. content is a plain string
// for system/assistant/tool_result entries and an array of blocks for user
// entries — kept as RawMessage so the caller picks the right decode path.
type grokChatEntry struct {
	Type       string          `json:"type"` // system | user | assistant | tool_result
	Content    json.RawMessage `json:"content"`
	Reasoning  string          `json:"reasoning"`
	ToolCalls  []grokToolCall  `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
	ModelID    string          `json:"model_id"`
}

// grokToolCall is one entry of an assistant message's tool_calls[].
type grokToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // a JSON-encoded string
}

// grokContentBlock is one block of a user entry's content array.
type grokContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// grokSignals mirrors the subset of signals.json this package reads.
// signals.json is absent in ~11% of sessions (very short / aborted ones).
type grokSignals struct {
	UserMessageCount      int `json:"userMessageCount"`
	AssistantMessageCount int `json:"assistantMessageCount"`
	ContextTokensUsed     int `json:"contextTokensUsed"`
}

// grokUpdate is the minimal shape of an updates.jsonl line. timestamp is
// Unix epoch seconds (fractional).
type grokUpdate struct {
	Timestamp float64 `json:"timestamp"`
}
