package grok

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// supportedChatFormatVersion is the highest summary.json chat_format_version
// this parser understands. A session declaring a newer version is skipped so
// a future Grok upgrade cannot crash discovery.
const supportedChatFormatVersion = 1

const (
	maxRecentMessages  = 10
	maxRecentTools     = 20
	maxEntryTimestamps = 500
	// maxUpdatesTailBytes bounds how much of updates.jsonl is read for the
	// activity sparkline. updates.jsonl can reach ~18 MB; only the recent
	// tail matters for the (short) sparkline window.
	maxUpdatesTailBytes = 512 * 1024
)

// ParseGrokSession reads one Grok session directory into a model.Session.
// It returns an error when summary.json is missing/unreadable or declares an
// unsupported chat_format_version; callers skip such sessions.
func ParseGrokSession(sessionDir string) (*model.Session, error) {
	summary, err := readGrokSummary(filepath.Join(sessionDir, "summary.json"))
	if err != nil {
		return nil, err
	}
	if summary.ChatFormatVersion > supportedChatFormatVersion {
		return nil, fmt.Errorf("grok: unsupported chat_format_version %d", summary.ChatFormatVersion)
	}

	s := &model.Session{
		Agent:         "grok",
		SessionID:     summary.Info.ID,
		JSONLPath:     sessionDir,
		CWD:           summary.Info.CWD,
		Model:         summary.CurrentModelID,
		GitBranch:     summary.HeadBranch,
		TotalMessages: summary.NumChatMessages,
		IsSidechain:   summary.SessionKind == "subagent",
	}
	if s.SessionID == "" {
		s.SessionID = filepath.Base(sessionDir)
	}
	if s.CWD == "" {
		s.CWD = decodeGrokDirName(filepath.Base(filepath.Dir(sessionDir)))
	}
	if summary.ChatFormatVersion > 0 {
		s.Version = strconv.Itoa(summary.ChatFormatVersion)
	}

	// Timestamps. updated_at == last_active_at in every observed sample;
	// fall back across both, then created_at.
	s.LastActivity = parseGrokTime(firstNonEmpty(summary.LastActiveAt, summary.UpdatedAt, summary.CreatedAt))
	s.LastSummaryAt = parseGrokTime(firstNonEmpty(summary.UpdatedAt, summary.LastActiveAt))

	// Transcript: counts, recent messages/tools, status.
	chat := parseGrokChatHistory(filepath.Join(sessionDir, "chat_history.jsonl"))
	s.RecentMessages = chat.recentMessages
	s.RecentTools = chat.recentTools
	s.Status, s.CurrentTool = grokStatus(chat.lastEntry)

	// Message counts: prefer signals.json, fall back to counting the transcript.
	if signals, ok := readGrokSignals(filepath.Join(sessionDir, "signals.json")); ok {
		s.UserMessages = signals.UserMessageCount
		s.AssistantMessages = signals.AssistantMessageCount
	} else {
		s.UserMessages = chat.userCount
		s.AssistantMessages = chat.assistantCount
	}
	if s.TotalMessages == 0 {
		s.TotalMessages = s.UserMessages + s.AssistantMessages
	}

	// Title: generated_title → session_summary → first user message.
	s.Name = firstNonEmpty(summary.GeneratedTitle, summary.SessionSummary, chat.firstUserText)

	// Activity sparkline timestamps from the bounded tail of updates.jsonl.
	s.EntryTimestamps = readGrokUpdatesTail(filepath.Join(sessionDir, "updates.jsonl"))

	return s, nil
}

// decodeGrokDirName reverses Grok's cwd encoding (standard URL percent-encoding;
// in practice only "/" is escaped as %2F).
func decodeGrokDirName(name string) string {
	if decoded, err := url.PathUnescape(name); err == nil {
		return decoded
	}
	return name
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

func parseGrokTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func readGrokSummary(path string) (*grokSummary, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var sum grokSummary
	if err := json.Unmarshal(data, &sum); err != nil {
		return nil, fmt.Errorf("grok: parse summary.json: %w", err)
	}
	return &sum, nil
}

// readGrokSignals returns (signals, true) when signals.json exists and parses.
func readGrokSignals(path string) (grokSignals, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return grokSignals{}, false
	}
	var sig grokSignals
	if err := json.Unmarshal(data, &sig); err != nil {
		return grokSignals{}, false
	}
	return sig, true
}

// grokChatResult is what parseGrokChatHistory extracts from chat_history.jsonl.
type grokChatResult struct {
	recentMessages []model.ConversationMessage
	recentTools    []model.ToolCall
	lastEntry      *grokChatEntry
	userCount      int
	assistantCount int
	firstUserText  string
}

// parseGrokChatHistory scans chat_history.jsonl. A missing or unreadable file
// yields a zero-value result — the session is still valid from summary.json.
func parseGrokChatHistory(path string) grokChatResult {
	var res grokChatResult
	f, err := os.Open(path)
	if err != nil {
		return res
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		var e grokChatEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		switch e.Type {
		case "user":
			res.userCount++
			res.lastEntry = copyGrokEntry(&e)
			text := grokEntryText(e.Content)
			if res.firstUserText == "" && text != "" {
				res.firstUserText = model.Truncate(text, 300)
			}
			if text != "" {
				res.recentMessages = append(res.recentMessages, model.ConversationMessage{
					Role: "user", Text: model.Truncate(text, 300),
				})
			}
		case "assistant":
			res.assistantCount++
			res.lastEntry = copyGrokEntry(&e)
			for _, tc := range e.ToolCalls {
				res.recentTools = append(res.recentTools, model.ToolCall{
					Name: normalizeGrokToolName(tc.Name),
				})
			}
			if text := grokEntryText(e.Content); text != "" {
				res.recentMessages = append(res.recentMessages, model.ConversationMessage{
					Role: "assistant", Text: model.Truncate(text, 300),
				})
			}
		case "tool_result":
			res.lastEntry = copyGrokEntry(&e)
		}
	}

	if len(res.recentMessages) > maxRecentMessages {
		res.recentMessages = res.recentMessages[len(res.recentMessages)-maxRecentMessages:]
	}
	if len(res.recentTools) > maxRecentTools {
		res.recentTools = res.recentTools[len(res.recentTools)-maxRecentTools:]
	}
	return res
}

func copyGrokEntry(e *grokChatEntry) *grokChatEntry {
	cp := *e
	return &cp
}

// grokEntryText extracts the first human-readable text from a chat entry's
// content: a plain string for system/assistant/tool_result entries, an array
// of {type,text} blocks for user entries.
func grokEntryText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	switch raw[0] {
	case '"':
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	case '[':
		var blocks []grokContentBlock
		if json.Unmarshal(raw, &blocks) != nil {
			return ""
		}
		for _, b := range blocks {
			if b.Type == "text" && b.Text != "" {
				return b.Text
			}
		}
	}
	return ""
}

// normalizeGrokToolName upper-cases the first letter so Grok tool names render
// consistently with the other providers' activity labels.
func normalizeGrokToolName(name string) string {
	if name == "" {
		return name
	}
	r, size := utf8.DecodeRuneInString(name)
	return string(unicode.ToUpper(r)) + name[size:]
}

// grokStatus infers session status from the last transcript entry.
func grokStatus(e *grokChatEntry) (model.SessionStatus, string) {
	if e == nil {
		return model.StatusUnknown, ""
	}
	switch e.Type {
	case "assistant":
		if len(e.ToolCalls) > 0 {
			return model.StatusExecutingTool, normalizeGrokToolName(e.ToolCalls[len(e.ToolCalls)-1].Name)
		}
		return model.StatusWaitingForUser, ""
	case "user":
		return model.StatusThinking, ""
	case "tool_result":
		return model.StatusProcessingResult, ""
	}
	return model.StatusUnknown, ""
}

// readGrokUpdatesTail reads the trailing maxUpdatesTailBytes of updates.jsonl
// and returns the event timestamps it finds, for the activity sparkline.
// updates.jsonl is the largest file in a session, so only the tail is read.
func readGrokUpdatesTail(path string) []time.Time {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	var start int64
	if info.Size() > maxUpdatesTailBytes {
		start = info.Size() - maxUpdatesTailBytes
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	// Seeking into the middle of the file lands mid-line; discard the first
	// (almost certainly partial) line.
	if start > 0 && scanner.Scan() {
		_ = scanner.Bytes()
	}

	var stamps []time.Time
	for scanner.Scan() {
		var u grokUpdate
		if json.Unmarshal(scanner.Bytes(), &u) != nil || u.Timestamp <= 0 {
			continue
		}
		sec := int64(u.Timestamp)
		nsec := int64((u.Timestamp - float64(sec)) * 1e9)
		stamps = append(stamps, time.Unix(sec, nsec))
	}
	if len(stamps) > maxEntryTimestamps {
		stamps = stamps[len(stamps)-maxEntryTimestamps:]
	}
	return stamps
}
