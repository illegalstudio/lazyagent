package pi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nahime0/lazyagent/internal/claude"
)

// ParsePiJSONL reads a pi session JSONL file and returns a populated Session.
func ParsePiJSONL(path string) (*claude.Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	session := &claude.Session{
		JSONLPath:    path,
		SessionID:    strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		LastActivity: info.ModTime(),
		Agent:        "pi",
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	var recentTools []claude.ToolCall
	var recentMessages []claude.ConversationMessage
	var lastMessageEntry *piEntry
	var entryTimestamps []time.Time
	parsed := false

	for scanner.Scan() {
		line := scanner.Bytes()
		var e piEntry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		parsed = true

		ts, _ := time.Parse(time.RFC3339Nano, e.Timestamp)

		switch e.Type {
		case "session":
			if session.CWD == "" && e.CWD != "" {
				session.CWD = e.CWD
			}

		case "model_change":
			if e.ModelID != "" {
				session.Model = e.ModelID
			}

		case "compaction":
			if !ts.IsZero() {
				session.LastSummaryAt = ts
			}

		case "message":
			if e.Message == nil {
				continue
			}

			if !ts.IsZero() {
				entryTimestamps = append(entryTimestamps, ts)
				if len(entryTimestamps) > 500 {
					trimmed := make([]time.Time, 500)
					copy(trimmed, entryTimestamps[len(entryTimestamps)-500:])
					entryTimestamps = trimmed
				}
			}

			switch e.Message.Role {
			case "user":
				session.UserMessages++
				lastMessageEntry = copyEntry(&e)
				if text := firstPiText(e.Message); text != "" {
					recentMessages = append(recentMessages, claude.ConversationMessage{
						Role: "user", Text: truncate(text, 300), Timestamp: ts,
					})
					if len(recentMessages) > 20 {
						recentMessages = recentMessages[len(recentMessages)-10:]
					}
				}

			case "assistant":
				session.AssistantMessages++
				lastMessageEntry = copyEntry(&e)
				if e.Message.Model != "" {
					session.Model = e.Message.Model
				}
				if e.Message.Usage != nil {
					u := e.Message.Usage
					session.InputTokens += u.Input
					session.OutputTokens += u.Output
					session.CacheReadTokens += u.CacheRead
					session.CacheCreationTokens += u.CacheWrite
					if u.Cost != nil {
						session.CostUSD += u.Cost.Total
					}
				}
				blocks := parsePiContent(e.Message.Content)
				for _, b := range blocks {
					if b.Type == "toolCall" {
						recentTools = append(recentTools, claude.ToolCall{
							Name: normalizePiToolName(b.Name), Timestamp: ts,
						})
						if len(recentTools) > 40 {
							recentTools = recentTools[len(recentTools)-20:]
						}
						if isWriteTool(b.Name) {
							if fp := extractPiFilePath(b.Arguments); fp != "" {
								session.LastFileWrite = fp
								session.LastFileWriteAt = ts
							}
						}
					}
				}
				if text := firstPiTextFromBlocks(blocks); text != "" {
					recentMessages = append(recentMessages, claude.ConversationMessage{
						Role: "assistant", Text: truncate(text, 300), Timestamp: ts,
					})
					if len(recentMessages) > 20 {
						recentMessages = recentMessages[len(recentMessages)-10:]
					}
				}

			case "toolResult":
				lastMessageEntry = copyEntry(&e)
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

	// Determine status from the last message entry.
	session.Status = determinePiStatus(lastMessageEntry)
	if lastMessageEntry != nil {
		entryTs, _ := time.Parse(time.RFC3339Nano, lastMessageEntry.Timestamp)
		if !entryTs.IsZero() {
			session.LastActivity = entryTs
		}
		if session.Status == claude.StatusExecutingTool && lastMessageEntry.Message != nil {
			blocks := parsePiContent(lastMessageEntry.Message.Content)
			for _, b := range blocks {
				if b.Type == "toolCall" {
					session.CurrentTool = normalizePiToolName(b.Name)
				}
			}
		}
	}

	return session, nil
}

// copyEntry returns a shallow copy of a piEntry (so we can store a pointer
// to the last entry without it being overwritten on the next loop iteration).
func copyEntry(e *piEntry) *piEntry {
	cp := *e
	return &cp
}

// parsePiContent deserializes pi content which can be a string or array of blocks.
func parsePiContent(raw json.RawMessage) []piContentBlock {
	if len(raw) == 0 {
		return nil
	}
	if raw[0] == '"' {
		return nil
	}
	var blocks []piContentBlock
	_ = json.Unmarshal(raw, &blocks)
	return blocks
}

// firstPiText extracts the first text from a pi message's content.
func firstPiText(m *piMessage) string {
	if len(m.Content) == 0 {
		return ""
	}
	if m.Content[0] == '"' {
		var s string
		json.Unmarshal(m.Content, &s)
		return s
	}
	blocks := parsePiContent(m.Content)
	return firstPiTextFromBlocks(blocks)
}

func firstPiTextFromBlocks(blocks []piContentBlock) string {
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			return b.Text
		}
	}
	return ""
}

// normalizePiToolName maps pi snake_case tool names to PascalCase names
// matching the activity tracker expectations.
func normalizePiToolName(name string) string {
	switch name {
	case "bash":
		return "Bash"
	case "read":
		return "Read"
	case "write":
		return "Write"
	case "edit":
		return "Edit"
	case "web_search":
		return "WebSearch"
	case "find":
		return "Glob"
	case "process":
		return "Bash"
	case "subagent":
		return "Agent"
	case "lsp":
		return "Grep" // closest activity: searching
	default:
		if len(name) > 0 {
			return strings.ToUpper(name[:1]) + name[1:]
		}
		return name
	}
}

// isWriteTool returns true for tools that write/edit files.
func isWriteTool(name string) bool {
	return name == "write" || name == "edit"
}

// extractPiFilePath extracts the "path" field from tool arguments.
func extractPiFilePath(raw json.RawMessage) string {
	var obj struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Path
	}
	return ""
}

// determinePiStatus infers session status from the last message entry.
func determinePiStatus(e *piEntry) claude.SessionStatus {
	if e == nil || e.Message == nil {
		return claude.StatusUnknown
	}
	switch e.Message.Role {
	case "assistant":
		blocks := parsePiContent(e.Message.Content)
		for _, b := range blocks {
			if b.Type == "toolCall" {
				return claude.StatusExecutingTool
			}
		}
		return claude.StatusWaitingForUser
	case "user":
		return claude.StatusThinking
	case "toolResult":
		return claude.StatusProcessingResult
	}
	return claude.StatusUnknown
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
