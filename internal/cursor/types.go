package cursor

// bubbleData is the JSON structure stored in state.vscdb cursorDiskKV
// under bubbleId:<sessionId>:<bubbleId> keys.
type bubbleData struct {
	Type           int          `json:"type"` // 1 = user, 2 = assistant/tool
	Text           string       `json:"text"`
	CreatedAt      string       `json:"createdAt"` // ISO 8601
	WorkspaceUris  []string     `json:"workspaceUris"`
	TokenCount     bubbleTokens `json:"tokenCount"`
	ToolFormerData toolFormer   `json:"toolFormerData"`
}

type bubbleTokens struct {
	InputTokens  int `json:"inputTokens"`
	OutputTokens int `json:"outputTokens"`
}

type toolFormer struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}
