package kimi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestParseSession(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sess-1")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeKimiSession(t, sessionDir)

	session, _, err := ParseSession(sessionDir, "/tmp/project")
	if err != nil {
		t.Fatalf("ParseSession() error = %v", err)
	}

	if session.Agent != "kimi" {
		t.Fatalf("Agent = %q, want kimi", session.Agent)
	}
	if session.SessionID != "sess-1" {
		t.Fatalf("SessionID = %q, want sess-1", session.SessionID)
	}
	if session.CWD != "/tmp/project" {
		t.Fatalf("CWD = %q, want /tmp/project", session.CWD)
	}
	if session.Name != "Custom title" {
		t.Fatalf("Name = %q, want Custom title", session.Name)
	}
	if session.Version != "1.10" {
		t.Fatalf("Version = %q, want 1.10", session.Version)
	}
	if session.Status != model.StatusWaitingForUser {
		t.Fatalf("Status = %v, want waiting", session.Status)
	}
	if session.UserMessages != 1 || session.AssistantMessages != 1 || session.TotalMessages != 2 {
		t.Fatalf("message counts = %d/%d/%d, want 1/1/2", session.UserMessages, session.AssistantMessages, session.TotalMessages)
	}
	if len(session.RecentMessages) != 2 {
		t.Fatalf("RecentMessages len = %d, want 2", len(session.RecentMessages))
	}
	if got := session.RecentMessages[0].Text; got != "please edit" {
		t.Fatalf("first message = %q, want please edit", got)
	}
	if len(session.RecentTools) != 1 || session.RecentTools[0].Name != "WriteFile" {
		t.Fatalf("RecentTools = %+v, want WriteFile", session.RecentTools)
	}
	if session.LastFileWrite != "/tmp/project/out.txt" {
		t.Fatalf("LastFileWrite = %q, want /tmp/project/out.txt", session.LastFileWrite)
	}
	if session.InputTokens != 15 || session.CacheReadTokens != 3 || session.CacheCreationTokens != 2 || session.OutputTokens != 7 {
		t.Fatalf("tokens = input %d cacheRead %d cacheCreate %d output %d, want 15/3/2/7",
			session.InputTokens, session.CacheReadTokens, session.CacheCreationTokens, session.OutputTokens)
	}
	wantLast := time.Unix(1700000004, 0)
	if !session.LastActivity.Equal(wantLast) {
		t.Fatalf("LastActivity = %v, want %v", session.LastActivity, wantLast)
	}
}

func TestDiscoverSessionsResolvesWorkDirMetadata(t *testing.T) {
	root := t.TempDir()
	sessionsRoot := filepath.Join(root, "sessions")
	workDir := "/tmp/kimi-project"
	workHash := md5Hex(workDir)
	sessionDir := filepath.Join(sessionsRoot, workHash, "sess-1")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeKimiSession(t, sessionDir)
	metadataPath := filepath.Join(root, "kimi.json")
	if err := os.WriteFile(metadataPath, []byte(fmt.Sprintf(`{"work_dirs":[{"path":%q,"kaos":"local","last_session_id":"sess-1"}]}`, workDir)), 0o600); err != nil {
		t.Fatal(err)
	}

	sessions, err := discoverSessionsFromDir(sessionsRoot, metadataPath, model.NewSessionCache())
	if err != nil {
		t.Fatalf("discoverSessionsFromDir() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("got %d sessions, want 1", len(sessions))
	}
	if sessions[0].CWD != workDir {
		t.Fatalf("CWD = %q, want %q", sessions[0].CWD, workDir)
	}
}

func TestExtractContextChunks(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "sess-1")
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		t.Fatal(err)
	}
	writeKimiSession(t, sessionDir)

	chunks, err := ExtractContextChunks(sessionDir, "/tmp/project")
	if err != nil {
		t.Fatalf("ExtractContextChunks() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Role != "user" || chunks[0].Text != "please edit" {
		t.Fatalf("first chunk = %+v, want user please edit", chunks[0])
	}
	if chunks[1].Role != "assistant" || chunks[1].Text != "done" {
		t.Fatalf("second chunk = %+v, want assistant done", chunks[1])
	}
	if chunks[0].Name != "Custom title" {
		t.Fatalf("Name = %q, want Custom title", chunks[0].Name)
	}
}

func writeKimiSession(t *testing.T, sessionDir string) {
	t.Helper()
	wire := `{"type":"metadata","protocol_version":"1.10"}
{"timestamp":1700000000.0,"message":{"type":"TurnBegin","payload":{"user_input":[{"type":"text","text":"please edit"}]}}}
{"timestamp":1700000001.0,"message":{"type":"ToolCall","payload":{"type":"function","id":"tool-1","function":{"name":"WriteFile","arguments":"{\"path\":\"out.txt\",\"content\":\"hello\"}"}}}}
{"timestamp":1700000002.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":15,"input_cache_read":3,"input_cache_creation":2,"output":7}}}}
{"timestamp":1700000003.0,"message":{"type":"ToolResult","payload":{"tool_call_id":"tool-1","return_value":{"is_error":false}}}}
{"timestamp":1700000004.0,"message":{"type":"ContentPart","payload":{"type":"text","text":"done"}}}
{"timestamp":1700000004.0,"message":{"type":"TurnEnd","payload":{}}}
`
	if err := os.WriteFile(filepath.Join(sessionDir, "wire.jsonl"), []byte(wire), 0o600); err != nil {
		t.Fatal(err)
	}
	context := `{"role":"user","content":"please edit"}
{"role":"assistant","content":[{"type":"think","think":"hidden"},{"type":"text","text":"done"}]}
{"role":"tool","content":[{"type":"text","text":"noisy output"}]}
`
	if err := os.WriteFile(filepath.Join(sessionDir, "context.jsonl"), []byte(context), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "state.json"), []byte(`{"custom_title":"Custom title"}`), 0o600); err != nil {
		t.Fatal(err)
	}
}
