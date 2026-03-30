package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// Raw JSONL entry structures

// jsonlEntry is unmarshaled once per line. The Message field is kept as
// json.RawMessage so non-user/assistant entries skip the expensive nested
// struct allocation entirely.
type jsonlEntry struct {
	Type        string          `json:"type"`
	Subtype     string          `json:"subtype"`
	URL         string          `json:"url"`
	SessionID   string          `json:"sessionId"`
	CWD         string          `json:"cwd"`
	Version     string          `json:"version"`
	GitBranch   string          `json:"gitBranch"`
	Timestamp   string          `json:"timestamp"`
	RawMessage  json.RawMessage `json:"message"`
	UUID        string          `json:"uuid"`
	IsSidechain bool            `json:"isSidechain"`
	CostUSD     float64         `json:"costUSD"`

	// Parsed lazily from RawMessage only for user/assistant entries.
	message *jsonlMessage
}

// parseMessage deserializes the RawMessage field into a jsonlMessage.
// Calling it is cheap if RawMessage is nil/empty.
func (e *jsonlEntry) parseMessage() *jsonlMessage {
	if e.message != nil {
		return e.message
	}
	if len(e.RawMessage) == 0 || e.RawMessage[0] == 'n' { // null
		return nil
	}
	var m jsonlMessage
	if json.Unmarshal(e.RawMessage, &m) != nil {
		return nil
	}
	m.parseContent()
	e.message = &m
	return e.message
}

type jsonlMessage struct {
	Role        string          `json:"role"`
	Model       string          `json:"model"`
	RawContent  json.RawMessage `json:"content"`
	Content     []jsonlContent  `json:"-"` // parsed from RawContent (without Input)
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

// jsonlContent is the lightweight content struct — Input is omitted to avoid
// deserializing potentially large tool payloads (Write/Edit file contents).
type jsonlContent struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Name      string `json:"name"`       // tool_use
	ToolUseID string `json:"tool_use_id"` // tool_result
	IsError   bool   `json:"is_error"`
}

// scanEntries is the shared scanning loop used by both ParseJSONL and ParseJSONLIncremental.
// Each line is unmarshaled once into jsonlEntry (with Message as json.RawMessage).
// The heavy message parsing is deferred and only done for user/assistant entries.
func scanEntries(scanner *bufio.Scanner, session *model.Session, initialOffset int64,
	recentTools []model.ToolCall, recentMessages []model.ConversationMessage,
	entryTimestamps []time.Time,
) (int64, bool, *jsonlEntry) {
	var lastMeaningful *jsonlEntry
	var prevTimestamp time.Time
	bytesConsumed := initialOffset
	parsed := false

	for scanner.Scan() {
		lineBytes := scanner.Bytes()
		bytesConsumed += int64(len(lineBytes)) + 1 // +1 for newline

		var e jsonlEntry
		if err := json.Unmarshal(lineBytes, &e); err != nil {
			continue
		}
		parsed = true

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

		if e.Type == "system" && e.Subtype == "bridge_status" && e.URL != "" {
			session.RemoteURL = e.URL
		}

		if !ts.IsZero() {
			prevTimestamp = ts
			entryTimestamps = append(entryTimestamps, ts)
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

		// Accumulate cost.
		session.CostUSD += e.CostUSD

		// Only parse the heavy Message for user/assistant entries.
		if e.Type != "user" && e.Type != "assistant" {
			continue
		}

		msg := e.parseMessage()
		if msg == nil {
			continue
		}

		// Accumulate tokens.
		if msg.Usage != nil {
			u := msg.Usage
			session.InputTokens += u.InputTokens
			session.OutputTokens += u.OutputTokens
			session.CacheCreationTokens += u.CacheCreationTokens
			session.CacheReadTokens += u.CacheReadTokens
		}

		switch e.Type {
		case "user":
			lastMeaningful = copyEntry(&e)
			if !isToolResult(msg) {
				session.UserMessages++
				if text := firstText(msg); text != "" {
					recentMessages = append(recentMessages, model.ConversationMessage{
						Role: "user", Text: model.Truncate(text, 300), Timestamp: ts,
					})
					if len(recentMessages) > 20 {
						recentMessages = recentMessages[len(recentMessages)-10:]
					}
				}
			}
		case "assistant":
			lastMeaningful = copyEntry(&e)
			session.AssistantMessages++
			if msg.Model != "" {
				session.Model = msg.Model
			}
			if text := firstText(msg); text != "" {
				recentMessages = append(recentMessages, model.ConversationMessage{
					Role: "assistant", Text: model.Truncate(text, 300), Timestamp: ts,
				})
				if len(recentMessages) > 20 {
					recentMessages = recentMessages[len(recentMessages)-10:]
				}
			}
			for _, c := range msg.Content {
				if c.Type == "tool_use" {
					recentTools = append(recentTools, model.ToolCall{Name: c.Name, Timestamp: ts})
					if len(recentTools) > 40 {
						recentTools = recentTools[len(recentTools)-20:]
					}
					if c.Name == "Write" || c.Name == "Edit" || c.Name == "NotebookEdit" {
						if fp := extractFilePathFromRaw(msg.RawContent); fp != "" {
							session.LastFileWrite = fp
							session.LastFileWriteAt = ts
						}
					}
				}
			}
		}
	}

	// Trim slices once at the end instead of per-iteration.
	if len(entryTimestamps) > 500 {
		entryTimestamps = entryTimestamps[len(entryTimestamps)-500:]
	}
	session.EntryTimestamps = entryTimestamps

	if len(recentTools) > 20 {
		recentTools = recentTools[len(recentTools)-20:]
	}
	session.RecentTools = recentTools

	if len(recentMessages) > 10 {
		recentMessages = recentMessages[len(recentMessages)-10:]
	}
	session.RecentMessages = recentMessages

	session.TotalMessages = session.UserMessages + session.AssistantMessages

	return bytesConsumed, parsed, lastMeaningful
}

// applyLastMeaningful sets status and current tool from the last user/assistant entry.
func applyLastMeaningful(session *model.Session, lastMeaningful *jsonlEntry) {
	session.Status = determineStatus(lastMeaningful)
	if lastMeaningful != nil {
		if ts, err := time.Parse(time.RFC3339Nano, lastMeaningful.Timestamp); err == nil {
			session.LastActivity = ts
		}
		if session.Status == model.StatusExecutingTool {
			if msg := lastMeaningful.parseMessage(); msg != nil {
				for _, c := range msg.Content {
					if c.Type == "tool_use" {
						session.CurrentTool = c.Name
					}
				}
			}
		}
	}
}

// ParseJSONL reads a JSONL file and returns a populated Session and
// the byte offset consumed (for incremental parsing on the next call).
func ParseJSONL(path string) (*model.Session, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
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

	bytesConsumed, parsed, lastMeaningful := scanEntries(scanner, session, 0, nil, nil, nil)

	if !parsed {
		return session, 0, nil
	}

	applyLastMeaningful(session, lastMeaningful)

	// Cap to file size: scanner adds +1 per line for the stripped newline,
	// but the last line may lack a trailing newline during concurrent writes.
	// Use a fresh Stat (not the one from before scanning) because the file
	// may have grown during scanning.
	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}

	return session, bytesConsumed, nil
}

// ParseJSONLIncremental reads only the tail of a JSONL file starting at the given
// byte offset, merging new entries into the provided base session.
// Returns the updated session and new byte offset.
func ParseJSONLIncremental(path string, offset int64, base *model.Session) (*model.Session, int64, error) {
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

	bytesConsumed, parsed, lastMeaningful := scanEntries(
		scanner, session, offset,
		session.RecentTools, session.RecentMessages, session.EntryTimestamps,
	)

	if !parsed {
		return session, offset, nil
	}

	// Only update status if we saw a user/assistant entry in the new tail.
	// Otherwise keep the status inherited from the base session.
	if lastMeaningful != nil {
		session.CurrentTool = "" // reset before possibly re-setting below
		applyLastMeaningful(session, lastMeaningful)
	}

	// Cap to file size to handle last line without trailing newline.
	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}

	return session, bytesConsumed, nil
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

// filePathNeedle is used by extractFilePathFromRaw for a quick byte-level pre-check.
var filePathNeedle = []byte(`"file_path"`)

// extractFilePathFromRaw extracts the "file_path" from a tool_use input inside
// the raw content JSON array. It parses only the tool_use entries (ignoring text
// blocks) with a targeted struct that captures just type, name, and input.
func extractFilePathFromRaw(rawContent json.RawMessage) string {
	if len(rawContent) == 0 || !bytes.Contains(rawContent, filePathNeedle) {
		return ""
	}
	// Parse only tool_use items — text blocks have no Input so they stay empty.
	var items []struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(rawContent, &items) != nil {
		return ""
	}
	// Walk backwards — we want the last Write/Edit/NotebookEdit file_path.
	for i := len(items) - 1; i >= 0; i-- {
		item := &items[i]
		if item.Type != "tool_use" || len(item.Input) == 0 {
			continue
		}
		if item.Name != "Write" && item.Name != "Edit" && item.Name != "NotebookEdit" {
			continue
		}
		var obj struct {
			FilePath string `json:"file_path"`
		}
		if json.Unmarshal(item.Input, &obj) == nil && obj.FilePath != "" {
			return obj.FilePath
		}
	}
	return ""
}

// copyEntry returns a shallow copy of a jsonlEntry so we can safely keep
// a pointer to it across loop iterations.
func copyEntry(e *jsonlEntry) *jsonlEntry {
	cp := *e
	return &cp
}

func determineStatus(e *jsonlEntry) model.SessionStatus {
	if e == nil {
		return model.StatusUnknown
	}
	switch e.Type {
	case "assistant":
		msg := e.parseMessage()
		if msg == nil {
			return model.StatusUnknown
		}
		for _, c := range msg.Content {
			if c.Type == "tool_use" {
				return model.StatusExecutingTool
			}
		}
		// Assistant responded with text — waiting for user
		return model.StatusWaitingForUser
	case "user":
		msg := e.parseMessage()
		if msg == nil {
			return model.StatusUnknown
		}
		if isToolResult(msg) {
			// Tool result was written — Claude is now thinking about it
			return model.StatusProcessingResult
		}
		// Human sent a message — Claude is thinking
		return model.StatusThinking
	}
	return model.StatusUnknown
}
