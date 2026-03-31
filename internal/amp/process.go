package amp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// ThreadsDir returns the path to Amp's local thread store.
func ThreadsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "amp", "threads")
}

// SessionPath returns the path to Amp's local session metadata.
func SessionPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "amp", "session.json")
}

// DiscoverSessions scans Amp's local thread store for session files.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(ThreadsDir(), SessionPath(), cache)
}

type parseJob struct {
	path         string
	lastThreadID string
	mtime        time.Time
}

type parseResult struct {
	session *model.Session
	path    string
	mtime   time.Time
	size    int64
}

func discoverSessionsFromDir(threadsDir, sessionPath string, cache *model.SessionCache) ([]*model.Session, error) {
	if threadsDir == "" {
		return nil, fmt.Errorf("could not find home directory")
	}

	lastThreadID := loadLastThreadID(sessionPath)
	seen := make(map[string]struct{})
	var sessions []*model.Session
	var jobs []parseJob

	entries, err := os.ReadDir(threadsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("could not read amp threads dir: %w", err)
	}

	// Phase 1: collect cache hits and jobs for parsing.
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		path := filepath.Join(threadsDir, entry.Name())
		seen[path] = struct{}{}

		cached, offset, mtime := cache.GetIncremental(path)
		if cached != nil && offset == 0 {
			// Full cache hit — file unchanged.
			sessions = append(sessions, cached)
			continue
		}

		jobs = append(jobs, parseJob{
			path:         path,
			lastThreadID: lastThreadID,
			mtime:        mtime,
		})
	}

	if len(jobs) > 0 {
		// Phase 2: parse files in parallel.
		workers := runtime.GOMAXPROCS(0)
		if workers > len(jobs) {
			workers = len(jobs)
		}

		results := make([]parseResult, len(jobs))
		var wg sync.WaitGroup
		jobCh := make(chan int, len(jobs))

		for i := range jobs {
			jobCh <- i
		}
		close(jobCh)

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range jobCh {
					j := &jobs[idx]
					session, size, err := ParseThread(j.path, j.lastThreadID)
					if err != nil {
						continue
					}
					results[idx] = parseResult{
						session: session,
						path:    j.path,
						mtime:   j.mtime,
						size:    size,
					}
				}
			}()
		}
		wg.Wait()

		// Phase 3: enrich and update cache (sequential).
		wtCache := make(map[string]wtInfo)
		for _, r := range results {
			if r.session == nil {
				continue
			}
			enrichWorktree(r.session, wtCache)
			cache.Put(r.path, r.mtime, r.size, r.session)
			sessions = append(sessions, r.session)
		}
	}

	cache.Prune(seen)
	return sessions, nil
}

type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

func enrichWorktree(session *model.Session, wtCache map[string]wtInfo) {
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

type sessionMeta struct {
	LastThreadID string `json:"lastThreadId"`
}

func loadLastThreadID(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var meta sessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return ""
	}
	return meta.LastThreadID
}

type threadFile struct {
	ID       string      `json:"id"`
	Created  int64       `json:"created"`
	Title    string      `json:"title"`
	Messages []threadMsg `json:"messages"`
	Env      threadEnv   `json:"env"`
	Debug    threadDebug `json:"~debug"`
}

type threadEnv struct {
	Initial threadInitial `json:"initial"`
}

type threadInitial struct {
	Trees    []threadTree `json:"trees"`
	Platform platformInfo `json:"platform"`
	Tags     []string     `json:"tags"`
}

type threadTree struct {
	DisplayName string         `json:"displayName"`
	URI         string         `json:"uri"`
	Repository  *threadRepoRef `json:"repository"`
}

type threadRepoRef struct {
	Type string `json:"type"`
	URL  string `json:"url"`
	Ref  string `json:"ref"`
	SHA  string `json:"sha"`
}

type platformInfo struct {
	Client        string `json:"client"`
	ClientVersion string `json:"clientVersion"`
	ClientType    string `json:"clientType"`
}

type threadDebug struct {
	LastInferenceUsage *threadUsage `json:"lastInferenceUsage"`
}

type threadMsg struct {
	Role    string          `json:"role"`
	Content []threadContent `json:"content"`
	State   *threadState    `json:"state"`
	Usage   *threadUsage    `json:"usage"`
	Meta    *threadMeta     `json:"meta"`
}

type threadState struct {
	Type       string `json:"type"`
	StopReason string `json:"stopReason"`
}

type threadMeta struct {
	SentAt int64 `json:"sentAt"`
}

type threadUsage struct {
	Model                    string `json:"model"`
	InputTokens              int    `json:"inputTokens"`
	OutputTokens             int    `json:"outputTokens"`
	CacheCreationInputTokens int    `json:"cacheCreationInputTokens"`
	CacheReadInputTokens     int    `json:"cacheReadInputTokens"`
	Timestamp                string `json:"timestamp"`
}

type threadContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	Name      string          `json:"name"`
	Complete  bool            `json:"complete"`
	Input     json.RawMessage `json:"input"`
	StartTime int64           `json:"startTime"`
	FinalTime int64           `json:"finalTime"`
	Run       *threadRun      `json:"run"`
}

type threadRun struct {
	Status string      `json:"status"`
	Result interface{} `json:"result"`
}

// ParseThread reads a full Amp thread JSON file.
func ParseThread(path, lastThreadID string) (*model.Session, int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, err
	}

	var tf threadFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, 0, err
	}

	session := &model.Session{
		SessionID: tf.ID,
		JSONLPath: path,
		Agent:     "amp",
		Name:      tf.Title,
	}

	if len(tf.Env.Initial.Trees) > 0 {
		session.CWD = decodeFileURI(tf.Env.Initial.Trees[0].URI)
		if ref := tf.Env.Initial.Trees[0].Repository; ref != nil && strings.HasPrefix(ref.Ref, "refs/heads/") {
			session.GitBranch = strings.TrimPrefix(ref.Ref, "refs/heads/")
		}
	}
	if tf.Env.Initial.Platform.ClientVersion != "" {
		session.Version = tf.Env.Initial.Platform.ClientVersion
	}
	if tf.Debug.LastInferenceUsage != nil && tf.Debug.LastInferenceUsage.Model != "" {
		session.Model = tf.Debug.LastInferenceUsage.Model
	}

	lastKind := ""
	lastTool := ""
	lastTs := millisTime(tf.Created)
	for _, msg := range tf.Messages {
		msgTs := messageTimestamp(msg)
		if !msgTs.IsZero() {
			lastTs = msgTs
			session.EntryTimestamps = append(session.EntryTimestamps, msgTs)
			if len(session.EntryTimestamps) > 500 {
				session.EntryTimestamps = session.EntryTimestamps[len(session.EntryTimestamps)-500:]
			}
		}

		switch msg.Role {
		case "user":
			if hasToolResult(msg) {
				lastKind = "tool_result"
			} else {
				session.UserMessages++
				if text := firstAmpText(msg.Content); text != "" {
					appendMessage(session, "user", text, msgTs)
				}
				lastKind = "user"
			}
		case "assistant":
			session.AssistantMessages++
			if msg.Usage != nil {
				if msg.Usage.Model != "" {
					session.Model = msg.Usage.Model
				}
				session.InputTokens += msg.Usage.InputTokens
				session.OutputTokens += msg.Usage.OutputTokens
				session.CacheCreationTokens += msg.Usage.CacheCreationInputTokens
				session.CacheReadTokens += msg.Usage.CacheReadInputTokens
			}
			if text := firstAmpText(msg.Content); text != "" {
				appendMessage(session, "assistant", text, msgTs)
			}
			for _, block := range msg.Content {
				if block.Type != "tool_use" {
					continue
				}
				appendTool(session, normalizeAmpToolName(block.Name), msgTs)
				if isAmpWriteTool(block.Name) {
					if fp := extractAmpFilePath(block.Input); fp != "" {
						session.LastFileWrite = fp
						session.LastFileWriteAt = msgTs
					}
				}
				lastTool = normalizeAmpToolName(block.Name)
			}

			if msg.State != nil && msg.State.StopReason == "tool_use" {
				lastKind = "tool_use"
			} else {
				lastKind = "assistant"
			}
		}
	}

	session.TotalMessages = session.UserMessages + session.AssistantMessages
	if session.TotalMessages == 0 {
		return nil, 0, fmt.Errorf("empty thread (no messages)")
	}
	session.LastActivity = lastTs
	session.Status = ampStatus(lastKind)
	if session.Status == model.StatusExecutingTool {
		session.CurrentTool = lastTool
	}

	if tf.ID == lastThreadID && session.Status == model.StatusWaitingForUser && time.Since(session.LastActivity) < 30*time.Second {
		session.Status = model.StatusThinking
	}

	return session, int64(len(data)), nil
}

func decodeFileURI(uri string) string {
	if strings.HasPrefix(uri, "file://") {
		return strings.TrimPrefix(uri, "file://")
	}
	return uri
}

func millisTime(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func messageTimestamp(msg threadMsg) time.Time {
	if msg.Usage != nil && msg.Usage.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, msg.Usage.Timestamp); err == nil {
			return ts
		}
	}
	if msg.Meta != nil && msg.Meta.SentAt != 0 {
		return millisTime(msg.Meta.SentAt)
	}
	var latest time.Time
	for _, block := range msg.Content {
		if t := blockTimestamp(block); t.After(latest) {
			latest = t
		}
	}
	return latest
}

func blockTimestamp(block threadContent) time.Time {
	if block.FinalTime != 0 {
		return millisTime(block.FinalTime)
	}
	if block.StartTime != 0 {
		return millisTime(block.StartTime)
	}
	return time.Time{}
}

func firstAmpText(blocks []threadContent) string {
	for _, block := range blocks {
		switch block.Type {
		case "text":
			if block.Text != "" {
				return block.Text
			}
		case "thinking":
			if block.Thinking != "" {
				return block.Thinking
			}
		}
	}
	return ""
}

func hasToolResult(msg threadMsg) bool {
	for _, block := range msg.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
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
		Role: role, Text: model.Truncate(text, 300), Timestamp: ts,
	})
	if len(session.RecentMessages) > 10 {
		session.RecentMessages = session.RecentMessages[len(session.RecentMessages)-10:]
	}
}

func normalizeAmpToolName(name string) string {
	switch name {
	case "read_file", "Read":
		return "Read"
	case "bash", "Bash":
		return "Bash"
	case "grep", "Grep":
		return "Grep"
	case "glob", "Glob":
		return "Glob"
	case "edit_file", "Edit":
		return "Edit"
	case "write_file", "Write":
		return "Write"
	case "web_search", "WebSearch":
		return "WebSearch"
	default:
		if name == "" {
			return ""
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func isAmpWriteTool(name string) bool {
	switch name {
	case "edit_file", "write_file", "Edit", "Write":
		return true
	default:
		return false
	}
}

func extractAmpFilePath(raw json.RawMessage) string {
	var obj struct {
		Path string `json:"path"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return obj.Path
	}
	return ""
}

func ampStatus(kind string) model.SessionStatus {
	switch kind {
	case "assistant":
		return model.StatusWaitingForUser
	case "user":
		return model.StatusThinking
	case "tool_use":
		return model.StatusExecutingTool
	case "tool_result":
		return model.StatusProcessingResult
	default:
		return model.StatusIdle
	}
}
