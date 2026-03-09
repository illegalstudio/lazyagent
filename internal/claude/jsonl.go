package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nahime0/lazyagent/internal/model"
)

// Raw JSONL entry structures

type jsonlEntry struct {
	Type        string        `json:"type"`
	SessionID   string        `json:"sessionId"`
	CWD         string        `json:"cwd"`
	Version     string        `json:"version"`
	GitBranch   string        `json:"gitBranch"`
	Timestamp   string        `json:"timestamp"`
	Message     *jsonlMessage `json:"message"`
	UUID        string        `json:"uuid"`
	IsSidechain bool          `json:"isSidechain"`
	CostUSD     float64       `json:"costUSD"`
}

type jsonlMessage struct {
	Role        string          `json:"role"`
	Model       string          `json:"model"`
	RawContent  json.RawMessage `json:"content"`
	Content     []jsonlContent  `json:"-"` // parsed from RawContent
	TextContent string          `json:"-"` // set when content is a plain string
	Usage       *jsonlUsage     `json:"usage"`
}

type jsonlUsage struct {
	InputTokens         int `json:"input_tokens"`
	OutputTokens        int `json:"output_tokens"`
	CacheCreationTokens int `json:"cache_creation_input_tokens"`
	CacheReadTokens     int `json:"cache_read_input_tokens"`
}

func (m *jsonlMessage) parseContent() {
	if len(m.RawContent) == 0 {
		return
	}
	// Content can be a plain string (user messages) or an array of objects.
	if m.RawContent[0] == '"' {
		json.Unmarshal(m.RawContent, &m.TextContent)
	} else if m.RawContent[0] == '[' {
		json.Unmarshal(m.RawContent, &m.Content)
	}
}

type jsonlContent struct {
	Type       string          `json:"type"`
	Text       string          `json:"text"`
	Name       string          `json:"name"`       // tool_use
	ToolUseID  string          `json:"tool_use_id"` // tool_result
	IsError    bool            `json:"is_error"`
	Input      json.RawMessage `json:"input"`
}

// ParseJSONL reads a JSONL file and returns a populated Session.
func ParseJSONL(path string) (*model.Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	// The filename (without extension) is always the authoritative session ID.
	// After compaction, the new JSONL starts with entries from the old session,
	// so reading sessionId from entries would give the wrong (old) ID.
	filenameID := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	session := &model.Session{
		JSONLPath:    path,
		SessionID:    filenameID,
		LastActivity: info.ModTime(),
		Agent:        "claude",
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	// Single-pass: extract metadata, count messages, collect recent tools/messages,
	// and track the last meaningful entry for status determination.
	var recentTools []model.ToolCall
	var recentMessages []model.ConversationMessage
	var lastMeaningful *jsonlEntry
	var prevTimestamp time.Time
	var entryTimestamps []time.Time
	parsed := false

	for scanner.Scan() {
		var e jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		parsed = true
		if e.Message != nil {
			e.Message.parseContent()
		}

		ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)

		if e.IsSidechain {
			session.IsSidechain = true
		}

		if e.Type == "summary" {
			if !prevTimestamp.IsZero() {
				session.LastSummaryAt = prevTimestamp
			} else {
				session.LastSummaryAt = session.LastActivity
			}
		}

		if !ts.IsZero() {
			prevTimestamp = ts
			entryTimestamps = append(entryTimestamps, ts)
			if len(entryTimestamps) > 500 {
				trimmed := make([]time.Time, 500)
				copy(trimmed, entryTimestamps[len(entryTimestamps)-500:])
				entryTimestamps = trimmed
			}
		}

		// Extract metadata from whichever entry provides it first.
		if session.CWD == "" && e.CWD != "" {
			session.CWD = e.CWD
		}
		if session.Version == "" && e.Version != "" {
			session.Version = e.Version
		}
		if session.GitBranch == "" && e.GitBranch != "" {
			session.GitBranch = e.GitBranch
		}

		// Accumulate cost and tokens
		session.CostUSD += e.CostUSD
		if e.Message != nil && e.Message.Usage != nil {
			u := e.Message.Usage
			session.InputTokens += u.InputTokens
			session.OutputTokens += u.OutputTokens
			session.CacheCreationTokens += u.CacheCreationTokens
			session.CacheReadTokens += u.CacheReadTokens
		}

		switch e.Type {
		case "user":
			lastMeaningful = &e
			if e.Message != nil && !isToolResult(e.Message) {
				session.UserMessages++
				if text := firstText(e.Message); text != "" {
					recentMessages = append(recentMessages, model.ConversationMessage{
						Role: "user", Text: truncate(text, 300), Timestamp: ts,
					})
					if len(recentMessages) > 20 {
						recentMessages = recentMessages[len(recentMessages)-10:]
					}
				}
			}
		case "assistant":
			lastMeaningful = &e
			if e.Message != nil {
				session.AssistantMessages++
				if e.Message.Model != "" {
					session.Model = e.Message.Model
				}
				if text := firstText(e.Message); text != "" {
					recentMessages = append(recentMessages, model.ConversationMessage{
						Role: "assistant", Text: truncate(text, 300), Timestamp: ts,
					})
					if len(recentMessages) > 20 {
						recentMessages = recentMessages[len(recentMessages)-10:]
					}
				}
				for _, c := range e.Message.Content {
					if c.Type == "tool_use" {
						recentTools = append(recentTools, model.ToolCall{Name: c.Name, Timestamp: ts})
						if len(recentTools) > 40 {
							recentTools = recentTools[len(recentTools)-20:]
						}
						if c.Name == "Write" || c.Name == "Edit" || c.Name == "NotebookEdit" {
							if fp := extractFilePath(c.Input); fp != "" {
								session.LastFileWrite = fp
								session.LastFileWriteAt = ts
							}
						}
					}
				}
			}
		}
	}

	if !parsed {
		return session, nil
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages
	session.EntryTimestamps = entryTimestamps

	if len(recentTools) > 20 {
		recentTools = recentTools[len(recentTools)-20:]
	}
	session.RecentTools = recentTools

	if len(recentMessages) > 10 {
		recentMessages = recentMessages[len(recentMessages)-10:]
	}
	session.RecentMessages = recentMessages

	// Determine status from the last meaningful entry
	session.Status = determineStatus(lastMeaningful)
	if lastMeaningful != nil {
		if ts, err := time.Parse(time.RFC3339Nano, lastMeaningful.Timestamp); err == nil {
			session.LastActivity = ts
		}
		if session.Status == model.StatusExecutingTool && lastMeaningful.Message != nil {
			for _, c := range lastMeaningful.Message.Content {
				if c.Type == "tool_use" {
					session.CurrentTool = c.Name
				}
			}
		}
	}

	return session, nil
}

func isToolResult(m *jsonlMessage) bool {
	for _, c := range m.Content {
		if c.Type == "tool_result" {
			return true
		}
	}
	return false
}

func firstText(m *jsonlMessage) string {
	if m.TextContent != "" {
		return m.TextContent
	}
	for _, c := range m.Content {
		if c.Type == "text" && c.Text != "" {
			return c.Text
		}
	}
	return ""
}

// extractFilePath extracts the "file_path" value from a tool_use input
// without unmarshaling the entire JSON into a map.
func extractFilePath(raw json.RawMessage) string {
	var obj struct {
		FilePath string `json:"file_path"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.FilePath
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func determineStatus(e *jsonlEntry) model.SessionStatus {
	if e == nil {
		return model.StatusUnknown
	}
	switch e.Type {
	case "assistant":
		if e.Message == nil {
			return model.StatusUnknown
		}
		for _, c := range e.Message.Content {
			if c.Type == "tool_use" {
				return model.StatusExecutingTool
			}
		}
		// Assistant responded with text — waiting for user
		return model.StatusWaitingForUser
	case "user":
		if e.Message == nil {
			return model.StatusUnknown
		}
		if isToolResult(e.Message) {
			// Tool result was written — Claude is now thinking about it
			return model.StatusProcessingResult
		}
		// Human sent a message — Claude is thinking
		return model.StatusThinking
	}
	return model.StatusUnknown
}
