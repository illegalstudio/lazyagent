package claude

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

// --- helpers ---

// mkJSONL joins entries with newlines to form a JSONL payload.
func mkJSONL(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// writeTempJSONL writes content to a temp .jsonl file and returns its path.
func writeTempJSONL(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test-session.jsonl")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func ts(s string) string { return s } // readability alias for timestamps

// --- extractFilePathFromRaw ---

func TestExtractFilePathFromRaw_WriteTool(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"text","text":"Let me write that file."},
		{"type":"tool_use","name":"Write","input":{"file_path":"/tmp/hello.go","content":"package main"}}
	]`)
	got := extractFilePathFromRaw(raw)
	if got != "/tmp/hello.go" {
		t.Errorf("got %q, want /tmp/hello.go", got)
	}
}

func TestExtractFilePathFromRaw_EditTool(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"tool_use","name":"Edit","input":{"file_path":"/src/main.go","old_string":"a","new_string":"b"}}
	]`)
	got := extractFilePathFromRaw(raw)
	if got != "/src/main.go" {
		t.Errorf("got %q, want /src/main.go", got)
	}
}

func TestExtractFilePathFromRaw_NotebookEditTool(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"tool_use","name":"NotebookEdit","input":{"file_path":"/nb/test.ipynb","cell":"1"}}
	]`)
	got := extractFilePathFromRaw(raw)
	if got != "/nb/test.ipynb" {
		t.Errorf("got %q, want /nb/test.ipynb", got)
	}
}

func TestExtractFilePathFromRaw_LastWriteWins(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"tool_use","name":"Write","input":{"file_path":"/first.go","content":"a"}},
		{"type":"tool_use","name":"Edit","input":{"file_path":"/second.go","old_string":"x","new_string":"y"}}
	]`)
	got := extractFilePathFromRaw(raw)
	if got != "/second.go" {
		t.Errorf("got %q, want /second.go (last write tool)", got)
	}
}

func TestExtractFilePathFromRaw_NoWriteTools(t *testing.T) {
	raw := json.RawMessage(`[
		{"type":"tool_use","name":"Read","input":{"file_path":"/readme.md"}},
		{"type":"text","text":"Done reading."}
	]`)
	got := extractFilePathFromRaw(raw)
	if got != "" {
		t.Errorf("got %q, want empty string (no write tools)", got)
	}
}

func TestExtractFilePathFromRaw_EmptyContent(t *testing.T) {
	if got := extractFilePathFromRaw(nil); got != "" {
		t.Errorf("nil: got %q, want empty", got)
	}
	if got := extractFilePathFromRaw(json.RawMessage(`[]`)); got != "" {
		t.Errorf("empty array: got %q, want empty", got)
	}
}

func TestExtractFilePathFromRaw_NoFilePathKey(t *testing.T) {
	// Pre-screening should skip this because "file_path" doesn't appear.
	raw := json.RawMessage(`[{"type":"tool_use","name":"Bash","input":{"command":"ls"}}]`)
	got := extractFilePathFromRaw(raw)
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractFilePathFromRaw_MalformedJSON(t *testing.T) {
	raw := json.RawMessage(`not valid json with "file_path" in it`)
	got := extractFilePathFromRaw(raw)
	if got != "" {
		t.Errorf("got %q, want empty for malformed JSON", got)
	}
}

// --- parseMessage / jsonlMessage ---

func TestParseMessage_UserTextString(t *testing.T) {
	e := jsonlEntry{
		RawMessage: json.RawMessage(`{"role":"user","content":"hello world"}`),
	}
	msg := e.parseMessage()
	if msg == nil {
		t.Fatal("parseMessage returned nil")
	}
	if msg.Role != "user" {
		t.Errorf("role = %q, want user", msg.Role)
	}
	if msg.TextContent != "hello world" {
		t.Errorf("TextContent = %q, want 'hello world'", msg.TextContent)
	}
}

func TestParseMessage_AssistantWithToolUse(t *testing.T) {
	e := jsonlEntry{
		RawMessage: json.RawMessage(`{
			"role":"assistant",
			"model":"claude-sonnet-4-20250514",
			"content":[
				{"type":"text","text":"Let me check."},
				{"type":"tool_use","name":"Read","tool_use_id":"tu1"}
			]
		}`),
	}
	msg := e.parseMessage()
	if msg == nil {
		t.Fatal("parseMessage returned nil")
	}
	if msg.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", msg.Model)
	}
	if len(msg.Content) != 2 {
		t.Fatalf("Content length = %d, want 2", len(msg.Content))
	}
	if msg.Content[1].Name != "Read" {
		t.Errorf("tool name = %q, want Read", msg.Content[1].Name)
	}
}

func TestParseMessage_NullMessage(t *testing.T) {
	e := jsonlEntry{RawMessage: json.RawMessage(`null`)}
	if msg := e.parseMessage(); msg != nil {
		t.Errorf("expected nil for null message, got %+v", msg)
	}
}

func TestParseMessage_EmptyMessage(t *testing.T) {
	e := jsonlEntry{RawMessage: nil}
	if msg := e.parseMessage(); msg != nil {
		t.Errorf("expected nil for empty message, got %+v", msg)
	}
}

func TestParseMessage_Cached(t *testing.T) {
	e := jsonlEntry{
		RawMessage: json.RawMessage(`{"role":"user","content":"test"}`),
	}
	msg1 := e.parseMessage()
	msg2 := e.parseMessage()
	if msg1 != msg2 {
		t.Error("expected same pointer on second call (cached)")
	}
}

// --- determineStatus ---

func TestDetermineStatus_Nil(t *testing.T) {
	if s := determineStatus(nil); s != model.StatusUnknown {
		t.Errorf("got %v, want StatusUnknown", s)
	}
}

func TestDetermineStatus_AssistantText(t *testing.T) {
	e := &jsonlEntry{
		Type:       "assistant",
		RawMessage: json.RawMessage(`{"role":"assistant","content":[{"type":"text","text":"Done."}]}`),
	}
	if s := determineStatus(e); s != model.StatusWaitingForUser {
		t.Errorf("got %v, want StatusWaitingForUser", s)
	}
}

func TestDetermineStatus_AssistantToolUse(t *testing.T) {
	e := &jsonlEntry{
		Type:       "assistant",
		RawMessage: json.RawMessage(`{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}`),
	}
	if s := determineStatus(e); s != model.StatusExecutingTool {
		t.Errorf("got %v, want StatusExecutingTool", s)
	}
}

func TestDetermineStatus_UserMessage(t *testing.T) {
	e := &jsonlEntry{
		Type:       "user",
		RawMessage: json.RawMessage(`{"role":"user","content":"do something"}`),
	}
	if s := determineStatus(e); s != model.StatusThinking {
		t.Errorf("got %v, want StatusThinking", s)
	}
}

func TestDetermineStatus_UserToolResult(t *testing.T) {
	e := &jsonlEntry{
		Type:       "user",
		RawMessage: json.RawMessage(`{"role":"user","content":[{"type":"tool_result","tool_use_id":"tu1","content":"ok"}]}`),
	}
	if s := determineStatus(e); s != model.StatusProcessingResult {
		t.Errorf("got %v, want StatusProcessingResult", s)
	}
}

// --- scanEntries ---

func TestScanEntries_BasicSession(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"hello"},"cwd":"/proj","version":"1.0","gitBranch":"main"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hi there!"}],"usage":{"input_tokens":100,"output_tokens":50}}}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	_, parsed, last := scanEntries(scanner, session, 0, nil, nil, nil)

	if !parsed {
		t.Fatal("expected parsed=true")
	}
	if session.CWD != "/proj" {
		t.Errorf("CWD = %q, want /proj", session.CWD)
	}
	if session.Version != "1.0" {
		t.Errorf("Version = %q, want 1.0", session.Version)
	}
	if session.GitBranch != "main" {
		t.Errorf("GitBranch = %q, want main", session.GitBranch)
	}
	if session.UserMessages != 1 {
		t.Errorf("UserMessages = %d, want 1", session.UserMessages)
	}
	if session.AssistantMessages != 1 {
		t.Errorf("AssistantMessages = %d, want 1", session.AssistantMessages)
	}
	if session.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2", session.TotalMessages)
	}
	if session.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", session.InputTokens)
	}
	if session.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", session.OutputTokens)
	}
	if session.Model != "claude-sonnet-4-20250514" {
		t.Errorf("Model = %q", session.Model)
	}
	if last == nil {
		t.Fatal("expected lastMeaningful != nil")
	}
	if last.Type != "assistant" {
		t.Errorf("lastMeaningful.Type = %q, want assistant", last.Type)
	}
	if len(session.RecentMessages) != 2 {
		t.Errorf("RecentMessages length = %d, want 2", len(session.RecentMessages))
	}
}

func TestScanEntries_SkipsNonUserAssistant(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"system","timestamp":"2025-01-15T10:00:00Z","message":{"role":"system","content":"init"},"cwd":"/proj"}`,
		`{"type":"summary","timestamp":"2025-01-15T10:00:01Z","message":null}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	_, parsed, last := scanEntries(scanner, session, 0, nil, nil, nil)

	if !parsed {
		t.Fatal("expected parsed=true (system entries are still parsed for metadata)")
	}
	if session.UserMessages != 0 || session.AssistantMessages != 0 {
		t.Errorf("expected 0 user/assistant messages, got %d/%d", session.UserMessages, session.AssistantMessages)
	}
	if last != nil {
		t.Error("expected lastMeaningful=nil (no user/assistant entries)")
	}
	if session.CWD != "/proj" {
		t.Errorf("CWD = %q, want /proj (metadata extracted from non-user entry)", session.CWD)
	}
}

func TestScanEntries_CostAccumulation(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","costUSD":0.01,"message":{"role":"user","content":"a"}}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:00:01Z","costUSD":0.05,"message":{"role":"assistant","content":[{"type":"text","text":"b"}]}}`,
		`{"type":"result","timestamp":"2025-01-15T10:00:02Z","costUSD":0.02,"message":null}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	// Cost should accumulate from all entry types, not just user/assistant.
	wantCost := 0.08
	if diff := session.CostUSD - wantCost; diff > 0.001 || diff < -0.001 {
		t.Errorf("CostUSD = %f, want ~%f", session.CostUSD, wantCost)
	}
}

func TestScanEntries_ToolCallsTracked(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"assistant","timestamp":"2025-01-15T10:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"},{"type":"tool_use","name":"Read"}]}}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	if len(session.RecentTools) != 2 {
		t.Fatalf("RecentTools length = %d, want 2", len(session.RecentTools))
	}
	if session.RecentTools[0].Name != "Bash" {
		t.Errorf("RecentTools[0] = %q, want Bash", session.RecentTools[0].Name)
	}
	if session.RecentTools[1].Name != "Read" {
		t.Errorf("RecentTools[1] = %q, want Read", session.RecentTools[1].Name)
	}
}

func TestScanEntries_FileWriteTracked(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"assistant","timestamp":"2025-01-15T10:00:00Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Write","input":{"file_path":"/tmp/out.go","content":"pkg"}}]}}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	if session.LastFileWrite != "/tmp/out.go" {
		t.Errorf("LastFileWrite = %q, want /tmp/out.go", session.LastFileWrite)
	}
	if session.LastFileWriteAt.IsZero() {
		t.Error("LastFileWriteAt should not be zero")
	}
}

func TestScanEntries_SidechainDetected(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","isSidechain":true,"message":{"role":"user","content":"sub-task"}}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	if !session.IsSidechain {
		t.Error("expected IsSidechain=true")
	}
}

func TestScanEntries_SummaryTimestamp(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"x"}}`,
		`{"type":"summary","timestamp":"2025-01-15T10:00:05Z","message":null}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	want, _ := time.Parse(time.RFC3339, "2025-01-15T10:00:00Z")
	if !session.LastSummaryAt.Equal(want) {
		t.Errorf("LastSummaryAt = %v, want %v (timestamp of previous entry)", session.LastSummaryAt, want)
	}
}

func TestScanEntries_EntryTimestampsTrimmed(t *testing.T) {
	// Generate >500 entries to verify trimming.
	var lines []string
	for i := 0; i < 510; i++ {
		ts := fmt.Sprintf("2025-01-15T10:%02d:%02dZ", i/60, i%60)
		lines = append(lines, fmt.Sprintf(`{"type":"system","timestamp":"%s","message":null}`, ts))
	}
	jsonl := mkJSONL(lines...)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	if len(session.EntryTimestamps) != 500 {
		t.Errorf("EntryTimestamps length = %d, want 500 (trimmed)", len(session.EntryTimestamps))
	}
}

func TestScanEntries_RecentMessagesTrimmed(t *testing.T) {
	var lines []string
	for i := 0; i < 25; i++ {
		ts := fmt.Sprintf("2025-01-15T10:00:%02dZ", i)
		role := "user"
		msgType := "user"
		if i%2 == 1 {
			role = "assistant"
			msgType = "assistant"
		}
		content := fmt.Sprintf("message %d", i)
		if role == "user" {
			lines = append(lines, fmt.Sprintf(`{"type":"%s","timestamp":"%s","message":{"role":"%s","content":"%s"}}`, msgType, ts, role, content))
		} else {
			lines = append(lines, fmt.Sprintf(`{"type":"%s","timestamp":"%s","message":{"role":"%s","content":[{"type":"text","text":"%s"}]}}`, msgType, ts, role, content))
		}
	}
	jsonl := mkJSONL(lines...)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	if len(session.RecentMessages) > 10 {
		t.Errorf("RecentMessages length = %d, want <= 10", len(session.RecentMessages))
	}
}

func TestScanEntries_MalformedLinesSkipped(t *testing.T) {
	jsonl := mkJSONL(
		`not json at all`,
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"valid"}}`,
		`{broken json`,
		`{"type":"assistant","timestamp":"2025-01-15T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"}]}}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	_, parsed, _ := scanEntries(scanner, session, 0, nil, nil, nil)

	if !parsed {
		t.Fatal("expected parsed=true")
	}
	if session.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2 (malformed lines skipped)", session.TotalMessages)
	}
}

func TestScanEntries_TokenAccumulation(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"assistant","timestamp":"2025-01-15T10:00:00Z","message":{"role":"assistant","content":[{"type":"text","text":"a"}],"usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":2,"cache_read_input_tokens":3}}}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:00:01Z","message":{"role":"assistant","content":[{"type":"text","text":"b"}],"usage":{"input_tokens":20,"output_tokens":15,"cache_creation_input_tokens":4,"cache_read_input_tokens":1}}}`,
	)
	session := &model.Session{}
	scanner := bufio.NewScanner(strings.NewReader(jsonl))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	scanEntries(scanner, session, 0, nil, nil, nil)

	if session.InputTokens != 30 {
		t.Errorf("InputTokens = %d, want 30", session.InputTokens)
	}
	if session.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", session.OutputTokens)
	}
	if session.CacheCreationTokens != 6 {
		t.Errorf("CacheCreationTokens = %d, want 6", session.CacheCreationTokens)
	}
	if session.CacheReadTokens != 4 {
		t.Errorf("CacheReadTokens = %d, want 4", session.CacheReadTokens)
	}
}

// --- ParseJSONL (full file) ---

func TestParseJSONL_FullParse(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"build this"},"cwd":"/app","version":"2.0","gitBranch":"dev"}`,
		`{"type":"assistant","timestamp":"2025-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-opus-4-20250514","content":[{"type":"text","text":"Sure!"}],"usage":{"input_tokens":200,"output_tokens":100}}}`,
	)
	path := writeTempJSONL(t, jsonl)

	session, offset, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}

	if offset <= 0 {
		t.Errorf("offset = %d, want > 0", offset)
	}
	if session.Agent != "claude" {
		t.Errorf("Agent = %q, want claude", session.Agent)
	}
	// SessionID from filename
	if session.SessionID != "test-session" {
		t.Errorf("SessionID = %q, want test-session", session.SessionID)
	}
	if session.CWD != "/app" {
		t.Errorf("CWD = %q, want /app", session.CWD)
	}
	if session.Status != model.StatusWaitingForUser {
		t.Errorf("Status = %v, want StatusWaitingForUser", session.Status)
	}
	if session.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2", session.TotalMessages)
	}
}

func TestParseJSONL_EmptyFile(t *testing.T) {
	path := writeTempJSONL(t, "")

	session, offset, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if offset != 0 {
		t.Errorf("offset = %d, want 0 for empty file", offset)
	}
	if session.TotalMessages != 0 {
		t.Errorf("TotalMessages = %d, want 0", session.TotalMessages)
	}
}

func TestParseJSONL_NonExistentFile(t *testing.T) {
	_, _, err := ParseJSONL("/nonexistent/path/session.jsonl")
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

// --- ParseJSONLIncremental ---

func TestParseJSONLIncremental_AppendsNewEntries(t *testing.T) {
	initial := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"first"},"cwd":"/proj"}`,
	)
	path := writeTempJSONL(t, initial)

	// Full parse first.
	session, offset, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if session.UserMessages != 1 {
		t.Fatalf("initial UserMessages = %d, want 1", session.UserMessages)
	}

	// Append new content.
	appended := `{"type":"assistant","timestamp":"2025-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"response"}],"usage":{"input_tokens":50,"output_tokens":25}}}` + "\n"
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(appended)
	f.Close()

	// Incremental parse from offset.
	cloned := session.Clone()
	updated, newOffset, err := ParseJSONLIncremental(path, offset, cloned)
	if err != nil {
		t.Fatal(err)
	}
	if newOffset <= offset {
		t.Errorf("newOffset = %d, should be > %d", newOffset, offset)
	}
	if updated.AssistantMessages != 1 {
		t.Errorf("AssistantMessages = %d, want 1", updated.AssistantMessages)
	}
	if updated.TotalMessages != 2 {
		t.Errorf("TotalMessages = %d, want 2", updated.TotalMessages)
	}
	if updated.InputTokens != 50 {
		t.Errorf("InputTokens = %d, want 50", updated.InputTokens)
	}
	if updated.Status != model.StatusWaitingForUser {
		t.Errorf("Status = %v, want StatusWaitingForUser", updated.Status)
	}
}

func TestParseJSONLIncremental_NoNewContent(t *testing.T) {
	jsonl := mkJSONL(
		`{"type":"user","timestamp":"2025-01-15T10:00:00Z","message":{"role":"user","content":"hi"}}`,
	)
	path := writeTempJSONL(t, jsonl)

	session, offset, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}

	// Incremental with nothing new — should return same state.
	cloned := session.Clone()
	updated, newOffset, err := ParseJSONLIncremental(path, offset, cloned)
	if err != nil {
		t.Fatal(err)
	}
	if newOffset != offset {
		t.Errorf("newOffset = %d, want %d (no new content)", newOffset, offset)
	}
	if updated.TotalMessages != 1 {
		t.Errorf("TotalMessages = %d, want 1 (unchanged)", updated.TotalMessages)
	}
}

// --- applyLastMeaningful ---

func TestApplyLastMeaningful_SetsStatusAndActivity(t *testing.T) {
	session := &model.Session{}
	e := &jsonlEntry{
		Type:       "assistant",
		Timestamp:  "2025-01-15T10:00:05Z",
		RawMessage: json.RawMessage(`{"role":"assistant","content":[{"type":"tool_use","name":"Write"}]}`),
	}

	applyLastMeaningful(session, e)

	if session.Status != model.StatusExecutingTool {
		t.Errorf("Status = %v, want StatusExecutingTool", session.Status)
	}
	if session.CurrentTool != "Write" {
		t.Errorf("CurrentTool = %q, want Write", session.CurrentTool)
	}
	wantTime, _ := time.Parse(time.RFC3339, "2025-01-15T10:00:05Z")
	if !session.LastActivity.Equal(wantTime) {
		t.Errorf("LastActivity = %v, want %v", session.LastActivity, wantTime)
	}
}

func TestApplyLastMeaningful_NilEntry(t *testing.T) {
	session := &model.Session{}
	applyLastMeaningful(session, nil)
	if session.Status != model.StatusUnknown {
		t.Errorf("Status = %v, want StatusUnknown for nil entry", session.Status)
	}
}

// --- firstText / isToolResult ---

func TestFirstText_PlainString(t *testing.T) {
	m := &jsonlMessage{TextContent: "hello"}
	if got := firstText(m); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestFirstText_ContentArray(t *testing.T) {
	m := &jsonlMessage{
		Content: []jsonlContent{
			{Type: "tool_use", Name: "Bash"},
			{Type: "text", Text: "the answer"},
		},
	}
	if got := firstText(m); got != "the answer" {
		t.Errorf("got %q, want 'the answer'", got)
	}
}

func TestFirstText_NoText(t *testing.T) {
	m := &jsonlMessage{
		Content: []jsonlContent{{Type: "tool_use", Name: "Read"}},
	}
	if got := firstText(m); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestIsToolResult_True(t *testing.T) {
	m := &jsonlMessage{
		Content: []jsonlContent{{Type: "tool_result", ToolUseID: "tu1"}},
	}
	if !isToolResult(m) {
		t.Error("expected true")
	}
}

func TestIsToolResult_False(t *testing.T) {
	m := &jsonlMessage{
		Content: []jsonlContent{{Type: "text", Text: "hello"}},
	}
	if isToolResult(m) {
		t.Error("expected false")
	}
}

// --- copyEntry ---

func TestCopyEntry(t *testing.T) {
	orig := &jsonlEntry{Type: "user", Timestamp: "2025-01-15T10:00:00Z"}
	cp := copyEntry(orig)
	if cp == orig {
		t.Error("expected different pointer")
	}
	cp.Type = "assistant"
	if orig.Type != "user" {
		t.Error("modifying copy should not affect original")
	}
}

func TestParseJSONL_BridgeStatus(t *testing.T) {
	content := mkJSONL(
		`{"type":"system","subtype":"bridge_status","url":"https://claude.ai/code/session_ABC123","sessionId":"abc","version":"2.1.86","timestamp":"2026-01-15T10:00:00Z"}`,
		`{"type":"user","timestamp":"2026-01-15T10:00:01Z","message":{"role":"user","content":"hello"},"cwd":"/proj","version":"1.0","gitBranch":"main"}`,
		`{"type":"assistant","timestamp":"2026-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hi!"}],"usage":{"input_tokens":10,"output_tokens":5}}}`,
	)
	path := writeTempJSONL(t, content)

	sess, _, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if sess.RemoteURL != "https://claude.ai/code/session_ABC123" {
		t.Errorf("RemoteURL = %q, want %q", sess.RemoteURL, "https://claude.ai/code/session_ABC123")
	}
}

func TestParseJSONL_NoBridgeStatus(t *testing.T) {
	content := mkJSONL(
		`{"type":"user","timestamp":"2026-01-15T10:00:01Z","message":{"role":"user","content":"hello"},"cwd":"/proj","version":"1.0","gitBranch":"main"}`,
		`{"type":"assistant","timestamp":"2026-01-15T10:00:05Z","message":{"role":"assistant","model":"claude-sonnet-4-20250514","content":[{"type":"text","text":"Hi!"}],"usage":{"input_tokens":10,"output_tokens":5}}}`,
	)
	path := writeTempJSONL(t, content)

	sess, _, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if sess.RemoteURL != "" {
		t.Errorf("RemoteURL = %q, want empty", sess.RemoteURL)
	}
}
