# Grok Agent Integration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add xAI's Grok CLI as a first-class agent in `lazyagent` — discovered in the TUI/GUI/API, searchable, prunable, and compactable — alongside Claude, Codex, Cursor, Amp, pi, and OpenCode.

**Architecture:** A new `internal/grok` provider package, modeled on `internal/pi`, walks `~/.grok/sessions/<encoded-cwd>/<uuid>/` (depth exactly 2), parses each session directory into a `model.Session`, and integrates with `model.SessionCache`. Grok sessions are *directories* (not single JSONL files), so `prune` and `compact` get directory-aware branches. Subagent sessions are hidden by reusing the existing `IsSidechain` filter. Token/cost fields stay zero — Grok's on-disk data exposes no input/output/cache split.

**Tech Stack:** Go 1.x, standard library (`encoding/json`, `net/url`, `path/filepath`, `bufio`), `modernc.org/sqlite` (search index only — Grok's own FTS index is not reused).

---

## Design decisions locked in before implementation

These resolve ambiguities and minor tensions in the spec. They are binding for every task below.

1. **`model.Session.JSONLPath` = the session *directory* absolute path** (spec §4.2 lists this as the first option). `prune` deletes it with `os.RemoveAll`; `compact` treats it as a directory of files. Search builds its own sources and does not use `JSONLPath`.
2. **Cache invalidation key = `chat_history.jsonl`** (its path, mtime, size — via `model.SessionCache`). It is always present and grows on every message, so its mtime captures live activity. `summary.json` is small and re-read on every parse. A directory mtime would *not* catch in-file content changes, so the directory itself is not the key.
3. **`EntryTimestamps` (activity sparkline) = a bounded tail of `updates.jsonl`.** Spec §4.2 maps the sparkline to `updates.jsonl` timestamps; spec §11 risk #6 says never read the *whole* `updates.jsonl` (it can be ~18 MB). A bounded 512 KiB tail satisfies both — it is not "the whole file", and the sparkline window is short so recent timestamps are all that matter.
4. **Subagent sessions** (`summary.json` → `session_kind == "subagent"`) set `IsSidechain = true`. Verified: `core.SessionManager.filterSessionsLocked` (`internal/core/session.go:248`) drops `IsSidechain` sessions, and it backs the TUI (`VisibleSessions`), the tray/GUI (`internal/tray/service.go`), and the HTTP API (`QuerySessions`, `internal/api/server.go`). No new filter code is needed.
5. **Token/cost fields stay zero** for Grok (`InputTokens`, `OutputTokens`, `CacheCreationTokens`, `CacheReadTokens`, `CostUSD`). Grok's data has no input/output/cache split. Documented as a per-agent caveat in Phase 5.
6. **Test fixtures are synthetic temp directories** built by a shared `t.TempDir()` helper — exactly the pattern `internal/pi/process_test.go` uses (the spec names `internal/pi` as the authoritative template). No committed `testdata/` directory is needed; oversized-payload compact fixtures are also generated synthetically.
7. **Resume command for `search` is out of scope.** The spec's integration file list (§5) does not include `internal/search/run.go`'s `resumeCommand`. `openResult` already prints a graceful "No resume command available" message for unknown agents — Grok search results are indexed, displayed, and openable-to-CWD, just not auto-resumed.

---

## File structure

| File | Status | Responsibility |
|------|--------|----------------|
| `internal/grok/types.go` | Create | Go structs for `summary.json`, `chat_history.jsonl`, `signals.json`, `updates.jsonl`. |
| `internal/grok/parse.go` | Create | `ParseGrokSession(dir)` → `*model.Session`; transcript / signals / updates-tail helpers. |
| `internal/grok/process.go` | Create | `GrokSessionsDir()`, `DiscoverSessions(cache)`, `SessionDirs()`, `SessionDiskBytes(dir)`, depth-2 walk, parallel parse. |
| `internal/grok/parse_test.go` | Create | Unit tests for `ParseGrokSession`. |
| `internal/grok/process_test.go` | Create | Unit tests for discovery + the shared `writeGrokSession` test helper. |
| `internal/core/provider.go` | Modify | `GrokProvider` type, `NewGrokProvider()`, `BuildProvider` case, `"all"` inclusion, import. |
| `internal/core/config.go` | Modify | `"grok": true` in `DefaultConfig().Agents`. |
| `internal/core/provider_test.go` | Create/Modify | `BuildProvider("grok", …)` returns a `*GrokProvider`. |
| `main.go` | Modify | `grok` in the `--agent` flag help, usage block, and validation switch. |
| `internal/ui/app.go` | Modify | `G ` prefix for `s.Agent == "grok"`. |
| `internal/search/types.go` | Modify | `"grok"` in `supportedAgents`. |
| `internal/search/extractors.go` | Modify | `listGrokSources`, `extractGrok`, `listSources`/`extractChunks` cases. |
| `internal/search/run.go` | Modify | `grok` in the `--agent` help text. |
| `internal/search/extractors_test.go` | Create | `extractGrok` test. |
| `internal/prune/prune.go` | Modify | `"grok"` in `SupportedAgents`; package doc comment. |
| `internal/prune/delete.go` | Modify | `deleteGrokSession`, `executeDelete` case, `removeEmptyDirs` grok root. |
| `internal/prune/report.go` | Modify | `totalBytes` directory-aware branch for Grok. |
| `internal/prune/delete_test.go` | Create/Modify | `deleteGrokSession` test. |
| `internal/compact/grok.go` | Create | `compactGrokSession`, `estimateGrokSession`, `sessionSizeBytes`, deep-truncation helpers. |
| `internal/compact/compact.go` | Modify | `grok` in `supportedAgents`; `collectCandidates` size branch. |
| `internal/compact/rewrite.go` | Modify | `guardPath` grok root; `estimateSizes`/`executeCompact` grok branches; extract `estimateJSONL`. |
| `internal/compact/grok_test.go` | Create | `compactGrokSession` test (oversized payload). |
| `docs/superpowers/plans/2026-05-20-grok-compact-phase0-findings.md` | Create (Task 7) | Recorded outcome of the compact resume-verification. |
| `docs/concepts/supported-agents.md` | Modify | Table row + per-agent "Grok" note. |
| `docs/maintenance/prune.md`, `compact.md`, `search.md` | Modify | Add Grok to supported-agent lists. |
| `docs/usage/cli.md` | Modify | Add Grok to the `--agent` table and `search`/`compact`/`prune` notes. |

---

## Phase 1 — Core provider

Outcome: Grok sessions visible in TUI / GUI / API. Build stays green.

### Task 1: `internal/grok` types + parser

**Files:**
- Create: `internal/grok/types.go`
- Create: `internal/grok/parse.go`
- Test: `internal/grok/parse_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/grok/parse_test.go`:

```go
package grok

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// writeSession creates a session directory at root/encodedCWD/uuid and writes
// the given files (relative paths → contents) into it. Returns the session dir.
func writeSession(t *testing.T, root, encodedCWD, uuid string, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(root, encodedCWD, uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const primarySummary = `{
  "info": {"id": "019e0000-0000-7000-8000-000000000001", "cwd": "/Users/alice/project"},
  "session_summary": "fix the parser",
  "generated_title": "Fix the JSON parser bug",
  "created_at": "2026-05-17T10:33:00.260953Z",
  "updated_at": "2026-05-17T11:33:49.449038Z",
  "last_active_at": "2026-05-17T11:33:49.449038Z",
  "num_chat_messages": 4,
  "current_model_id": "grok-build",
  "head_branch": "feature/parser",
  "chat_format_version": 1
}`

const primaryChat = `{"type":"system","content":"system prompt"}
{"type":"user","content":[{"type":"text","text":"Hello Grok"}]}
{"type":"assistant","content":"Working on it","tool_calls":[{"id":"call-1","name":"bash","arguments":"{}"}],"model_id":"grok-build"}
{"type":"tool_result","content":"command output","tool_call_id":"call-1"}
`

const primarySignals = `{"userMessageCount":1,"assistantMessageCount":1,"contextTokensUsed":1234}`

func TestParseGrokSession_Fields(t *testing.T) {
	root := t.TempDir()
	dir := writeSession(t, root, "%2FUsers%2Falice%2Fproject", "019e0000-0000-7000-8000-000000000001", map[string]string{
		"summary.json":      primarySummary,
		"chat_history.jsonl": primaryChat,
		"signals.json":      primarySignals,
	})

	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatalf("ParseGrokSession: %v", err)
	}
	if s.Agent != "grok" {
		t.Errorf("Agent = %q, want grok", s.Agent)
	}
	if s.SessionID != "019e0000-0000-7000-8000-000000000001" {
		t.Errorf("SessionID = %q", s.SessionID)
	}
	if s.JSONLPath != dir {
		t.Errorf("JSONLPath = %q, want %q", s.JSONLPath, dir)
	}
	if s.CWD != "/Users/alice/project" {
		t.Errorf("CWD = %q", s.CWD)
	}
	if s.Name != "Fix the JSON parser bug" {
		t.Errorf("Name = %q, want generated_title", s.Name)
	}
	if s.Model != "grok-build" {
		t.Errorf("Model = %q", s.Model)
	}
	if s.GitBranch != "feature/parser" {
		t.Errorf("GitBranch = %q", s.GitBranch)
	}
	if s.TotalMessages != 4 {
		t.Errorf("TotalMessages = %d, want 4", s.TotalMessages)
	}
	if s.UserMessages != 1 || s.AssistantMessages != 1 {
		t.Errorf("counts = (%d,%d), want (1,1)", s.UserMessages, s.AssistantMessages)
	}
	if s.LastActivity.IsZero() {
		t.Error("LastActivity is zero")
	}
	if s.IsSidechain {
		t.Error("primary session must not be a sidechain")
	}
	// Token/cost fields stay zero — Grok exposes no input/output/cache split.
	if s.InputTokens != 0 || s.OutputTokens != 0 || s.CacheReadTokens != 0 ||
		s.CacheCreationTokens != 0 || s.CostUSD != 0 {
		t.Error("token/cost fields must stay zero for Grok")
	}
	if s.Status != model.StatusProcessingResult {
		t.Errorf("Status = %v, want ProcessingResult (last entry is tool_result)", s.Status)
	}
}

func TestParseGrokSession_TitleFallback(t *testing.T) {
	root := t.TempDir()
	// No generated_title, no session_summary → fall back to first user message.
	summary := `{"info":{"id":"x","cwd":"/tmp/p"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z"}`
	chat := `{"type":"user","content":[{"type":"text","text":"Please refactor auth"}]}` + "\n"
	dir := writeSession(t, root, "%2Ftmp%2Fp", "x", map[string]string{
		"summary.json": summary, "chat_history.jsonl": chat,
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Please refactor auth" {
		t.Errorf("Name = %q, want first user message", s.Name)
	}
}

func TestParseGrokSession_CountsFromTranscriptWhenNoSignals(t *testing.T) {
	root := t.TempDir()
	dir := writeSession(t, root, "%2Ftmp%2Fp", "y", map[string]string{
		"summary.json":      primarySummary,
		"chat_history.jsonl": primaryChat,
		// signals.json deliberately absent
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.UserMessages != 1 || s.AssistantMessages != 1 {
		t.Errorf("counts from transcript = (%d,%d), want (1,1)", s.UserMessages, s.AssistantMessages)
	}
}

func TestParseGrokSession_Subagent(t *testing.T) {
	root := t.TempDir()
	summary := `{"info":{"id":"sub","cwd":"/tmp/wt"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z","session_kind":"subagent"}`
	dir := writeSession(t, root, "%2Ftmp%2Fwt", "sub", map[string]string{
		"summary.json": summary, "chat_history.jsonl": "",
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.IsSidechain {
		t.Error("subagent session must have IsSidechain = true")
	}
}

func TestParseGrokSession_UnsupportedFormatVersion(t *testing.T) {
	root := t.TempDir()
	summary := `{"info":{"id":"z","cwd":"/tmp/p"},"chat_format_version":999,
		"updated_at":"2026-05-17T11:00:00Z"}`
	dir := writeSession(t, root, "%2Ftmp%2Fp", "z", map[string]string{
		"summary.json": summary,
	})
	if _, err := ParseGrokSession(dir); err == nil {
		t.Error("expected error for unsupported chat_format_version")
	}
}

func TestParseGrokSession_MissingSummary(t *testing.T) {
	root := t.TempDir()
	dir := writeSession(t, root, "%2Ftmp%2Fp", "w", map[string]string{
		"chat_history.jsonl": primaryChat,
	})
	if _, err := ParseGrokSession(dir); err == nil {
		t.Error("expected error when summary.json is missing")
	}
}

func TestDecodeGrokDirName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"%2FUsers%2Falice%2Fproject", "/Users/alice/project"},
		{"%2Ftmp", "/tmp"},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		if got := decodeGrokDirName(tt.in); got != tt.want {
			t.Errorf("decodeGrokDirName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/grok/...`
Expected: FAIL — `undefined: ParseGrokSession`, `undefined: decodeGrokDirName`.

- [ ] **Step 3: Create `internal/grok/types.go`**

```go
package grok

import "encoding/json"

// grokSummary mirrors the subset of summary.json this package reads.
// Timestamps are RFC3339 UTC strings with microseconds and a trailing Z.
type grokSummary struct {
	Info struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
	SessionSummary    string `json:"session_summary"`
	GeneratedTitle    string `json:"generated_title"`
	CreatedAt         string `json:"created_at"`
	UpdatedAt         string `json:"updated_at"`
	LastActiveAt      string `json:"last_active_at"`
	NumChatMessages   int    `json:"num_chat_messages"`
	CurrentModelID    string `json:"current_model_id"`
	GitRootDir        string `json:"git_root_dir"`
	HeadBranch        string `json:"head_branch"`
	ChatFormatVersion int    `json:"chat_format_version"`
	// SessionKind is present ONLY on subagent sessions ("subagent").
	SessionKind string `json:"session_kind"`
}

// grokChatEntry is one line of chat_history.jsonl. content is a plain string
// for system/assistant/tool_result entries and an array of blocks for user
// entries — kept as RawMessage so the caller picks the right decode path.
type grokChatEntry struct {
	Type       string          `json:"type"` // system | user | assistant | tool_result
	Content    json.RawMessage `json:"content"`
	Reasoning  string          `json:"reasoning"`
	ToolCalls  []grokToolCall  `json:"tool_calls"`
	ToolCallID string          `json:"tool_call_id"`
	ModelID    string          `json:"model_id"`
}

// grokToolCall is one entry of an assistant message's tool_calls[].
type grokToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // a JSON-encoded string
}

// grokContentBlock is one block of a user entry's content array.
type grokContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// grokSignals mirrors the subset of signals.json this package reads.
// signals.json is absent in ~11% of sessions (very short / aborted ones).
type grokSignals struct {
	UserMessageCount      int `json:"userMessageCount"`
	AssistantMessageCount int `json:"assistantMessageCount"`
	ContextTokensUsed     int `json:"contextTokensUsed"`
}

// grokUpdate is the minimal shape of an updates.jsonl line. timestamp is
// Unix epoch seconds (fractional).
type grokUpdate struct {
	Timestamp float64 `json:"timestamp"`
}
```

- [ ] **Step 4: Create `internal/grok/parse.go`**

```go
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
	"strings"
	"time"

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
		for _, b := range blocks {
			if b.Text != "" {
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
	return strings.ToUpper(name[:1]) + name[1:]
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
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/grok/...`
Expected: PASS — all `TestParseGrokSession_*` and `TestDecodeGrokDirName` green.

- [ ] **Step 6: Commit**

```bash
git add internal/grok/types.go internal/grok/parse.go internal/grok/parse_test.go
git commit -m "feat(grok): add session directory parser"
```

---

### Task 2: `internal/grok` discovery

**Files:**
- Create: `internal/grok/process.go`
- Test: `internal/grok/process_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/grok/process_test.go`:

```go
package grok

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestGrokSessionsDir(t *testing.T) {
	dir := GrokSessionsDir()
	if dir == "" {
		t.Fatal("GrokSessionsDir() returned empty string")
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".grok", "sessions")
	if dir != want {
		t.Errorf("GrokSessionsDir() = %q, want %q", dir, want)
	}
}

func TestDiscoverSessions_MissingDir(t *testing.T) {
	sessions, err := discoverSessionsFromDir("/nonexistent/grok/sessions", model.NewSessionCache())
	if err != nil {
		t.Fatalf("missing dir must not error: %v", err)
	}
	if sessions != nil {
		t.Errorf("got %v, want nil", sessions)
	}
}

func TestDiscoverSessions_PrimaryAndSubagent(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "%2FUsers%2Falice%2Fproject", "019e0000-0000-7000-8000-000000000001", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat, "signals.json": primarySignals,
	})
	subSummary := `{"info":{"id":"sub","cwd":"/tmp/wt"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z","session_kind":"subagent"}`
	writeSession(t, root, "%2Ftmp%2Fwt", "sub", map[string]string{
		"summary.json": subSummary, "chat_history.jsonl": "",
	})

	sessions, err := discoverSessionsFromDir(root, model.NewSessionCache())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("got %d sessions, want 2", len(sessions))
	}
	var primary, sub *model.Session
	for _, s := range sessions {
		if s.IsSidechain {
			sub = s
		} else {
			primary = s
		}
	}
	if primary == nil || sub == nil {
		t.Fatal("expected one primary and one subagent session")
	}
	if primary.Agent != "grok" {
		t.Errorf("Agent = %q", primary.Agent)
	}
}

func TestDiscoverSessions_SkipsNonSessionEntries(t *testing.T) {
	root := t.TempDir()
	cwdDir := filepath.Join(root, "%2Ftmp%2Fp")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A cwd-level file and a root-level file must both be ignored.
	if err := os.WriteFile(filepath.Join(cwdDir, "prompt_history.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "session_search.sqlite"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSession(t, root, "%2Ftmp%2Fp", "real", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})

	sessions, err := discoverSessionsFromDir(root, model.NewSessionCache())
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1 (sqlite + prompt_history must be skipped)", len(sessions))
	}
}

func TestDiscoverSessions_MalformedSummarySkipped(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "%2Ftmp%2Fp", "bad", map[string]string{
		"summary.json": "{not json", "chat_history.jsonl": "",
	})
	writeSession(t, root, "%2Ftmp%2Fp", "good", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})
	sessions, err := discoverSessionsFromDir(root, model.NewSessionCache())
	if err != nil {
		t.Fatalf("one bad session must not abort the scan: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
}

func TestDiscoverSessions_CacheHit(t *testing.T) {
	root := t.TempDir()
	writeSession(t, root, "%2Ftmp%2Fp", "c", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})
	cache := model.NewSessionCache()
	first, err := discoverSessionsFromDir(root, cache)
	if err != nil || len(first) != 1 {
		t.Fatalf("first discover: %v, n=%d", err, len(first))
	}
	second, err := discoverSessionsFromDir(root, cache)
	if err != nil || len(second) != 1 {
		t.Fatalf("second discover: %v, n=%d", err, len(second))
	}
	if first[0] != second[0] {
		t.Error("unchanged session should be served from cache (same pointer)")
	}
}

func TestSessionDirsAndDiskBytes(t *testing.T) {
	root := t.TempDir()
	dir := writeSession(t, root, "%2Ftmp%2Fp", "s", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
		"terminal/cmd-1.log": "some output",
	})
	dirs := walkSessionDirs(root)
	if len(dirs) != 1 || dirs[0] != dir {
		t.Fatalf("walkSessionDirs = %v, want [%s]", dirs, dir)
	}
	if got := SessionDiskBytes(dir); got <= 0 {
		t.Errorf("SessionDiskBytes = %d, want > 0", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/grok/...`
Expected: FAIL — `undefined: GrokSessionsDir`, `discoverSessionsFromDir`, `walkSessionDirs`, `SessionDiskBytes`.

- [ ] **Step 3: Create `internal/grok/process.go`**

```go
// Package grok discovers xAI Grok CLI sessions from ~/.grok/sessions.
//
// Grok stores one directory per session, exactly two levels deep:
//
//	~/.grok/sessions/<url-encoded-cwd>/<session-uuid>/
//
// Each session directory carries a summary.json (metadata) and a
// chat_history.jsonl (transcript). This package walks that tree, parses each
// session into a model.Session, and integrates with model.SessionCache so
// unchanged sessions are not re-parsed.
package grok

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/illegalstudio/lazyagent/internal/claude"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// GrokSessionsDir returns the path to ~/.grok/sessions.
func GrokSessionsDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".grok", "sessions")
}

// DiscoverSessions scans ~/.grok/sessions for Grok session directories.
func DiscoverSessions(cache *model.SessionCache) ([]*model.Session, error) {
	return discoverSessionsFromDir(GrokSessionsDir(), cache)
}

// SessionDirs returns every Grok session directory on disk. Used by the
// search indexer and maintenance commands that need the raw directory list.
func SessionDirs() []string {
	return walkSessionDirs(GrokSessionsDir())
}

// SessionDiskBytes returns the total size in bytes of every file inside a
// Grok session directory. Best-effort: unreadable entries contribute zero.
func SessionDiskBytes(dir string) int64 {
	var total int64
	_ = filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if info, err := d.Info(); err == nil {
			total += info.Size()
		}
		return nil
	})
	return total
}

type wtInfo struct {
	isWorktree bool
	mainRepo   string
}

type parseJob struct {
	sessionDir string
	cacheKey   string // chat_history.jsonl path — the cache invalidation key
	mtime      time.Time
}

type parseResult struct {
	session  *model.Session
	cacheKey string
	mtime    time.Time
}

// walkSessionDirs returns every session directory under sessionsDir: every
// depth-2 directory that contains a summary.json. Files at the root
// (session_search.sqlite) and at cwd level (prompt_history.jsonl) are skipped
// because only directories are descended into.
func walkSessionDirs(sessionsDir string) []string {
	if sessionsDir == "" {
		return nil
	}
	cwdEntries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var dirs []string
	for _, cwdEntry := range cwdEntries {
		if !cwdEntry.IsDir() {
			continue
		}
		cwdPath := filepath.Join(sessionsDir, cwdEntry.Name())
		sessEntries, err := os.ReadDir(cwdPath)
		if err != nil {
			continue
		}
		for _, sessEntry := range sessEntries {
			if !sessEntry.IsDir() {
				continue
			}
			sessionDir := filepath.Join(cwdPath, sessEntry.Name())
			if _, err := os.Stat(filepath.Join(sessionDir, "summary.json")); err != nil {
				continue // not a session directory
			}
			dirs = append(dirs, sessionDir)
		}
	}
	return dirs
}

// discoverSessionsFromDir scans a Grok sessions root and returns parsed
// sessions. A missing root is not an error (Grok not installed → nil, nil).
func discoverSessionsFromDir(sessionsDir string, cache *model.SessionCache) ([]*model.Session, error) {
	if sessionsDir == "" {
		return nil, nil
	}
	if _, err := os.Stat(sessionsDir); err != nil {
		if os.IsNotExist(err) {
			return nil, nil // Grok not installed — not an error.
		}
		return nil, fmt.Errorf("could not read grok sessions dir: %w", err)
	}

	wtCache := make(map[string]wtInfo)
	seen := make(map[string]struct{})
	var sessions []*model.Session
	var jobs []parseJob

	// Phase 1: classify each session as cache-hit or needs-parse.
	for _, sessionDir := range walkSessionDirs(sessionsDir) {
		cacheKey := filepath.Join(sessionDir, "chat_history.jsonl")
		seen[cacheKey] = struct{}{}
		cached, offset, mtime := cache.GetIncremental(cacheKey)
		// A full cache hit (file unchanged) reuses the parsed session. Any
		// change — including the offset>0 "file grew" case — triggers a full
		// re-parse, because Grok parsing reads summary.json + the whole
		// chat_history.jsonl rather than appending incrementally.
		if cached != nil && offset == 0 {
			sessions = append(sessions, cached)
			continue
		}
		jobs = append(jobs, parseJob{sessionDir: sessionDir, cacheKey: cacheKey, mtime: mtime})
	}

	if len(jobs) > 0 {
		// Phase 2: parse sessions in parallel.
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
					session, err := ParseGrokSession(j.sessionDir)
					if err != nil {
						continue // skip malformed / unsupported session
					}
					results[idx] = parseResult{session: session, cacheKey: j.cacheKey, mtime: j.mtime}
				}
			}()
		}
		wg.Wait()

		// Phase 3: enrich worktree info and update the cache (sequential).
		for _, r := range results {
			if r.session == nil {
				continue
			}
			if r.session.CWD != "" {
				if _, ok := wtCache[r.session.CWD]; !ok {
					isWT, mainRepo := claude.IsWorktree(r.session.CWD)
					wtCache[r.session.CWD] = wtInfo{isWorktree: isWT, mainRepo: mainRepo}
				}
				wt := wtCache[r.session.CWD]
				r.session.IsWorktree = wt.isWorktree
				r.session.MainRepo = wt.mainRepo
			}
			// size 0 forces a full re-parse on any future mtime change.
			cache.Put(r.cacheKey, r.mtime, 0, r.session)
			sessions = append(sessions, r.session)
		}
	}

	cache.Prune(seen)
	return sessions, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/grok/...`
Expected: PASS — all discovery tests green.

- [ ] **Step 5: Run the full build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/grok/process.go internal/grok/process_test.go
git commit -m "feat(grok): add session discovery with cache + parallel parse"
```

---

### Task 3: Provider wiring + config default

**Files:**
- Modify: `internal/core/provider.go`
- Modify: `internal/core/config.go:50-57`
- Test: `internal/core/provider_test.go`

- [ ] **Step 1: Write the failing test**

Create (or append to) `internal/core/provider_test.go`:

```go
package core

import "testing"

func TestBuildProvider_Grok(t *testing.T) {
	p := BuildProvider("grok", DefaultConfig())
	if _, ok := p.(*GrokProvider); !ok {
		t.Fatalf("BuildProvider(\"grok\") = %T, want *GrokProvider", p)
	}
}

func TestDefaultConfig_GrokEnabled(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.AgentEnabled("grok") {
		t.Error("grok must be enabled by default")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/core/ -run 'Grok'`
Expected: FAIL — `undefined: GrokProvider`.

- [ ] **Step 3: Add `"grok": true` to `DefaultConfig().Agents`**

In `internal/core/config.go`, change the `Agents` map literal in `DefaultConfig()` (lines 50-57) to:

```go
		Agents: map[string]bool{
			"claude":   true,
			"pi":       true,
			"opencode": true,
			"cursor":   true,
			"codex":    true,
			"amp":      true,
			"grok":     true,
		},
```

(`LoadConfig` already backfills missing keys for existing config files via its loop over `defaults.Agents`, so older installs pick `grok` up automatically.)

- [ ] **Step 4: Add the `GrokProvider` to `internal/core/provider.go`**

Add `"github.com/illegalstudio/lazyagent/internal/grok"` to the import block (keep imports alphabetically ordered — it goes after `cursor`, before `model`).

Add the provider type after `AmpProvider` (after line 156), modeled exactly on `PiProvider`:

```go
// GrokProvider discovers xAI Grok CLI sessions from disk.
type GrokProvider struct {
	cache *model.SessionCache
}

// NewGrokProvider creates a GrokProvider with an mtime-based cache.
func NewGrokProvider() *GrokProvider {
	return &GrokProvider{cache: model.NewSessionCache()}
}

func (p *GrokProvider) DiscoverSessions() ([]*model.Session, error) {
	return grok.DiscoverSessions(p.cache)
}

func (p *GrokProvider) UseWatcher() bool               { return true }
func (p *GrokProvider) RefreshInterval() time.Duration { return 0 }
func (p *GrokProvider) WatchDirs() []string            { return []string{grok.GrokSessionsDir()} }
```

In `BuildProvider`, add a case after `case "amp":` (line 174):

```go
	case "grok":
		return NewGrokProvider()
```

In the `default: // "all"` branch, add after the `amp` block (after line 194):

```go
		if cfg.AgentEnabled("grok") {
			providers = append(providers, NewGrokProvider())
		}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `go test ./internal/core/ -run 'Grok'`
Expected: PASS.

- [ ] **Step 6: Run the full build + test**

Run: `go build ./... && go test ./internal/core/... ./internal/grok/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/core/provider.go internal/core/config.go internal/core/provider_test.go
git commit -m "feat(grok): wire GrokProvider into BuildProvider and config"
```

---

### Task 4: CLI flag + TUI prefix

**Files:**
- Modify: `main.go:57`, `main.go:63-70`, `main.go:121`, `main.go:124`
- Modify: `internal/ui/app.go:828-837`

- [ ] **Step 1: Update the `--agent` flag in `main.go`**

Change the flag definition (line 57):

```go
	agentMode := flag.String("agent", "all", "Which agent sessions to show: claude, pi, opencode, cursor, codex, amp, grok, all (default: all)")
```

Add a usage example line in the help block — after the `amp` line (line 69), before the `all` line:

```
  lazyagent --agent grok        Monitor only Grok CLI sessions
```

Change the validation switch (line 121):

```go
		case "claude", "pi", "opencode", "cursor", "codex", "amp", "grok", "all":
```

Change the error message (line 124):

```go
			fmt.Fprintf(os.Stderr, "Error: unknown --agent value %q (use claude, pi, opencode, cursor, codex, amp, grok, or all)\n", *agentMode)
```

- [ ] **Step 2: Add the `G` prefix in `internal/ui/app.go`**

In the agent-prefix chain (lines 828-837), add a `grok` branch after the `cursor` branch:

```go
	agentPrefix := ""
	if s.Agent == "pi" {
		agentPrefix = "π "
	} else if s.Agent == "opencode" {
		agentPrefix = "O "
	} else if s.Agent == "cursor" {
		agentPrefix = "C "
	} else if s.Agent == "grok" {
		agentPrefix = "G "
	} else if s.Desktop != nil {
		agentPrefix = "D "
	}
```

- [ ] **Step 3: Build and verify**

Run: `go build ./... && go vet ./...`
Expected: no errors.

Run: `./lazyagent --agent grok` (after `go build -o lazyagent .`) — if Grok is installed, Grok sessions appear with a `G ` prefix; if not, an empty list (no error). Run `./lazyagent --agent bogus` and confirm the error message now lists `grok`.

- [ ] **Step 4: Verify the GUI panel**

The macOS GUI panel and the HTTP API consume `model.Session` generically via `SessionManager`, so a Grok session surfaces with no GUI-specific change. Grep for agent-badge logic in `internal/tray/` and `internal/assets/`:

Run: `grep -rn '"pi"\|"codex"\|agentPrefix\|Agent ==' internal/tray internal/assets`

If a GUI-side agent-badge map exists, add a `grok → "G"` entry there. If none exists (expected), no change — note it in the commit body.

- [ ] **Step 5: Commit**

```bash
git add main.go internal/ui/app.go
git commit -m "feat(grok): add --agent grok flag and TUI G prefix"
```

---

## Phase 2 — Search

Outcome: `lazyagent search` indexes and finds Grok transcripts. Depends on Phase 1.

### Task 5: `search` Grok support

**Files:**
- Modify: `internal/search/types.go:5`
- Modify: `internal/search/extractors.go`
- Modify: `internal/search/run.go:32`, `run.go:39-44`
- Test: `internal/search/extractors_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/search/extractors_test.go`:

```go
package search

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExtractGrok(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "%2Ftmp%2Fp", "sess-1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	summary := `{"info":{"id":"sess-1","cwd":"/tmp/p"},"generated_title":"Parser work"}`
	chat := `{"type":"system","content":"ignore me"}
{"type":"user","content":[{"type":"text","text":"find the parser bug"}]}
{"type":"assistant","content":"looking into the parser now"}
{"type":"tool_result","content":"grep matched parser.go"}
`
	if err := os.WriteFile(filepath.Join(sessionDir, "summary.json"), []byte(summary), 0o644); err != nil {
		t.Fatal(err)
	}
	chatPath := filepath.Join(sessionDir, "chat_history.jsonl")
	if err := os.WriteFile(chatPath, []byte(chat), 0o644); err != nil {
		t.Fatal(err)
	}

	src, ok := fileSource("grok", "sess-1", chatPath)
	if !ok {
		t.Fatal("fileSource failed")
	}
	chunks, err := extractGrok(src)
	if err != nil {
		t.Fatalf("extractGrok: %v", err)
	}
	// user + assistant + tool_result = 3 chunks; system is skipped.
	if len(chunks) != 3 {
		t.Fatalf("got %d chunks, want 3", len(chunks))
	}
	for _, c := range chunks {
		if c.SessionID != "sess-1" {
			t.Errorf("SessionID = %q", c.SessionID)
		}
		if c.CWD != "/tmp/p" {
			t.Errorf("CWD = %q", c.CWD)
		}
		if c.Name != "Parser work" {
			t.Errorf("Name = %q", c.Name)
		}
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/search/ -run TestExtractGrok`
Expected: FAIL — `undefined: extractGrok`.

- [ ] **Step 3: Add `"grok"` to `supportedAgents`**

In `internal/search/types.go`, change line 5:

```go
var supportedAgents = []string{"claude", "codex", "pi", "amp", "grok"}
```

- [ ] **Step 4: Add `listGrokSources`, `extractGrok`, and the switch cases**

In `internal/search/extractors.go`, add `"github.com/illegalstudio/lazyagent/internal/grok"` to the import block (alphabetical — after `core`, before `pi`).

Add a `grok` case to `listSources` (after the `amp` case, line 100):

```go
	case "grok":
		return listGrokSources(), nil
```

Add a `grok` case to `extractChunks` (after the `amp` case, line 149):

```go
	case "grok":
		return extractGrok(src)
```

Add these functions at the end of `extractors.go`:

```go
// grokSummaryLite is the subset of a Grok summary.json the indexer needs.
type grokSummaryLite struct {
	Info struct {
		ID  string `json:"id"`
		CWD string `json:"cwd"`
	} `json:"info"`
	GeneratedTitle string `json:"generated_title"`
	SessionSummary string `json:"session_summary"`
}

type grokChatLine struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

// listGrokSources returns one source per Grok session, keyed on its
// chat_history.jsonl file (the transcript the indexer scans).
func listGrokSources() []sourceState {
	var out []sourceState
	for _, dir := range grok.SessionDirs() {
		path := filepath.Join(dir, "chat_history.jsonl")
		if src, ok := fileSource("grok", filepath.Base(dir), path); ok {
			out = append(out, src)
		}
	}
	return out
}

// extractGrok turns one Grok session's chat_history.jsonl into search chunks.
// CWD, title and the canonical session ID come from the sibling summary.json.
func extractGrok(src sourceState) ([]chunk, error) {
	sessionDir := filepath.Dir(src.Path)
	sessionID := src.ID
	var cwd, name string
	if data, err := os.ReadFile(filepath.Join(sessionDir, "summary.json")); err == nil {
		var sum grokSummaryLite
		if json.Unmarshal(data, &sum) == nil {
			if sum.Info.ID != "" {
				sessionID = sum.Info.ID
			}
			cwd = sum.Info.CWD
			name = sum.GeneratedTitle
			if name == "" {
				name = sum.SessionSummary
			}
		}
	}
	allowed := map[string]bool{"text": true}
	var chunks []chunk
	err := scanJSONL(src.Path, func(line []byte) {
		var e grokChatLine
		if json.Unmarshal(line, &e) != nil {
			return
		}
		if e.Type != "user" && e.Type != "assistant" && e.Type != "tool_result" {
			return
		}
		// contentText handles both the user array form and the plain-string
		// form used by assistant/tool_result entries.
		text := contentText(e.Content, allowed)
		chunks = appendChunk(chunks, src, sessionID, cwd, name, e.Type, time.Time{}, text)
	})
	return chunks, err
}
```

- [ ] **Step 5: Update the `search --agent` help text**

In `internal/search/run.go`, change the flag (line 32):

```go
	fs.StringVar(&opts.agent, "agent", "all", "Agent to search: claude,codex,pi,amp,grok,all")
```

And the usage example block (around line 43) — add a line after the `--agent codex` example:

```
  lazyagent search --agent grok "parser bug"
```

- [ ] **Step 6: Run the test + build**

Run: `go test ./internal/search/... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/search/types.go internal/search/extractors.go internal/search/run.go internal/search/extractors_test.go
git commit -m "feat(grok): add Grok transcript indexing to search"
```

---

## Phase 3 — Prune

Outcome: `lazyagent prune` can delete Grok sessions (directory-aware). Depends on Phase 1.

### Task 6: `prune` Grok support

**Files:**
- Modify: `internal/prune/prune.go:1-8` (package doc), `prune.go:24`
- Modify: `internal/prune/delete.go`
- Modify: `internal/prune/report.go:141-154`
- Test: `internal/prune/delete_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/prune/delete_test.go`:

```go
package prune

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestDeleteGrokSession(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "%2Ftmp%2Fp", "sess-1")
	if err := os.MkdirAll(filepath.Join(sessionDir, "terminal"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"summary.json", "chat_history.jsonl", "updates.jsonl", "terminal/cmd-1.log"} {
		if err := os.WriteFile(filepath.Join(sessionDir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	s := &model.Session{Agent: "grok", JSONLPath: sessionDir}
	if err := deleteGrokSession(s, root); err != nil {
		t.Fatalf("deleteGrokSession: %v", err)
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Error("session directory should be gone")
	}
}

func TestDeleteGrokSession_RejectsOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir() // a different root
	s := &model.Session{Agent: "grok", JSONLPath: outside}
	if err := deleteGrokSession(s, root); err == nil {
		t.Error("expected error deleting a directory outside the grok root")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/prune/ -run Grok`
Expected: FAIL — `undefined: deleteGrokSession`.

- [ ] **Step 3: Add `"grok"` to `SupportedAgents` and update the package doc**

In `internal/prune/prune.go`, change line 24:

```go
var SupportedAgents = []string{"claude", "pi", "codex", "grok"}
```

Update the package doc comment (lines 1-8) so the supported set is accurate:

```go
// Package prune implements the `lazyagent prune` subcommand, which deletes
// old or orphaned chat sessions from supported coding agents.
//
// Supported agents (v1): claude, pi, codex, grok. Amp is skipped because local
// thread files are re-synced from the remote. Cursor and OpenCode store
// sessions inside SQLite databases owned by third-party apps; deleting rows
// there is deferred to a future version.
package prune
```

- [ ] **Step 4: Add directory-aware deletion in `internal/prune/delete.go`**

Add `"github.com/illegalstudio/lazyagent/internal/grok"` to the import block (alphabetical — after `core`, before `model`).

In `executeDelete`, add `grokRoot` next to the other roots (after line 27):

```go
	grokRoot := grok.GrokSessionsDir()
```

Add a `grok` case to the per-session switch (after the `codex` case, line 54):

```go
		case "grok":
			err = deleteGrokSession(s, grokRoot)
			if err == nil && s.JSONLPath != "" {
				dirsToGC[filepath.Dir(s.JSONLPath)] = struct{}{}
			}
```

Add the deletion function after `deleteCodexSession` (after line 113):

```go
// deleteGrokSession removes an entire Grok session directory. A Grok session
// is a directory tree (summary.json, chat_history.jsonl, updates.jsonl,
// terminal/, …), so it is deleted recursively rather than as a single file.
func deleteGrokSession(s *model.Session, root string) error {
	if root == "" {
		return fmt.Errorf("grok sessions directory not found")
	}
	if err := chatops.EnsureWithin(s.JSONLPath, []string{root}); err != nil {
		return err
	}
	return os.RemoveAll(s.JSONLPath)
}
```

Update `removeEmptyDirs` to accept and consider the Grok root. Change its signature and body (lines 214-238):

```go
// removeEmptyDirs deletes any project directory in dirs that is now empty.
// Only directories that sit directly inside one of the known agent roots are
// removed, never the roots themselves.
func removeEmptyDirs(dirs map[string]struct{}, claudeRoots []string, piRoot, codexRoot, grokRoot string) {
	roots := make([]string, 0, len(claudeRoots)+3)
	roots = append(roots, claudeRoots...)
	for _, r := range []string{piRoot, codexRoot, grokRoot} {
		if r != "" {
			roots = append(roots, r)
		}
	}

	for dir := range dirs {
		absDir, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if !chatops.IsBelowAny(absDir, roots) {
			continue
		}
		entries, err := os.ReadDir(absDir)
		if err != nil || len(entries) > 0 {
			continue
		}
		_ = os.Remove(absDir)
	}
}
```

Update the call site in `executeDelete` (line 75):

```go
	removeEmptyDirs(dirsToGC, claudeRoots, piRoot, codexRoot, grokRoot)
```

> **Note:** a Grok `<encoded-cwd>/` directory also holds a cwd-level
> `prompt_history.jsonl`, so it will not be empty after its last session is
> pruned and `removeEmptyDirs` will (correctly) leave it. `prompt_history.jsonl`
> is shared cwd state, not per-session, so prune never deletes it. This matches
> the spec's "cosmetic, match existing behavior" stance (risk #7).

- [ ] **Step 5: Make the prune report directory-aware**

In `internal/prune/report.go`, add `"github.com/illegalstudio/lazyagent/internal/grok"` to the import block (alphabetical — after `chatops`). Change `totalBytes` (lines 141-154):

```go
// totalBytes sums each candidate's on-disk size. For Grok the candidate is a
// directory, so its whole tree is measured; for the JSONL-per-session agents
// a single file is stat-ed. Missing or unreadable paths contribute zero.
func totalBytes(candidates []Candidate) int64 {
	var total int64
	for _, c := range candidates {
		if c.Session.JSONLPath == "" {
			continue
		}
		if c.Session.Agent == "grok" {
			total += grok.SessionDiskBytes(c.Session.JSONLPath)
			continue
		}
		info, err := os.Stat(c.Session.JSONLPath)
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total
}
```

- [ ] **Step 6: Run the test + build**

Run: `go test ./internal/prune/... && go build ./...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/prune/prune.go internal/prune/delete.go internal/prune/report.go internal/prune/delete_test.go
git commit -m "feat(grok): add directory-aware Grok pruning"
```

---

## Phase 4 — Compact

Outcome: `lazyagent compact` shrinks Grok sessions. Depends on Phase 1. **Internally gated on Task 7 (Phase 0).**

### Task 7: Phase 0 — empirical resume verification

This task writes **no production code**. It determines which files inside a Grok
session can be truncated without breaking `grok` resume. Its output is a
findings note that Task 8 depends on. **Do not start Task 8 until this note
exists and records a clear verdict.**

**File:**
- Create: `docs/superpowers/plans/2026-05-20-grok-compact-phase0-findings.md`

- [ ] **Step 1: Confirm Grok is installed with at least one disposable session**

Run: `ls ~/.grok/sessions` and confirm at least one `<encoded-cwd>/<uuid>/` session directory exists. If Grok is not installed or there is no disposable session, **stop** — this task requires a live Grok install. Record "blocked: no Grok install" in the findings note and escalate; Task 8 cannot proceed safely.

- [ ] **Step 2: Pick a disposable session and snapshot it**

Choose a closed, non-important session directory. Copy the whole `~/.grok/sessions/<encoded-cwd>/` tree to a scratch location so the original is recoverable.

- [ ] **Step 3: Baseline — resume the untouched copy**

From the session's `cwd`, run the Grok CLI's resume command for that session ID (consult `grok --help`; the limits code confirms the binary is `grok`). Confirm the session loads and shows prior context. Record the exact resume command used.

- [ ] **Step 4: Test truncating `updates.jsonl`**

On a fresh copy, replace large string values in `updates.jsonl` with short markers (or delete the file's bulk entirely). Resume again. Record: does the session still load and continue coherently? Hypothesis: yes — `updates.jsonl` is a render/telemetry stream not replayed on resume.

- [ ] **Step 5: Test truncating `chat_history.jsonl` `tool_result.content`**

On a fresh copy, truncate the `content` strings of `tool_result` lines in `chat_history.jsonl` (keep every line, keep `tool_call_id`). Resume. Record: does it load? Hypothesis: yes — this is the model-facing transcript; truncating tool output changes what the model sees, exactly like Claude compact, which is the intended behavior.

- [ ] **Step 6: Test truncating `terminal/*.log` and `rewind_points.jsonl`**

On fresh copies, truncate `terminal/*.log` files and the embedded file snapshots in `rewind_points.jsonl`. Resume each. Record results. Note explicitly that gutting `rewind_points.jsonl` disables Grok's rewind feature for that session.

- [ ] **Step 7: Write the findings note**

Create `docs/superpowers/plans/2026-05-20-grok-compact-phase0-findings.md` with, for each of the four files (`updates.jsonl`, `chat_history.jsonl` tool_result, `terminal/*.log`, `rewind_points.jsonl`): **SAFE to truncate** / **UNSAFE** / **SAFE with caveat**, plus the resume command used and any surprising cross-references observed.

The verdict drives Task 8:
- Each file marked **SAFE** → keep its handling in Task 8.
- A file marked **UNSAFE** → remove it from `grokBulkJSONL` (or skip the terminal-log handling) in Task 8 and note the narrowed scope.
- If resume turns out to cross-reference `updates.jsonl` and `chat_history.jsonl` for consistency → narrow Task 8 to whatever is provably safe, even if that means only `terminal/*.log`. A smaller, correct compact beats a broad, corrupting one.

- [ ] **Step 8: Commit the findings note**

```bash
git add docs/superpowers/plans/2026-05-20-grok-compact-phase0-findings.md
git commit -m "docs(grok): record compact Phase 0 resume-verification findings"
```

---

### Task 8: `compact` Grok rewriter

Implement the directory-aware Grok compactor. **The `grokBulkJSONL` list and the
terminal-log handling below assume all four files from Task 7 came back SAFE.
Before writing code, open the Phase 0 findings note and remove any file marked
UNSAFE.**

**Files:**
- Create: `internal/compact/grok.go`
- Modify: `internal/compact/compact.go:24-28`, `compact.go:264-271`
- Modify: `internal/compact/rewrite.go` (`guardPath`, `estimateRewrite`, `estimateSizes`, `executeCompact`)
- Test: `internal/compact/grok_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/compact/grok_test.go`:

```go
package compact

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeGrokSessionForCompact builds a Grok session directory with an
// oversized tool-output payload in updates.jsonl.
func writeGrokSessionForCompact(t *testing.T, big string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "sess-1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"summary.json":       `{"info":{"id":"sess-1","cwd":"/tmp/p"},"chat_format_version":1}`,
		"chat_history.jsonl": `{"type":"tool_result","content":` + jsonString(big) + `,"tool_call_id":"call-1"}` + "\n",
		"updates.jsonl": `{"timestamp":1,"method":"x","params":{"text":` + jsonString(big) + `}}` + "\n" +
			`{"timestamp":2,"method":"y","params":{"small":"ok"}}` + "\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for sc.Scan() {
		n++
		var v any
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			t.Fatalf("line %d of %s is not valid JSON: %v", n, path, err)
		}
	}
	return n
}

func TestCompactGrokSession(t *testing.T) {
	big := strings.Repeat("A", 50*1024) // 50 KiB — well above the 10 KiB threshold
	dir := writeGrokSessionForCompact(t, big)

	before := grokDirSize(t, dir)
	newSize, err := compactGrokSession(dir, 10*1024, true)
	if err != nil {
		t.Fatalf("compactGrokSession: %v", err)
	}
	if newSize >= before {
		t.Errorf("size did not shrink: before=%d after=%d", before, newSize)
	}
	// Every rewritten JSONL file must stay valid and keep its line count.
	if n := countLines(t, filepath.Join(dir, "updates.jsonl")); n != 2 {
		t.Errorf("updates.jsonl line count = %d, want 2", n)
	}
	if n := countLines(t, filepath.Join(dir, "chat_history.jsonl")); n != 1 {
		t.Errorf("chat_history.jsonl line count = %d, want 1", n)
	}
	// tool_call_id linkage must survive truncation.
	data, _ := os.ReadFile(filepath.Join(dir, "chat_history.jsonl"))
	if !strings.Contains(string(data), `"tool_call_id":"call-1"`) {
		t.Error("tool_call_id linkage was lost")
	}
	// Backups were requested.
	if _, err := os.Stat(filepath.Join(dir, "updates.jsonl.bak")); err != nil {
		t.Error("expected updates.jsonl.bak backup")
	}
}

func grokDirSize(t *testing.T, dir string) int64 {
	t.Helper()
	var total int64
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			total += info.Size()
		}
	}
	return total
}

func TestEstimateGrokSession(t *testing.T) {
	big := strings.Repeat("B", 50*1024)
	dir := writeGrokSessionForCompact(t, big)
	before := grokDirSize(t, dir)
	after, err := estimateGrokSession(dir, 10*1024)
	if err != nil {
		t.Fatalf("estimateGrokSession: %v", err)
	}
	if after >= before {
		t.Errorf("estimate did not shrink: before=%d after=%d", before, after)
	}
	// Estimation must not modify any file.
	if grokDirSize(t, dir) != before {
		t.Error("estimateGrokSession modified the session on disk")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/compact/ -run Grok`
Expected: FAIL — `undefined: compactGrokSession`, `estimateGrokSession`.

- [ ] **Step 3: Extract a reusable `estimateJSONL` in `internal/compact/rewrite.go`**

`estimateRewrite` currently bakes in `mutatorFor(agent)`. Grok needs a different mutator per file, so factor the scan loop out. Replace `estimateRewrite` (lines 153-186) with:

```go
// estimateRewrite runs the agent's mutator in memory without writing anything
// and returns the projected post-rewrite file size. Used by --dry-run.
func estimateRewrite(path, agent string, threshold int64) (int64, error) {
	return estimateJSONL(path, mutatorFor(agent), threshold)
}

// estimateJSONL simulates a per-line rewrite with the given mutator and
// returns the projected file size, writing nothing.
func estimateJSONL(path string, mutator lineMutator, threshold int64) (int64, error) {
	in, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer in.Close()

	var total int64
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 1024*1024), 64*1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		var entry map[string]any
		if err := json.Unmarshal(raw, &entry); err != nil {
			total += int64(len(raw)) + 1
			continue
		}
		mutator(entry, threshold)
		encoded, err := json.Marshal(entry)
		if err != nil {
			total += int64(len(raw)) + 1
			continue
		}
		total += int64(len(encoded)) + 1
	}
	if err := scanner.Err(); err != nil {
		return 0, err
	}
	return total, nil
}
```

- [ ] **Step 4: Add the Grok root to `guardPath`**

In `internal/compact/rewrite.go`, add `"github.com/illegalstudio/lazyagent/internal/grok"` to the import block (alphabetical — after `core`, before `pi`). In `guardPath` (lines 202-214), add the Grok root:

```go
// guardPath blocks compacting anything outside the known agent roots.
func guardPath(path string) error {
	cfg := core.LoadConfig()
	var roots []string
	roots = append(roots, claude.ClaudeProjectsDirs(cfg.ClaudeDirs)...)
	if d := pi.PiSessionsDir(); d != "" {
		roots = append(roots, d)
	}
	if d := codex.SessionsDir(); d != "" {
		roots = append(roots, d)
	}
	if d := grok.GrokSessionsDir(); d != "" {
		roots = append(roots, d)
	}
	return chatops.EnsureWithin(path, roots)
}
```

- [ ] **Step 5: Create `internal/compact/grok.go`**

> **Phase 0 gate:** `grokBulkJSONL` and the `terminal/*.log` block below assume
> Task 7 returned all four files SAFE. Remove any file the findings note marked
> UNSAFE before continuing.

```go
package compact

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/illegalstudio/lazyagent/internal/grok"
	"github.com/illegalstudio/lazyagent/internal/model"
)

// grokBulkJSONL lists the JSONL files inside a Grok session directory whose
// oversized payloads compact truncates. Phase 0 resume-verification confirmed
// these are safe to shrink (see 2026-05-20-grok-compact-phase0-findings.md):
//
//   - updates.jsonl       — ACP render/telemetry stream, ~70% of session size,
//                           not replayed on resume → deep string truncation.
//   - chat_history.jsonl  — model-facing transcript; only tool_result.content
//                           is truncated (same trade-off as Claude compact).
//   - rewind_points.jsonl — checkpoint snapshots; truncating disables Grok's
//                           rewind feature for the session.
var grokBulkJSONL = []string{"updates.jsonl", "chat_history.jsonl", "rewind_points.jsonl"}

// sessionSizeBytes returns the on-disk size of a session: a directory total
// for Grok, a single file size for the JSONL-per-session agents.
func sessionSizeBytes(s *model.Session) int64 {
	if s.Agent == "grok" {
		return grok.SessionDiskBytes(s.JSONLPath)
	}
	info, err := os.Stat(s.JSONLPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

// grokFileMutator returns the per-line mutator for a Grok session file.
func grokFileMutator(name string) lineMutator {
	if name == "chat_history.jsonl" {
		return compactGrokChatLine
	}
	// updates.jsonl, rewind_points.jsonl: not replayed on resume, so every
	// oversized string leaf can be truncated.
	return func(entry map[string]any, threshold int64) int64 {
		return truncateGrokDeep(entry, threshold)
	}
}

// compactGrokChatLine truncates the content of a tool_result entry. The
// tool_call_id ↔ tool_calls[].id linkage is untouched so the transcript stays
// internally consistent and resumable.
func compactGrokChatLine(entry map[string]any, threshold int64) int64 {
	if entry["type"] != "tool_result" {
		return 0
	}
	s, ok := entry["content"].(string)
	if !ok {
		return 0
	}
	if newVal, delta := truncateString(s, threshold); delta > 0 {
		entry["content"] = newVal
		return delta
	}
	return 0
}

// truncateGrokDeep recursively truncates every oversized string value in a
// decoded JSON structure. Used for files Grok does not replay on resume.
func truncateGrokDeep(v any, threshold int64) int64 {
	var saved int64
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if s, ok := child.(string); ok {
				if newVal, delta := truncateString(s, threshold); delta > 0 {
					t[k] = newVal
					saved += delta
				}
				continue
			}
			saved += truncateGrokDeep(child, threshold)
		}
	case []any:
		for _, child := range t {
			saved += truncateGrokDeep(child, threshold)
		}
	}
	return saved
}

// grokTerminalLogs returns the terminal/*.log paths inside a session dir.
func grokTerminalLogs(dir string) []string {
	logs, _ := filepath.Glob(filepath.Join(dir, "terminal", "*.log"))
	return logs
}

// compactGrokSession truncates oversized payloads across the bulky files of a
// Grok session directory and returns the directory's new total size.
func compactGrokSession(dir string, threshold int64, backup bool) (int64, error) {
	if err := guardPath(dir); err != nil {
		return 0, err
	}
	for _, name := range grokBulkJSONL {
		path := filepath.Join(dir, name)
		if _, err := os.Stat(path); err != nil {
			continue // file absent for this session
		}
		if _, err := rewriteFile(path, grokFileMutator(name), threshold, backup); err != nil {
			return 0, fmt.Errorf("%s: %w", name, err)
		}
	}
	for _, log := range grokTerminalLogs(dir) {
		if err := compactGrokTerminalLog(log, threshold, backup); err != nil {
			return 0, fmt.Errorf("%s: %w", filepath.Base(log), err)
		}
	}
	return grok.SessionDiskBytes(dir), nil
}

// compactGrokTerminalLog truncates a raw (non-JSON) terminal capture log.
func compactGrokTerminalLog(path string, threshold int64, backup bool) error {
	if err := guardPath(path); err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Size() <= threshold {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	newVal, delta := truncateString(string(data), threshold)
	if delta <= 0 {
		return nil
	}
	if backup {
		if err := copyFile(path, path+".bak"); err != nil {
			return fmt.Errorf("backup: %w", err)
		}
	}
	return os.WriteFile(path, []byte(newVal), info.Mode())
}

// estimateGrokSession simulates the rewrite of every bulky file and returns
// the directory's projected post-compaction total size. It writes nothing.
func estimateGrokSession(dir string, threshold int64) (int64, error) {
	total := grok.SessionDiskBytes(dir)
	for _, name := range grokBulkJSONL {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		after, err := estimateJSONL(path, grokFileMutator(name), threshold)
		if err != nil {
			continue
		}
		total -= info.Size() - after
	}
	for _, log := range grokTerminalLogs(dir) {
		info, err := os.Stat(log)
		if err != nil || info.Size() <= threshold {
			continue
		}
		data, err := os.ReadFile(log)
		if err != nil {
			continue
		}
		if newVal, delta := truncateString(string(data), threshold); delta > 0 {
			total -= info.Size() - int64(len(newVal))
		}
	}
	return total, nil
}
```

- [ ] **Step 6: Register Grok in `compact.go` and branch on the directory model**

In `internal/compact/compact.go`, add the agent to `supportedAgents` (lines 24-28). The colour `#89B4FA` (blue) is distinct from claude `#E7A15E`, pi `#F38BA8`, and codex `#A6E3A1`:

```go
var supportedAgents = []chatops.Agent{
	{Key: "claude", Label: "Claude Code", Color: "#E7A15E"},
	{Key: "pi", Label: "pi coding agent", Color: "#F38BA8"},
	{Key: "codex", Label: "Codex CLI", Color: "#A6E3A1"},
	{Key: "grok", Label: "Grok", Color: "#89B4FA"},
}
```

In `collectCandidates` (lines 264-271), replace the single-file stat with `sessionSizeBytes` so a Grok session directory is measured as a whole:

```go
			size := sessionSizeBytes(s)
			if size < opts.minSize {
				continue
			}
			all = append(all, Candidate{Session: s, SizeBefore: size})
```

(Delete the old `info, err := os.Stat(s.JSONLPath)` / `if err != nil { continue }` / `if info.Size() < opts.minSize` lines it replaces. The `os` import stays — it is still used elsewhere in the file.)

- [ ] **Step 7: Branch `estimateSizes` and `executeCompact` on Grok**

In `internal/compact/rewrite.go`, replace `estimateSizes` (lines 188-200):

```go
// estimateSizes fills SizeAfter for every candidate based on a dry-run
// simulation. Best-effort: on error the after-size equals the before-size.
func estimateSizes(candidates []Candidate, threshold int64) {
	for i := range candidates {
		c := &candidates[i]
		var after int64
		var err error
		if c.Session.Agent == "grok" {
			after, err = estimateGrokSession(c.Session.JSONLPath, threshold)
		} else {
			after, err = estimateRewrite(c.Session.JSONLPath, c.Session.Agent, threshold)
		}
		if err != nil {
			c.SizeAfter = c.SizeBefore
			continue
		}
		c.SizeAfter = after
	}
}
```

Replace the loop body of `executeCompact` (lines 251-265) so Grok uses the directory compactor:

```go
	for i := range candidates {
		c := &candidates[i]
		var newSize int64
		var err error
		if c.Session.Agent == "grok" {
			newSize, err = compactGrokSession(c.Session.JSONLPath, opts.threshold, opts.backup)
		} else {
			newSize, err = rewriteFile(c.Session.JSONLPath, mutatorFor(c.Session.Agent), opts.threshold, opts.backup)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed: %s — %v\n", filepath.Base(c.Session.JSONLPath), err)
			failed++
			continue
		}
		c.SizeAfter = newSize
		if newSize < c.SizeBefore {
			saved += c.SizeBefore - newSize
		}
		processed++
	}
```

- [ ] **Step 8: Run the test + full build + full suite**

Run: `go test ./internal/compact/... && go build ./... && go test ./...`
Expected: PASS — `TestCompactGrokSession` and `TestEstimateGrokSession` green, whole suite green.

- [ ] **Step 9: Commit**

```bash
git add internal/compact/grok.go internal/compact/compact.go internal/compact/rewrite.go internal/compact/grok_test.go
git commit -m "feat(grok): add directory-aware Grok compaction"
```

---

## Phase 5 — Documentation

Outcome: docs describe Grok everywhere the supported-agent set is listed.

### Task 9: Documentation updates

**Files:**
- Modify: `docs/concepts/supported-agents.md`
- Modify: `docs/maintenance/prune.md`, `docs/maintenance/compact.md`, `docs/maintenance/search.md`
- Modify: `docs/usage/cli.md`

- [ ] **Step 1: Update `docs/concepts/supported-agents.md`**

Change the opening line "lazyagent supports seven agents" to "lazyagent supports eight agents".

Add a table row after the `pi` / `OpenCode` rows (keep the existing column layout):

```
| [Grok CLI](https://github.com/xai-org/grok-cli) | `~/.grok/sessions/<encoded-cwd>/<uuid>/` | Directory per session (JSONL + JSON) | `G` |
```

Add a per-agent note after the "OpenCode" subsection, before "What's not supported (yet)":

```markdown
### Grok CLI

Grok writes one *directory* per session, two levels deep under
`~/.grok/sessions/<url-encoded-cwd>/<session-uuid>/`. Each session directory
holds a `summary.json` (metadata), a `chat_history.jsonl` (transcript), an
`updates.jsonl` stream, and several smaller files. lazyagent reads
`summary.json` plus `chat_history.jsonl` and decodes the cwd from the standard
URL percent-encoding of the parent directory name.

**No per-session cost.** Grok's on-disk data does not expose an
input/output/cache token split, so Grok sessions show no per-session token or
cost figures in any interface — those fields are honestly left at zero. (The
separate `lazyagent limits` command still reports Grok's monthly billing
window; that uses Grok's billing API, not on-disk session data.)

**Subagent sessions** (`session_kind: "subagent"` in `summary.json`) are
treated as sidechains and hidden from the default list, the same as Claude's
sub-agent sessions.
```

- [ ] **Step 2: Update `docs/concepts/supported-agents.md` selecting-a-subset block**

Add a line to the `--agent` code block (after `lazyagent --agent opencode`):

```
lazyagent --agent grok
```

- [ ] **Step 3: Update the maintenance docs**

In `docs/maintenance/prune.md`, find where the supported agents are listed (`claude, pi, codex`) and add `grok`. Add a sentence: "Grok sessions are directories, so pruning a Grok session deletes the whole session directory recursively."

In `docs/maintenance/compact.md`, change the `--agent LIST` table row (line 28) subset hint from `claude,pi,codex` to `claude,pi,codex,grok`, and the line 78 example `--agent claude,codex,pi` to include `grok`. Add a sentence to the field-paths section (around line 82): "For Grok, compact rewrites the bulky files inside the session directory — `updates.jsonl`, oversized `tool_result` payloads in `chat_history.jsonl`, `rewind_points.jsonl`, and `terminal/*.log`. Truncating `rewind_points.jsonl` disables Grok's rewind feature for that session."

In `docs/maintenance/search.md`: line 10, add **Grok** to the list of plain-text-transcript agents; line 26, change the `--agent LIST` subset to `claude,codex,pi,amp,grok`; lines 104-107, add a bullet `- **grok** — Grok CLI (~/.grok/sessions/)`. The resume-command table (lines 73-76) gets no Grok row — `search` has no Grok resume command (out of scope); opening a Grok result prints a graceful "no resume command" message.

- [ ] **Step 4: Update `docs/usage/cli.md`**

Add a row to the `--agent` value table (after the `amp` row, lines 96-99):

```
| `grok` | Grok CLI |
```

Update the `search` paragraph (line 156): change "(Claude, Codex, pi, Amp)" to "(Claude, Codex, pi, Amp, Grok)".

- [ ] **Step 5: Verify the docs build / links**

Run: `grep -rn "seven agents\|claude,pi,codex\b" docs/` and confirm no stale "seven agents" or incomplete agent lists remain where Grok belongs.

- [ ] **Step 6: Commit**

```bash
git add docs/
git commit -m "docs(grok): document Grok as a supported agent"
```

---

## Final verification

- [ ] **Run the whole suite green**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all packages build, vet clean, all tests pass.

- [ ] **Smoke-test the binary** (if Grok is installed locally)

Run: `go build -o lazyagent . && ./lazyagent --agent grok` — Grok sessions list with the `G ` prefix, subagent sessions hidden. Then `./lazyagent search --agent grok "<a word from a known session>"`, `./lazyagent prune --agent grok --dry-run`, and `./lazyagent compact --agent grok --dry-run` — each lists Grok sessions without error.

---

## Self-review (completed by plan author)

**1. Spec coverage** — every spec section maps to a task:
- §4 provider package → Tasks 1-3. §4.1 discovery strategy → Task 2. §4.2 data mapping → Task 1 (`ParseGrokSession`). §4.2 token-gap → zero fields asserted in Task 1's test, documented in Task 9. §4.3 provider wiring → Task 3.
- §5 integration file list → Tasks 3 (provider, config), 4 (main, ui), 5 (search), 6 (prune), 8 (compact), 9 (docs).
- §6 subagents/`IsSidechain` → Task 1 (set flag) + Tasks 1/2 tests; downstream filtering verified in "Design decisions" #4.
- §7.1 search → Task 5. §7.2 prune (directory-aware) → Task 6. §7.3 compact Phase 0 → Task 7; Rewriter → Task 8.
- §8 phasing → Phases 1-5 mirror the spec. §9 testing → tests in every code task. §10 error handling → missing-dir/malformed-summary/format-version/missing-signals all covered by Task 1-2 tests. §11 risks → #1 Task 7, #2 Task 6, #3 Design decision #4, #4 Task 1+9, #5 `supportedChatFormatVersion`, #6 bounded `updates.jsonl` tail (Design decision #3), #7 Task 6 note. §13 → Phase 4 starts with the Phase 0 task before any Rewriter code.

**2. Placeholder scan** — no `TBD`/`TODO`/"add error handling"/"similar to Task N". Task 7 is intentionally a verification procedure (no production code); Task 8 is fully concrete and carries explicit Phase 0 gate instructions for narrowing scope — this is a documented gate, not a placeholder.

**3. Type consistency** — `ParseGrokSession`, `discoverSessionsFromDir`, `walkSessionDirs`, `SessionDirs`, `SessionDiskBytes`, `GrokSessionsDir` used consistently across grok/search/prune/compact. `grokFileMutator` returns the existing `lineMutator` type. `estimateJSONL` is introduced in Task 8 step 3 and consumed in step 5. `sessionSizeBytes`/`grokBulkJSONL`/`grokTerminalLogs` defined in `grok.go`, used in `compact.go`/`rewrite.go`. `removeEmptyDirs` signature change (added `grokRoot`) is applied at its one call site in the same task.
