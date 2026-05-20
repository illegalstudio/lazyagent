package grok

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
{"type":"user","content":[{"type":"text","text":"<user_info>\nOS Version: macos\n</user_info>"}]}
{"type":"user","content":[{"type":"text","text":"\n\n<system-reminder>\nThe following skills are available\n</system-reminder>"}]}
{"type":"user","content":[{"type":"text","text":"<user_query>\nHello Grok\n</user_query>"}]}
{"type":"assistant","content":"","reasoning":{"text":"Let me look into it","encrypted":"abc","id":"r1"},"tool_calls":[{"id":"call-1","name":"bash","arguments":"{}"}],"model_id":"grok-build"}
{"type":"tool_result","content":"command output","tool_call_id":"call-1"}
{"type":"assistant","content":"Here is the result","reasoning":{"text":"done","encrypted":"def","id":"r2"},"model_id":"grok-build"}
`

const primarySignals = `{"userMessageCount":1,"assistantMessageCount":2,"contextTokensUsed":1234}`

func TestParseGrokSession_Fields(t *testing.T) {
	root := t.TempDir()
	dir := writeSession(t, root, "%2FUsers%2Falice%2Fproject", "019e0000-0000-7000-8000-000000000001", map[string]string{
		"summary.json":       primarySummary,
		"chat_history.jsonl": primaryChat,
		"signals.json":       primarySignals,
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
	if s.UserMessages != 1 || s.AssistantMessages != 2 {
		t.Errorf("counts = (%d,%d), want (1,2)", s.UserMessages, s.AssistantMessages)
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
	if s.Status != model.StatusWaitingForUser {
		t.Errorf("Status = %v, want WaitingForUser (last entry is assistant with no tool_calls)", s.Status)
	}
}

func TestParseGrokSession_TitleFallback(t *testing.T) {
	root := t.TempDir()
	// No generated_title, no session_summary → fall back to first user message.
	summary := `{"info":{"id":"x","cwd":"/tmp/p"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z"}`
	chat := `{"type":"user","content":[{"type":"text","text":"<user_query>Please refactor auth</user_query>"}]}` + "\n"
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
		"summary.json":       primarySummary,
		"chat_history.jsonl": primaryChat,
		// signals.json deliberately absent
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.UserMessages != 1 || s.AssistantMessages != 2 {
		t.Errorf("counts from transcript = (%d,%d), want (1,2)", s.UserMessages, s.AssistantMessages)
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

func TestParseGrokSession_StatusExecutingTool(t *testing.T) {
	root := t.TempDir()
	summary := `{"info":{"id":"t","cwd":"/tmp/p"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z"}`
	// Chat ends on an assistant entry with a pending tool call.
	chat := `{"type":"user","content":[{"type":"text","text":"run the tests"}]}
{"type":"assistant","content":"running","reasoning":{"text":"running tests","encrypted":"x","id":"r1"},"tool_calls":[{"id":"call-9","name":"bash","arguments":"{}"}]}
`
	dir := writeSession(t, root, "%2Ftmp%2Fp", "t", map[string]string{
		"summary.json": summary, "chat_history.jsonl": chat,
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != model.StatusExecutingTool {
		t.Errorf("Status = %v, want ExecutingTool", s.Status)
	}
	if s.CurrentTool != "Bash" {
		t.Errorf("CurrentTool = %q, want Bash", s.CurrentTool)
	}
}

func TestParseGrokSession_StatusThinking(t *testing.T) {
	root := t.TempDir()
	summary := `{"info":{"id":"u","cwd":"/tmp/p"},"chat_format_version":1,
		"updated_at":"2026-05-17T11:00:00Z"}`
	chat := `{"type":"user","content":[{"type":"text","text":"hello"}]}` + "\n"
	dir := writeSession(t, root, "%2Ftmp%2Fp", "u", map[string]string{
		"summary.json": summary, "chat_history.jsonl": chat,
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Status != model.StatusThinking {
		t.Errorf("Status = %v, want Thinking", s.Status)
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

func TestParseGrokSession_FiltersSyntheticUserEntries(t *testing.T) {
	// primaryChat has 3 user entries: <user_info>, <system-reminder>, and the
	// real <user_query>. Only the last is a message.
	root := t.TempDir()
	dir := writeSession(t, root, "%2Ftmp%2Fp", "f", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	userMsgs := 0
	for _, m := range s.RecentMessages {
		if strings.Contains(m.Text, "<system-reminder>") || strings.Contains(m.Text, "<user_info>") {
			t.Errorf("synthetic entry leaked into RecentMessages: %q", m.Text)
		}
		if m.Role == "user" {
			userMsgs++
			if strings.Contains(m.Text, "<user_query>") {
				t.Errorf("user message not unwrapped: %q", m.Text)
			}
			if m.Text != "Hello Grok" {
				t.Errorf("user message = %q, want %q", m.Text, "Hello Grok")
			}
		}
	}
	if userMsgs != 1 {
		t.Errorf("got %d user messages in RecentMessages, want 1", userMsgs)
	}
}

func TestParseGrokSession_AssistantEntriesParsed(t *testing.T) {
	// Regression: Grok assistant entries carry `reasoning` as an OBJECT. A
	// mistyped struct field made json.Unmarshal fail and silently drop every
	// assistant entry.
	root := t.TempDir()
	dir := writeSession(t, root, "%2Ftmp%2Fp", "a", map[string]string{
		"summary.json": primarySummary, "chat_history.jsonl": primaryChat,
	})
	s, err := ParseGrokSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	asstMsgs := 0
	for _, m := range s.RecentMessages {
		if m.Role == "assistant" {
			asstMsgs++
		}
	}
	// primaryChat has 2 assistant entries: a tool-call turn (empty content,
	// no message row) and a final answer (content → one message row).
	if asstMsgs != 1 {
		t.Errorf("assistant messages in RecentMessages = %d, want 1", asstMsgs)
	}
	if s.AssistantMessages != 2 {
		t.Errorf("AssistantMessages = %d, want 2 (both entries counted)", s.AssistantMessages)
	}
	if len(s.RecentTools) == 0 {
		t.Error("no tools captured — assistant tool_calls were dropped")
	}
}

func TestUserMessageText(t *testing.T) {
	tests := []struct {
		name    string
		content string // raw JSON for the entry's content field
		want    string
		wantOK  bool
	}{
		{"user_query at start", `[{"type":"text","text":"<user_query>hi there</user_query>"}]`, "hi there", true},
		{"user_query mid-string", `[{"type":"text","text":"prefix <user_query>real</user_query>"}]`, "real", true},
		{"user_query unterminated", `[{"type":"text","text":"<user_query>tail only"}]`, "tail only", true},
		{"system-reminder", `[{"type":"text","text":"<system-reminder>ctx</system-reminder>"}]`, "", false},
		{"system-reminder leading whitespace", `[{"type":"text","text":"\n\n<system-reminder>ctx</system-reminder>"}]`, "", false},
		{"user_info", `[{"type":"text","text":"<user_info>os</user_info>"}]`, "", false},
		{"plain text", `[{"type":"text","text":"just a message"}]`, "just a message", true},
		{"empty", `[{"type":"text","text":""}]`, "", false},
		{"plain string content", `"plain string"`, "plain string", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := UserMessageText(json.RawMessage(tt.content))
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("UserMessageText(%s) = (%q,%v), want (%q,%v)", tt.content, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
