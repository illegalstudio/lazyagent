package search

import "time"

var supportedAgents = []string{"claude", "codex", "pi", "amp"}

type sourceState struct {
	Agent   string
	ID      string
	Path    string
	MTimeNS int64
	Size    int64
}

type chunk struct {
	Source sourceState

	SessionID string
	CWD       string
	Name      string
	Role      string
	Timestamp time.Time
	Text      string
}

type hit struct {
	Agent     string
	SessionID string
	CWD       string
	Name      string
	Role      string
	Timestamp time.Time
	Text      string
	Rank      float64
}

type sessionResult struct {
	Agent     string
	SessionID string
	CWD       string
	Name      string
	LastHit   time.Time
	BestRank  float64
	Matches   int
	Snippets  []hit
}
