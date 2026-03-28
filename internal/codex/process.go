package codex

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// SessionsDir returns the path to Codex session JSONL files under ~/.codex/sessions.
func SessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "sessions")
}

// SessionIndexPath returns the path to Codex's thread-name index file.
func SessionIndexPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".codex", "session_index.jsonl")
}

// DiscoverSessions scans the Codex sessions tree for JSONL session files.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(SessionsDir(), SessionIndexPath(), cache)
}

func discoverSessionsFromDir(sessionsDir, indexPath string, cache *model.SessionCache) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	names := loadSessionNames(indexPath)
	wtCache := make(map[string]wtInfo)
	seen := make(map[string]struct{})
	var sessions []*model.Session

	err := filepath.WalkDir(sessionsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}

		seen[path] = struct{}{}
		cached, offset, mtime := cache.GetIncremental(path)

		var session *model.Session
		switch {
		case cached != nil && offset == 0:
			session = cached
		case cached != nil && offset > 0:
			s, newOffset, err := ParseJSONLIncremental(path, offset, cached)
			if err != nil {
				return nil
			}
			session = s
			enrichSession(session, wtCache, names)
			cache.Put(path, mtime, newOffset, session)
		default:
			s, size, err := ParseJSONL(path)
			if err != nil {
				return nil
			}
			session = s
			enrichSession(session, wtCache, names)
			cache.Put(path, mtime, size, session)
		}

		sessions = append(sessions, session)
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("walk codex sessions: %w", err)
	}

	cache.Prune(seen)
	return sessions, nil
}

type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

func enrichSession(session *model.Session, wtCache map[string]wtInfo, names map[string]string) {
	if session.SessionID != "" && session.Name == "" {
		session.Name = names[session.SessionID]
	}
	if session.CWD == "" {
		return
	}
	if _, ok := wtCache[session.CWD]; !ok {
		isWT, mainRepo := claude.IsWorktree(session.CWD)
		wtCache[session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
	}
	wt := wtCache[session.CWD]
	session.IsWorktree = wt.isWorktree
	session.MainRepo = wt.mainRepo
}

type indexEntry struct {
	ID         string `json:"id"`
	ThreadName string `json:"thread_name"`
}

func loadSessionNames(path string) map[string]string {
	names := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e indexEntry
		if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
			continue
		}
		if e.ID != "" && e.ThreadName != "" {
			names[e.ID] = e.ThreadName
		}
	}
	return names
}

type jsonlEnvelope struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	ID            string          `json:"id"`
	CWD           string          `json:"cwd"`
	CLIVersion    string          `json:"cli_version"`
	AgentNickname string          `json:"agent_nickname"`
	Source        json.RawMessage `json:"source"`
}

type turnContextPayload struct {
	CWD   string `json:"cwd"`
	Model string `json:"model"`
	Git   gitCtx `json:"git"`
}

type gitCtx struct {
	Branch string `json:"branch"`
}

type responseItemPayload struct {
	Type      string              `json:"type"`
	Name      string              `json:"name"`
	Role      string              `json:"role"`
	Arguments string              `json:"arguments"`
	Content   []responseItemBlock `json:"content"`
}

type responseItemBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type eventPayload struct {
	Type             string          `json:"type"`
	LastAgentMessage string          `json:"last_agent_message"`
	Info             *tokenCountInfo `json:"info"`
}

type tokenCountInfo struct {
	TotalTokenUsage tokenUsage `json:"total_token_usage"`
}

type tokenUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
	ReasoningOutput   int `json:"reasoning_output_tokens"`
}

type lastMeaningful struct {
	Kind      string
	Timestamp time.Time
	ToolName  string
}

// ParseJSONL reads a Codex session file and builds a Session snapshot.
func ParseJSONL(path string) (*model.Session, int64, error) {
	return parseJSONL(path, 0, nil)
}

// ParseJSONLIncremental reads only new lines and merges them into a prior session.
func ParseJSONLIncremental(path string, offset int64, base *model.Session) (*model.Session, int64, error) {
	return parseJSONL(path, offset, base)
}

func parseJSONL(path string, offset int64, base *model.Session) (*model.Session, int64, error) {
	f, err := os.Open(path)
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
		session.JSONLPath = path
	} else {
		session = &model.Session{
			JSONLPath: path,
			Agent:     "codex",
		}
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	bytesConsumed := offset
	var last lastMeaningful
	if base != nil {
		last = lastMeaningful{Kind: statusKind(base.Status), Timestamp: base.LastActivity, ToolName: base.CurrentTool}
	}

	for scanner.Scan() {
		bytesConsumed += int64(len(scanner.Bytes())) + 1

		var env jsonlEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			continue
		}

		ts, _ := time.Parse(time.RFC3339Nano, env.Timestamp)
		if !ts.IsZero() {
			session.EntryTimestamps = append(session.EntryTimestamps, ts)
			if len(session.EntryTimestamps) > 500 {
				session.EntryTimestamps = session.EntryTimestamps[len(session.EntryTimestamps)-500:]
			}
		}

		switch env.Type {
		case "session_meta":
			var meta sessionMetaPayload
			if err := json.Unmarshal(env.Payload, &meta); err != nil {
				continue
			}
			if meta.ID != "" {
				session.SessionID = meta.ID
			}
			if meta.CWD != "" {
				session.CWD = meta.CWD
			}
			if meta.CLIVersion != "" {
				session.Version = meta.CLIVersion
			}
			if meta.AgentNickname != "" || strings.Contains(string(meta.Source), "\"subagent\"") {
				session.IsSidechain = true
			}
		case "turn_context":
			var ctx turnContextPayload
			if err := json.Unmarshal(env.Payload, &ctx); err != nil {
				continue
			}
			if ctx.CWD != "" {
				session.CWD = ctx.CWD
			}
			if ctx.Model != "" {
				session.Model = ctx.Model
			}
			if ctx.Git.Branch != "" {
				session.GitBranch = ctx.Git.Branch
			}
		case "response_item":
			var item responseItemPayload
			if err := json.Unmarshal(env.Payload, &item); err != nil {
				continue
			}
			switch item.Type {
			case "message":
				text := strings.TrimSpace(joinItemText(item.Content))
				switch item.Role {
				case "user":
					session.UserMessages++
					if text != "" {
						appendMessage(session, "user", text, ts)
					}
					last = lastMeaningful{Kind: "user", Timestamp: ts}
				case "assistant":
					session.AssistantMessages++
					if text != "" {
						appendMessage(session, "assistant", text, ts)
					}
					last = lastMeaningful{Kind: "assistant", Timestamp: ts}
				}
			case "function_call":
				appendTool(session, item.Name, ts)
				last = lastMeaningful{Kind: "tool", Timestamp: ts, ToolName: item.Name}
				if item.Name == "apply_patch" {
					session.LastFileWrite = "apply_patch"
					session.LastFileWriteAt = ts
				}
			case "function_call_output":
				last = lastMeaningful{Kind: "tool_output", Timestamp: ts}
			}
		case "event_msg":
			var event eventPayload
			if err := json.Unmarshal(env.Payload, &event); err != nil {
				continue
			}
			switch event.Type {
			case "user_message":
				last = lastMeaningful{Kind: "user", Timestamp: ts}
			case "agent_message":
				last = lastMeaningful{Kind: "assistant", Timestamp: ts}
			case "token_count":
				if event.Info != nil {
					session.InputTokens = event.Info.TotalTokenUsage.InputTokens
					session.CacheReadTokens = event.Info.TotalTokenUsage.CachedInputTokens
					session.OutputTokens = event.Info.TotalTokenUsage.OutputTokens + event.Info.TotalTokenUsage.ReasoningOutput
				}
			case "task_complete":
				if strings.TrimSpace(event.LastAgentMessage) != "" {
					last = lastMeaningful{Kind: "assistant", Timestamp: ts}
				}
			}
		}
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages
	session.Status = statusFromKind(last.Kind)
	session.CurrentTool = ""
	if session.Status == model.StatusExecutingTool {
		session.CurrentTool = last.ToolName
	}
	if !last.Timestamp.IsZero() {
		session.LastActivity = last.Timestamp
	}

	if fi, err := f.Stat(); err == nil && bytesConsumed > fi.Size() {
		bytesConsumed = fi.Size()
	}

	return session, bytesConsumed, nil
}

func appendTool(session *model.Session, name string, ts time.Time) {
	if name == "" {
		return
	}
	session.RecentTools = append(session.RecentTools, model.ToolCall{Name: name, Timestamp: ts})
	if len(session.RecentTools) > 20 {
		session.RecentTools = session.RecentTools[len(session.RecentTools)-20:]
	}
}

func appendMessage(session *model.Session, role, text string, ts time.Time) {
	session.RecentMessages = append(session.RecentMessages, model.ConversationMessage{
		Role:      role,
		Text:      model.Truncate(text, 300),
		Timestamp: ts,
	})
	if len(session.RecentMessages) > 10 {
		session.RecentMessages = session.RecentMessages[len(session.RecentMessages)-10:]
	}
}

func joinItemText(content []responseItemBlock) string {
	var parts []string
	for _, block := range content {
		if (block.Type == "input_text" || block.Type == "output_text") && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func statusKind(status model.SessionStatus) string {
	switch status {
	case model.StatusWaitingForUser:
		return "assistant"
	case model.StatusThinking:
		return "user"
	case model.StatusExecutingTool:
		return "tool"
	case model.StatusProcessingResult:
		return "tool_output"
	default:
		return ""
	}
}

func statusFromKind(kind string) model.SessionStatus {
	switch kind {
	case "assistant":
		return model.StatusWaitingForUser
	case "user":
		return model.StatusThinking
	case "tool":
		return model.StatusExecutingTool
	case "tool_output":
		return model.StatusProcessingResult
	default:
		return model.StatusIdle
	}
}
