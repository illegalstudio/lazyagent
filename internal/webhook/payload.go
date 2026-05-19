package webhook

import "time"

// Payload is the JSON body sent on every webhook delivery.
type Payload struct {
	ID          string    `json:"id"`
	Event       string    `json:"event"`
	SessionID   string    `json:"session_id"`
	Agent       string    `json:"agent"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	ProjectPath string    `json:"project_path"`
	Timestamp   time.Time `json:"timestamp"`
	API         *APILinks `json:"api,omitempty"`
}

// APILinks point back to the local lazyagent API server for full details.
// Present only when the API server is running.
type APILinks struct {
	SessionURL string `json:"session_url"`
}
