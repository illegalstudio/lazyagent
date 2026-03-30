package pi

import "encoding/json"

// piEntry is the top-level JSONL line structure for pi sessions.
// Fields are flat because different entry types use different subsets.
// The Message field is kept as json.RawMessage so non-message entries
// skip the expensive nested struct allocation entirely.
type piEntry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	RawMsg    json.RawMessage `json:"message"`

	// session header fields
	Version int    `json:"version"`
	CWD     string `json:"cwd"`

	// model_change fields
	Provider string `json:"provider"`
	ModelID  string `json:"modelId"`

	// compaction fields
	Summary          string `json:"summary"`
	FirstKeptEntryID string `json:"firstKeptEntryId"`
	TokensBefore     int    `json:"tokensBefore"`

	// thinking_level_change fields
	ThinkingLevel string `json:"thinkingLevel"`

	// session_info fields
	Name string `json:"name"`

	// Parsed lazily from RawMsg only for "message" entries.
	message *piMessage
}

// parseMessage deserializes the RawMsg field into a piMessage.
func (e *piEntry) parseMessage() *piMessage {
	if e.message != nil {
		return e.message
	}
	if len(e.RawMsg) == 0 || e.RawMsg[0] == 'n' { // null
		return nil
	}
	var m piMessage
	if json.Unmarshal(e.RawMsg, &m) != nil {
		return nil
	}
	e.message = &m
	return e.message
}

// piMessage represents a message within a pi entry.
type piMessage struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	Provider   string          `json:"provider"`
	Model      string          `json:"model"`
	Usage      *piUsage        `json:"usage"`
	StopReason string          `json:"stopReason"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	IsError    bool            `json:"isError"`
	Timestamp  int64           `json:"timestamp"` // Unix ms
}

// piUsage holds token counts and cost from assistant messages.
type piUsage struct {
	Input       int     `json:"input"`
	Output      int     `json:"output"`
	CacheRead   int     `json:"cacheRead"`
	CacheWrite  int     `json:"cacheWrite"`
	TotalTokens int     `json:"totalTokens"`
	Cost        *piCost `json:"cost"`
}

// piCost holds per-category cost breakdowns.
type piCost struct {
	Input      float64 `json:"input"`
	Output     float64 `json:"output"`
	CacheRead  float64 `json:"cacheRead"`
	CacheWrite float64 `json:"cacheWrite"`
	Total      float64 `json:"total"`
}

// piContentBlock represents one block in a content array.
type piContentBlock struct {
	Type      string          `json:"type"` // "text", "toolCall", "thinking", "image"
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`        // toolCall id
	Name      string          `json:"name"`      // toolCall name
	Arguments json.RawMessage `json:"arguments"` // toolCall arguments
}
