package pi

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nahime0/lazyagent/internal/model"
)

// ParsePiJSONL reads a pi session JSONL file and returns a populated Session
// and the byte offset consumed (for incremental parsing on the next call).
func ParsePiJSONL(path string) (*model.Session, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}

	session := &model.Session{
		JSONLPath:    path,
		SessionID:    strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		LastActivity: info.ModTime(),
		Agent:        "pi",
	}

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	var recentTools []model.ToolCall
	var recentMessages []model.ConversationMessage
	var lastMessageEntry *piEntry
	var entryTimestamps []time.Time
	var bytesConsumed int64
	parsed := false

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesConsumed += int64(len(line)) + 1 // +1 for newline
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

		case "session_info":
			if e.Name != "" {
				session.Name = e.Name
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
					recentMessages = append(recentMessages, model.ConversationMessage{
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
						recentTools = append(recentTools, model.ToolCall{
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
					recentMessages = append(recentMessages, model.ConversationMessage{
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
		return session, 0, nil
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
		if session.Status == model.StatusExecutingTool && lastMessageEntry.Message != nil {
			blocks := parsePiContent(lastMessageEntry.Message.Content)
			for _, b := range blocks {
				if b.Type == "toolCall" {
					session.CurrentTool = normalizePiToolName(b.Name)
				}
			}
		}
	}

	// Cap to file size: the last line may lack a trailing newline during concurrent writes.
	// Use a fresh Stat (not the one from before scanning) because the file
	// may have grown during scanning.
	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}

	return session, bytesConsumed, nil
}

// ParsePiJSONLIncremental reads only the tail of a pi JSONL file starting at
// the given byte offset, merging new entries into the provided base session.
func ParsePiJSONLIncremental(path string, offset int64, base *model.Session) (*model.Session, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	if _, err := f.Seek(offset, 0); err != nil {
		return nil, 0, err
	}

	session := base
	scanner := bufio.NewScanner(f)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 4*1024*1024)

	recentTools := session.RecentTools
	recentMessages := session.RecentMessages
	entryTimestamps := session.EntryTimestamps
	var lastMessageEntry *piEntry
	bytesConsumed := offset
	parsed := false

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesConsumed += int64(len(line)) + 1
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
		case "session_info":
			if e.Name != "" {
				session.Name = e.Name
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
					recentMessages = append(recentMessages, model.ConversationMessage{
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
						recentTools = append(recentTools, model.ToolCall{
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
					recentMessages = append(recentMessages, model.ConversationMessage{
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
		return session, offset, nil
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

	// Only update status if we saw a message entry in the new tail.
	// Otherwise keep the status inherited from the base session.
	if lastMessageEntry != nil {
		session.Status = determinePiStatus(lastMessageEntry)
		entryTs, _ := time.Parse(time.RFC3339Nano, lastMessageEntry.Timestamp)
		if !entryTs.IsZero() {
			session.LastActivity = entryTs
		}
		if session.Status == model.StatusExecutingTool && lastMessageEntry.Message != nil {
			blocks := parsePiContent(lastMessageEntry.Message.Content)
			for _, b := range blocks {
				if b.Type == "toolCall" {
					session.CurrentTool = normalizePiToolName(b.Name)
				}
			}
		}
	}

	// Cap to file size to handle last line without trailing newline.
	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}

	return session, bytesConsumed, nil
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
func determinePiStatus(e *piEntry) model.SessionStatus {
	if e == nil || e.Message == nil {
		return model.StatusUnknown
	}
	switch e.Message.Role {
	case "assistant":
		blocks := parsePiContent(e.Message.Content)
		for _, b := range blocks {
			if b.Type == "toolCall" {
				return model.StatusExecutingTool
			}
		}
		return model.StatusWaitingForUser
	case "user":
		return model.StatusThinking
	case "toolResult":
		return model.StatusProcessingResult
	}
	return model.StatusUnknown
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
