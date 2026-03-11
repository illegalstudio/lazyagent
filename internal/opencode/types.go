package opencode

import "encoding/json"

// dbSession maps to a row in the OpenCode `session` table.
type dbSession struct {
	ID             string
	ProjectID      string
	ParentID       *string
	Directory      string
	Title          string
	Version        string
	TimeCreated    int64
	TimeUpdated    int64
	TimeCompacting *int64
	TimeArchived   *int64
}

// messageData is the JSON structure stored in the `message.data` column.
type messageData struct {
	Role       string      `json:"role"`
	ModelID    string      `json:"modelID"`
	ProviderID string      `json:"providerID"`
	Agent      string      `json:"agent"`
	Cost       float64     `json:"cost"`
	Tokens     *tokensData `json:"tokens"`
	Finish     string      `json:"finish"`
	Time       *timeData   `json:"time"`
}

type tokensData struct {
	Input     int        `json:"input"`
	Output    int        `json:"output"`
	Reasoning int        `json:"reasoning"`
	Cache     cacheData  `json:"cache"`
}

type cacheData struct {
	Read  int `json:"read"`
	Write int `json:"write"`
}

type timeData struct {
	Created   int64 `json:"created"`
	Completed int64 `json:"completed"`
}

// partData is the JSON structure stored in the `part.data` column.
type partData struct {
	Type   string          `json:"type"`
	Text   string          `json:"text"`
	Tool   string          `json:"tool"`
	CallID string          `json:"callID"`
	State  json.RawMessage `json:"state"`
}

// partState holds the parsed tool state from part.data.state.
type partState struct {
	Status string          `json:"status"`
	Input  json.RawMessage `json:"input"`
	Output string          `json:"output"`
}
