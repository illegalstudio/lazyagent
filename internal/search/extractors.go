package search

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/illegalstudio/lazyagent/internal/amp"
	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/codex"
	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/pi"
)

type indexStats struct {
	Sources  int
	Updated  int
	Skipped  int
	Warnings []error
}

func indexAgents(idx *index, agents []string, reindex bool) indexStats {
	var stats indexStats
	if reindex {
		if err := idx.reset(); err != nil {
			stats.Warnings = append(stats.Warnings, fmt.Errorf("reset index: %w", err))
		}
	}
	cfg := core.LoadConfig()
	for _, agent := range agents {
		seen, agentStats := indexAgent(idx, agent, cfg)
		stats.Sources += agentStats.Sources
		stats.Updated += agentStats.Updated
		stats.Skipped += agentStats.Skipped
		stats.Warnings = append(stats.Warnings, agentStats.Warnings...)
		if err := idx.pruneMissing(agent, seen); err != nil {
			stats.Warnings = append(stats.Warnings, fmt.Errorf("prune %s index: %w", agent, err))
		}
	}
	return stats
}

func indexAgent(idx *index, agent string, cfg core.Config) (map[string]struct{}, indexStats) {
	seen := make(map[string]struct{})
	sources, err := listSources(agent, cfg)
	stats := indexStats{Sources: len(sources)}
	if err != nil {
		stats.Warnings = append(stats.Warnings, err)
		return seen, stats
	}
	for _, src := range sources {
		seen[src.ID] = struct{}{}
		current, err := idx.sourceCurrent(src)
		if err != nil {
			stats.Warnings = append(stats.Warnings, fmt.Errorf("%s %s: %w", agent, src.ID, err))
			continue
		}
		if current {
			stats.Skipped++
			_ = idx.touchSource(src)
			continue
		}
		chunks, err := extractChunks(src, cfg)
		if err != nil {
			stats.Warnings = append(stats.Warnings, fmt.Errorf("%s %s: %w", agent, src.ID, err))
			continue
		}
		if err := idx.replaceSource(src, chunks); err != nil {
			stats.Warnings = append(stats.Warnings, fmt.Errorf("index %s %s: %w", agent, src.ID, err))
			continue
		}
		stats.Updated++
	}
	return seen, stats
}

func listSources(agent string, cfg core.Config) ([]sourceState, error) {
	switch agent {
	case "claude":
		var out []sourceState
		for _, dir := range claude.ClaudeProjectsDirs(cfg.ClaudeDirs) {
			files, _ := filepath.Glob(filepath.Join(dir, "*", "*.jsonl"))
			for _, path := range files {
				if src, ok := fileSource(agent, strings.TrimSuffix(filepath.Base(path), ".jsonl"), path); ok {
					out = append(out, src)
				}
			}
		}
		return out, nil
	case "codex":
		return walkFiles(agent, codex.SessionsDir(), ".jsonl")
	case "pi":
		return walkFiles(agent, pi.PiSessionsDir(), ".jsonl")
	case "amp":
		return walkFiles(agent, amp.ThreadsDir(), ".json")
	default:
		return nil, fmt.Errorf("unsupported agent %q", agent)
	}
}

func walkFiles(agent, root, ext string) ([]sourceState, error) {
	if root == "" {
		return nil, nil
	}
	var out []sourceState
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ext {
			return nil
		}
		id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		if src, ok := fileSource(agent, id, path); ok {
			out = append(out, src)
		}
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return out, err
}

func fileSource(agent, id, path string) (sourceState, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return sourceState{}, false
	}
	return sourceState{
		Agent:   agent,
		ID:      id,
		Path:    path,
		MTimeNS: info.ModTime().UnixNano(),
		Size:    info.Size(),
	}, true
}

func extractChunks(src sourceState, cfg core.Config) ([]chunk, error) {
	switch src.Agent {
	case "claude":
		return extractClaude(src)
	case "codex":
		return extractCodex(src, loadCodexNames(codex.SessionIndexPath()))
	case "pi":
		return extractPi(src)
	case "amp":
		return extractAmp(src)
	default:
		return nil, fmt.Errorf("unsupported agent %q", src.Agent)
	}
	_ = cfg
	return nil, nil
}

type genericBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func contentText(raw json.RawMessage, allowed map[string]bool) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	if raw[0] == '"' {
		var s string
		_ = json.Unmarshal(raw, &s)
		return s
	}
	if raw[0] != '[' {
		return ""
	}
	var blocks []genericBlock
	if err := json.Unmarshal(raw, &blocks); err != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Text != "" && allowed[b.Type] {
			parts = append(parts, b.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func appendChunk(chunks []chunk, src sourceState, sessionID, cwd, name, role string, ts time.Time, text string) []chunk {
	text = strings.TrimSpace(text)
	if text == "" {
		return chunks
	}
	return append(chunks, chunk{
		Source:    src,
		SessionID: sessionID,
		CWD:       cwd,
		Name:      name,
		Role:      role,
		Timestamp: ts,
		Text:      text,
	})
}

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func millis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func scanJSONL(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		fn(scanner.Bytes())
	}
	return scanner.Err()
}

type claudeEntry struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"sessionId"`
	CWD        string          `json:"cwd"`
	Timestamp  string          `json:"timestamp"`
	RawMessage json.RawMessage `json:"message"`
}

type claudeMsg struct {
	Role       string          `json:"role"`
	RawContent json.RawMessage `json:"content"`
}

func extractClaude(src sourceState) ([]chunk, error) {
	sessionID := src.ID
	var cwd string
	var chunks []chunk
	err := scanJSONL(src.Path, func(line []byte) {
		var e claudeEntry
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if e.SessionID != "" {
			sessionID = e.SessionID
		}
		if cwd == "" && e.CWD != "" {
			cwd = e.CWD
		}
		if e.Type != "user" && e.Type != "assistant" {
			return
		}
		var msg claudeMsg
		if len(e.RawMessage) == 0 || json.Unmarshal(e.RawMessage, &msg) != nil {
			return
		}
		text := contentText(msg.RawContent, map[string]bool{"text": true})
		if e.Type == "user" && text == "" {
			return
		}
		chunks = appendChunk(chunks, src, sessionID, cwd, "", e.Type, parseTS(e.Timestamp), text)
	})
	return chunks, err
}

type codexEnv struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type codexMeta struct {
	ID  string `json:"id"`
	CWD string `json:"cwd"`
}

type codexItem struct {
	Type    string         `json:"type"`
	Role    string         `json:"role"`
	Content []genericBlock `json:"content"`
}

func loadCodexNames(path string) map[string]string {
	names := make(map[string]string)
	f, err := os.Open(path)
	if err != nil {
		return names
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		var e struct {
			ID         string `json:"id"`
			ThreadName string `json:"thread_name"`
		}
		if json.Unmarshal(scanner.Bytes(), &e) == nil && e.ID != "" {
			names[e.ID] = e.ThreadName
		}
	}
	return names
}

func extractCodex(src sourceState, names map[string]string) ([]chunk, error) {
	sessionID := src.ID
	var cwd string
	var chunks []chunk
	err := scanJSONL(src.Path, func(line []byte) {
		var env codexEnv
		if json.Unmarshal(line, &env) != nil {
			return
		}
		switch env.Type {
		case "session_meta":
			var meta codexMeta
			if json.Unmarshal(env.Payload, &meta) == nil {
				if meta.ID != "" {
					sessionID = meta.ID
				}
				if meta.CWD != "" {
					cwd = meta.CWD
				}
			}
		case "response_item":
			var item codexItem
			if json.Unmarshal(env.Payload, &item) != nil || item.Type != "message" {
				return
			}
			var parts []string
			for _, b := range item.Content {
				if (b.Type == "input_text" || b.Type == "output_text") && b.Text != "" {
					parts = append(parts, b.Text)
				}
			}
			chunks = appendChunk(chunks, src, sessionID, cwd, names[sessionID], item.Role, parseTS(env.Timestamp), strings.Join(parts, "\n"))
		}
	})
	return chunks, err
}

type piEntry struct {
	Type      string          `json:"type"`
	Timestamp string          `json:"timestamp"`
	CWD       string          `json:"cwd"`
	Name      string          `json:"name"`
	RawMsg    json.RawMessage `json:"message"`
}

type piMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

func extractPi(src sourceState) ([]chunk, error) {
	sessionID := src.ID
	var cwd, name string
	var chunks []chunk
	err := scanJSONL(src.Path, func(line []byte) {
		var e piEntry
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if e.Type == "session" && e.CWD != "" {
			cwd = e.CWD
			return
		}
		if e.Type == "session_info" && e.Name != "" {
			name = e.Name
			return
		}
		if e.Type != "message" {
			return
		}
		var msg piMsg
		if json.Unmarshal(e.RawMsg, &msg) != nil || msg.Role == "toolResult" {
			return
		}
		text := contentText(msg.Content, map[string]bool{"text": true})
		chunks = appendChunk(chunks, src, sessionID, cwd, name, msg.Role, parseTS(e.Timestamp), text)
	})
	return chunks, err
}

type ampThread struct {
	ID       string       `json:"id"`
	Title    string       `json:"title"`
	Created  int64        `json:"created"`
	Messages []ampMessage `json:"messages"`
	Env      struct {
		Initial struct {
			Trees []struct {
				URI string `json:"uri"`
			} `json:"trees"`
		} `json:"initial"`
	} `json:"env"`
}

type ampMessage struct {
	Role    string       `json:"role"`
	Content []ampContent `json:"content"`
	Meta    *struct {
		SentAt int64 `json:"sentAt"`
	} `json:"meta"`
}

type ampContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

func extractAmp(src sourceState) ([]chunk, error) {
	data, err := os.ReadFile(src.Path)
	if err != nil {
		return nil, err
	}
	var thread ampThread
	if err := json.Unmarshal(data, &thread); err != nil {
		return nil, err
	}
	sessionID := thread.ID
	if sessionID == "" {
		sessionID = src.ID
	}
	var cwd string
	if len(thread.Env.Initial.Trees) > 0 {
		cwd = strings.TrimPrefix(thread.Env.Initial.Trees[0].URI, "file://")
	}
	var chunks []chunk
	for _, msg := range thread.Messages {
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		var parts []string
		for _, block := range msg.Content {
			if block.Type == "text" && block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		ts := millis(thread.Created)
		if msg.Meta != nil && msg.Meta.SentAt > 0 {
			ts = millis(msg.Meta.SentAt)
		}
		chunks = appendChunk(chunks, src, sessionID, cwd, thread.Title, msg.Role, ts, strings.Join(parts, "\n"))
	}
	return chunks, nil
}
