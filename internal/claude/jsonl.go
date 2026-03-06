package claude

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
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
}

type jsonlMessage struct {
	Role    string        `json:"role"`
	Model   string        `json:"model"`
	Content []jsonlContent `json:"content"`
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
func ParseJSONL(path string) (*Session, error) {
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
	session := &Session{
		JSONLPath:    path,
		SessionID:    filenameID,
		LastActivity: info.ModTime(),
	}

	var entries []jsonlEntry
	scanner := bufio.NewScanner(f)
	// Increase buffer for large lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	for scanner.Scan() {
		var e jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		entries = append(entries, e)
	}

	if len(entries) == 0 {
		return session, nil
	}

	// Extract session metadata from the first user/assistant entry.
	// isSidechain is set if ANY entry marks this as a sidechain.
	// summary entries (type="summary") mark context compaction events.
	for _, e := range entries {
		if e.IsSidechain {
			session.IsSidechain = true
		}
		if e.Type == "summary" {
			// Summary entries have no timestamp; use the file mod time as proxy.
			session.LastSummaryAt = session.LastActivity
		}
		if e.Type == "user" || e.Type == "assistant" {
			if session.CWD == "" && e.CWD != "" {
				session.CWD = e.CWD
			}
			if session.Version == "" && e.Version != "" {
				session.Version = e.Version
			}
			if session.GitBranch == "" && e.GitBranch != "" {
				session.GitBranch = e.GitBranch
			}
		}
	}

	// Count messages and collect tool calls, conversation messages, and last file write
	var recentTools []ToolCall
	var recentMessages []ConversationMessage
	for _, e := range entries {
		ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)
		switch e.Type {
		case "user":
			if e.Message != nil && !isToolResult(e.Message.Content) {
				session.UserMessages++
				if text := firstTextBlock(e.Message.Content); text != "" {
					recentMessages = append(recentMessages, ConversationMessage{
						Role:      "user",
						Text:      truncate(text, 300),
						Timestamp: ts,
					})
				}
			}
		case "assistant":
			if e.Message != nil {
				session.AssistantMessages++
				if e.Message.Model != "" {
					session.Model = e.Message.Model
				}
				if text := firstTextBlock(e.Message.Content); text != "" {
					recentMessages = append(recentMessages, ConversationMessage{
						Role:      "assistant",
						Text:      truncate(text, 300),
						Timestamp: ts,
					})
				}
				for _, c := range e.Message.Content {
					if c.Type == "tool_use" {
						recentTools = append(recentTools, ToolCall{Name: c.Name, Timestamp: ts})
						if c.Name == "Write" || c.Name == "Edit" || c.Name == "NotebookEdit" {
							var input map[string]any
							if err := json.Unmarshal(c.Input, &input); err == nil {
								if fp, ok := input["file_path"].(string); ok && fp != "" {
									session.LastFileWrite = fp
									session.LastFileWriteAt = ts
								}
							}
						}
					}
				}
			}
		}
	}
	session.TotalMessages = session.UserMessages + session.AssistantMessages

	// Keep last 20 tool calls
	if len(recentTools) > 20 {
		recentTools = recentTools[len(recentTools)-20:]
	}
	session.RecentTools = recentTools

	// Keep last 10 conversation messages
	if len(recentMessages) > 10 {
		recentMessages = recentMessages[len(recentMessages)-10:]
	}
	session.RecentMessages = recentMessages

	// Determine status from the last meaningful entry
	last := lastMeaningful(entries)
	session.Status = determineStatus(last)
	if last != nil {
		if ts, err := time.Parse(time.RFC3339Nano, last.Timestamp); err == nil {
			session.LastActivity = ts
		}
		if session.Status == StatusExecutingTool && last.Message != nil {
			for _, c := range last.Message.Content {
				if c.Type == "tool_use" {
					session.CurrentTool = c.Name
				}
			}
		}
	}

	return session, nil
}

func isToolResult(content []jsonlContent) bool {
	for _, c := range content {
		if c.Type == "tool_result" {
			return true
		}
	}
	return false
}

func firstTextBlock(content []jsonlContent) string {
	for _, c := range content {
		if c.Type == "text" && c.Text != "" {
			return c.Text
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func lastMeaningful(entries []jsonlEntry) *jsonlEntry {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type == "user" || e.Type == "assistant" {
			return &entries[i]
		}
	}
	return nil
}

func determineStatus(e *jsonlEntry) SessionStatus {
	if e == nil {
		return StatusUnknown
	}
	switch e.Type {
	case "assistant":
		if e.Message == nil {
			return StatusUnknown
		}
		for _, c := range e.Message.Content {
			if c.Type == "tool_use" {
				return StatusExecutingTool
			}
		}
		// Assistant responded with text — waiting for user
		return StatusWaitingForUser
	case "user":
		if e.Message == nil {
			return StatusUnknown
		}
		if isToolResult(e.Message.Content) {
			// Tool result was written — Claude is now thinking about it
			return StatusProcessingResult
		}
		// Human sent a message — Claude is thinking
		return StatusThinking
	}
	return StatusUnknown
}
