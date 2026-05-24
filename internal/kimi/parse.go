package kimi

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

const (
	maxRecentMessages  = 10
	maxRecentTools     = 20
	maxEntryTimestamps = 500
)

// ParseSession reads one Kimi session directory into a model.Session.
func ParseSession(sessionDir, workDir string) (*model.Session, int64, error) {
	return ParseSessionIncremental(sessionDir, workDir, 0, nil)
}

// ParseSessionIncremental reads one Kimi session directory, optionally parsing
// only the bytes appended to wire.jsonl and merging them into base.
func ParseSessionIncremental(sessionDir, workDir string, offset int64, base *model.Session) (*model.Session, int64, error) {
	wirePath := filepath.Join(sessionDir, "wire.jsonl")
	f, err := os.Open(wirePath)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()

	if offset > 0 {
		if _, err := f.Seek(offset, 0); err != nil {
			return nil, 0, err
		}
	}

	var session *model.Session
	if base != nil {
		session = base.Clone()
		session.JSONLPath = sessionDir
	} else {
		session = &model.Session{
			Agent:     "kimi",
			SessionID: filepath.Base(sessionDir),
			JSONLPath: sessionDir,
			CWD:       workDir,
		}
	}
	if session.CWD == "" {
		session.CWD = workDir
	}
	if session.SessionID == "" {
		session.SessionID = filepath.Base(sessionDir)
	}

	state := readState(filepath.Join(sessionDir, "state.json"))
	if state.CustomTitle != "" {
		session.Name = state.CustomTitle
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)

	bytesConsumed := offset
	status := session.Status
	currentTool := session.CurrentTool
	if base == nil {
		status = model.StatusIdle
	}

	for scanner.Scan() {
		line := scanner.Bytes()
		bytesConsumed += int64(len(line)) + 1

		var env wireEnvelope
		if err := json.Unmarshal(line, &env); err != nil {
			continue
		}
		if env.Type == "metadata" {
			if env.ProtocolVersion != "" {
				session.Version = env.ProtocolVersion
			}
			continue
		}
		if env.Message.Type == "" {
			continue
		}

		ts := unixSeconds(env.Timestamp)
		if !ts.IsZero() {
			session.LastActivity = ts
			session.EntryTimestamps = append(session.EntryTimestamps, ts)
			if len(session.EntryTimestamps) > maxEntryTimestamps {
				session.EntryTimestamps = session.EntryTimestamps[len(session.EntryTimestamps)-maxEntryTimestamps:]
			}
		}

		switch env.Message.Type {
		case "TurnBegin":
			var p turnBeginPayload
			if json.Unmarshal(env.Message.Payload, &p) == nil {
				text := blocksText(p.UserInput, map[string]bool{"text": true})
				if text != "" {
					session.UserMessages++
					appendMessage(session, "user", text, ts)
				}
			}
			status = model.StatusThinking
			currentTool = ""
		case "ContentPart":
			var p contentPartPayload
			if json.Unmarshal(env.Message.Payload, &p) == nil && p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				session.AssistantMessages++
				appendMessage(session, "assistant", p.Text, ts)
			}
		case "ToolCall":
			var p toolCallPayload
			if json.Unmarshal(env.Message.Payload, &p) == nil {
				toolName := p.Function.Name
				appendTool(session, toolName, ts)
				setLastFileWrite(session, toolName, p.Function.Arguments, ts)
				status = model.StatusExecutingTool
				currentTool = toolName
			}
		case "ToolCallPart":
			if currentTool != "" {
				status = model.StatusExecutingTool
			}
		case "ToolResult":
			status = model.StatusProcessingResult
		case "StatusUpdate":
			var p statusUpdatePayload
			if json.Unmarshal(env.Message.Payload, &p) == nil {
				session.InputTokens += p.TokenUsage.InputOther
				session.OutputTokens += p.TokenUsage.Output
				session.CacheReadTokens += p.TokenUsage.InputCacheRead
				session.CacheCreationTokens += p.TokenUsage.InputCacheCreation
			}
		case "TurnEnd":
			status = model.StatusWaitingForUser
			currentTool = ""
		case "CompactionBegin", "CompactionEnd":
			session.LastSummaryAt = ts
		case "QuestionRequest", "StepInterrupted":
			status = model.StatusWaitingForUser
			currentTool = ""
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages
	session.Status = status
	session.CurrentTool = ""
	if status == model.StatusExecutingTool {
		session.CurrentTool = currentTool
	}
	if session.Name == "" {
		session.Name = firstUserMessage(session.RecentMessages)
	}

	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}
	return session, bytesConsumed, nil
}

type kimiState struct {
	CustomTitle string `json:"custom_title"`
}

func readState(path string) kimiState {
	data, err := os.ReadFile(path)
	if err != nil {
		return kimiState{}
	}
	var state kimiState
	_ = json.Unmarshal(data, &state)
	return state
}

type wireEnvelope struct {
	Type            string      `json:"type"`
	ProtocolVersion string      `json:"protocol_version"`
	Timestamp       float64     `json:"timestamp"`
	Message         wireMessage `json:"message"`
}

type wireMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type turnBeginPayload struct {
	UserInput []contentBlock `json:"user_input"`
}

type contentPartPayload struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type toolCallPayload struct {
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type statusUpdatePayload struct {
	TokenUsage kimiTokenUsage `json:"token_usage"`
}

type kimiTokenUsage struct {
	InputOther         int `json:"input_other"`
	Output             int `json:"output"`
	InputCacheRead     int `json:"input_cache_read"`
	InputCacheCreation int `json:"input_cache_creation"`
}

func unixSeconds(seconds float64) time.Time {
	if seconds == 0 {
		return time.Time{}
	}
	sec := int64(seconds)
	nsec := int64((seconds - float64(sec)) * 1e9)
	return time.Unix(sec, nsec)
}

func blocksText(blocks []contentBlock, allowed map[string]bool) string {
	var parts []string
	for _, block := range blocks {
		if block.Text != "" && allowed[block.Type] {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func appendTool(session *model.Session, name string, ts time.Time) {
	if name == "" {
		return
	}
	session.RecentTools = append(session.RecentTools, model.ToolCall{Name: name, Timestamp: ts})
	if len(session.RecentTools) > maxRecentTools {
		session.RecentTools = session.RecentTools[len(session.RecentTools)-maxRecentTools:]
	}
}

func appendMessage(session *model.Session, role, text string, ts time.Time) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	session.RecentMessages = append(session.RecentMessages, model.ConversationMessage{
		Role:      role,
		Text:      model.Truncate(text, 300),
		Timestamp: ts,
	})
	if len(session.RecentMessages) > maxRecentMessages {
		session.RecentMessages = session.RecentMessages[len(session.RecentMessages)-maxRecentMessages:]
	}
}

func setLastFileWrite(session *model.Session, toolName, args string, ts time.Time) {
	if toolName != "WriteFile" && toolName != "StrReplaceFile" {
		return
	}
	var payload struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal([]byte(args), &payload); err != nil || payload.Path == "" {
		session.LastFileWrite = toolName
		session.LastFileWriteAt = ts
		return
	}
	if filepath.IsAbs(payload.Path) || session.CWD == "" {
		session.LastFileWrite = payload.Path
	} else {
		session.LastFileWrite = filepath.Join(session.CWD, payload.Path)
	}
	session.LastFileWriteAt = ts
}

func firstUserMessage(messages []model.ConversationMessage) string {
	for _, msg := range messages {
		if msg.Role == "user" && msg.Text != "" {
			return msg.Text
		}
	}
	return ""
}

func parseContextFile(path, sessionID, cwd, name string) ([]ContextChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []ContextChunk
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var entry contextEntry
		if json.Unmarshal(scanner.Bytes(), &entry) != nil {
			continue
		}
		if entry.Role != "user" && entry.Role != "assistant" {
			continue
		}
		text := contextText(entry.Content)
		if text == "" {
			continue
		}
		chunks = append(chunks, ContextChunk{
			SessionID: sessionID,
			CWD:       cwd,
			Name:      name,
			Role:      entry.Role,
			Text:      text,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return chunks, nil
}

type contextEntry struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func contextText(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			return strings.TrimSpace(s)
		}
		return ""
	}
	var blocks []contentBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	return strings.TrimSpace(blocksText(blocks, map[string]bool{"text": true}))
}

// ContextChunk is a normalized Kimi transcript chunk for search indexing.
type ContextChunk struct {
	SessionID string
	CWD       string
	Name      string
	Role      string
	Text      string
}

// ExtractContextChunks reads context.jsonl for search indexing.
func ExtractContextChunks(sessionDir, workDir string) ([]ContextChunk, error) {
	sessionID := filepath.Base(sessionDir)
	state := readState(filepath.Join(sessionDir, "state.json"))
	chunks, err := parseContextFile(filepath.Join(sessionDir, "context.jsonl"), sessionID, workDir, state.CustomTitle)
	if err == nil || !os.IsNotExist(err) {
		return chunks, err
	}
	return extractWireChunks(filepath.Join(sessionDir, "wire.jsonl"), sessionID, workDir, state.CustomTitle)
}

func extractWireChunks(path, sessionID, cwd, name string) ([]ContextChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var chunks []ContextChunk
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var env wireEnvelope
		if json.Unmarshal(scanner.Bytes(), &env) != nil {
			continue
		}
		switch env.Message.Type {
		case "TurnBegin":
			var p turnBeginPayload
			if json.Unmarshal(env.Message.Payload, &p) == nil {
				if text := blocksText(p.UserInput, map[string]bool{"text": true}); strings.TrimSpace(text) != "" {
					chunks = append(chunks, ContextChunk{SessionID: sessionID, CWD: cwd, Name: name, Role: "user", Text: text})
				}
			}
		case "ContentPart":
			var p contentPartPayload
			if json.Unmarshal(env.Message.Payload, &p) == nil && p.Type == "text" && strings.TrimSpace(p.Text) != "" {
				chunks = append(chunks, ContextChunk{SessionID: sessionID, CWD: cwd, Name: name, Role: "assistant", Text: p.Text})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(chunks) == 0 {
		return nil, fmt.Errorf("kimi: no searchable content in %s", path)
	}
	return chunks, nil
}
