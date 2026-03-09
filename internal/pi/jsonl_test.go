package pi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nahime0/lazyagent/internal/claude"
)

func writeTempJSONL(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParsePiJSONL_SessionHeader(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl", `{"type":"session","version":3,"id":"abc-123","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/home/user/project"}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.CWD != "/home/user/project" {
		t.Errorf("CWD = %q, want /home/user/project", session.CWD)
	}
	if session.SessionID == "" {
		t.Error("SessionID should not be empty")
	}
}

func TestParsePiJSONL_ModelChange(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"model_change","id":"mc1","parentId":null,"timestamp":"2026-03-09T10:00:00.001Z","provider":"anthropic","modelId":"claude-sonnet-4-5"}
{"type":"model_change","id":"mc2","parentId":"mc1","timestamp":"2026-03-09T10:00:05.000Z","provider":"google","modelId":"gemini-3-pro"}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should use the latest model
	if session.Model != "gemini-3-pro" {
		t.Errorf("Model = %q, want gemini-3-pro", session.Model)
	}
}

func TestParsePiJSONL_MessageCounting(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Hello"}],"timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"stop","timestamp":1741514402000}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-09T10:00:03.000Z","message":{"role":"user","content":[{"type":"text","text":"Thanks"}],"timestamp":1741514403000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.UserMessages != 2 {
		t.Errorf("UserMessages = %d, want 2", session.UserMessages)
	}
	if session.AssistantMessages != 1 {
		t.Errorf("AssistantMessages = %d, want 1", session.AssistantMessages)
	}
	if session.TotalMessages != 3 {
		t.Errorf("TotalMessages = %d, want 3", session.TotalMessages)
	}
}

func TestParsePiJSONL_ToolCall_StatusExecutingTool(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Read my file"}],"timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"read","arguments":{"path":"/tmp/test.txt"}}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"toolUse","timestamp":1741514402000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status != claude.StatusExecutingTool {
		t.Errorf("Status = %v, want StatusExecutingTool", session.Status)
	}
	if session.CurrentTool != "Read" {
		t.Errorf("CurrentTool = %q, want Read", session.CurrentTool)
	}
	if len(session.RecentTools) != 1 {
		t.Fatalf("RecentTools length = %d, want 1", len(session.RecentTools))
	}
	if session.RecentTools[0].Name != "Read" {
		t.Errorf("RecentTools[0].Name = %q, want Read", session.RecentTools[0].Name)
	}
}

func TestParsePiJSONL_StatusWaitingForUser(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Hello","timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi there, how can I help?"}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"stop","timestamp":1741514402000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status != claude.StatusWaitingForUser {
		t.Errorf("Status = %v, want StatusWaitingForUser", session.Status)
	}
}

func TestParsePiJSONL_StatusThinking(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Hello","timestamp":1741514401000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status != claude.StatusThinking {
		t.Errorf("Status = %v, want StatusThinking", session.Status)
	}
}

func TestParsePiJSONL_StatusProcessingResult(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Read file","timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"read","arguments":{"path":"/tmp/test.txt"}}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"toolUse","timestamp":1741514402000}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-09T10:00:03.000Z","message":{"role":"toolResult","toolCallId":"tc1","toolName":"read","content":[{"type":"text","text":"file contents here"}],"isError":false,"timestamp":1741514403000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Status != claude.StatusProcessingResult {
		t.Errorf("Status = %v, want StatusProcessingResult", session.Status)
	}
}

func TestParsePiJSONL_WriteToolTracksFilePath(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Write a file","timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"tc1","name":"write","arguments":{"path":"/tmp/output.txt","content":"hello"}}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"toolUse","timestamp":1741514402000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.LastFileWrite != "/tmp/output.txt" {
		t.Errorf("LastFileWrite = %q, want /tmp/output.txt", session.LastFileWrite)
	}
}

func TestParsePiJSONL_Compaction(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Hello","timestamp":1741514401000}}
{"type":"compaction","id":"c1","parentId":"m1","timestamp":"2026-03-09T10:05:00.000Z","summary":"Session summary","firstKeptEntryId":"m1","tokensBefore":50000}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.LastSummaryAt.IsZero() {
		t.Error("LastSummaryAt should not be zero after compaction")
	}
}

func TestParsePiJSONL_RecentMessages(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":[{"type":"text","text":"Hello world"}],"timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi there!"}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":0,"cacheWrite":0,"totalTokens":150,"cost":{"input":0.0003,"output":0.00075,"cacheRead":0,"cacheWrite":0,"total":0.00105}},"stopReason":"stop","timestamp":1741514402000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(session.RecentMessages) != 2 {
		t.Fatalf("RecentMessages length = %d, want 2", len(session.RecentMessages))
	}
	if session.RecentMessages[0].Role != "user" {
		t.Errorf("RecentMessages[0].Role = %q, want user", session.RecentMessages[0].Role)
	}
	if session.RecentMessages[0].Text != "Hello world" {
		t.Errorf("RecentMessages[0].Text = %q, want 'Hello world'", session.RecentMessages[0].Text)
	}
	if session.RecentMessages[1].Role != "assistant" {
		t.Errorf("RecentMessages[1].Role = %q, want assistant", session.RecentMessages[1].Role)
	}
}

func TestParsePiJSONL_UserContentAsString(t *testing.T) {
	// Pi user messages can have content as a plain string
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Plain string content","timestamp":1741514401000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.UserMessages != 1 {
		t.Errorf("UserMessages = %d, want 1", session.UserMessages)
	}
	if len(session.RecentMessages) != 1 {
		t.Fatalf("RecentMessages length = %d, want 1", len(session.RecentMessages))
	}
	if session.RecentMessages[0].Text != "Plain string content" {
		t.Errorf("RecentMessages[0].Text = %q, want 'Plain string content'", session.RecentMessages[0].Text)
	}
}

func TestNormalizePiToolName(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"bash", "Bash"},
		{"read", "Read"},
		{"write", "Write"},
		{"edit", "Edit"},
		{"web_search", "WebSearch"},
		{"find", "Glob"},
		{"process", "Bash"},
		{"subagent", "Agent"},
		{"lsp", "Grep"},
		{"org_todo", "Org_todo"},
		{"custom_tool", "Custom_tool"},
		{"", ""},
	}
	for _, tt := range tests {
		got := normalizePiToolName(tt.input)
		if got != tt.want {
			t.Errorf("normalizePiToolName(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestParsePiJSONL_EmptyFile(t *testing.T) {
	path := writeTempJSONL(t, "empty.jsonl", "")
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.TotalMessages != 0 {
		t.Errorf("TotalMessages = %d, want 0", session.TotalMessages)
	}
}

func TestParsePiJSONL_AgentField(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.Agent != "pi" {
		t.Errorf("Agent = %q, want pi", session.Agent)
	}
}

func TestParsePiJSONL_CostAndTokens(t *testing.T) {
	path := writeTempJSONL(t, "test.jsonl",
		`{"type":"session","version":3,"id":"abc","timestamp":"2026-03-09T10:00:00.000Z","cwd":"/tmp"}
{"type":"message","id":"m1","parentId":null,"timestamp":"2026-03-09T10:00:01.000Z","message":{"role":"user","content":"Hello","timestamp":1741514401000}}
{"type":"message","id":"m2","parentId":"m1","timestamp":"2026-03-09T10:00:02.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Hi"}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":100,"output":50,"cacheRead":10,"cacheWrite":5,"totalTokens":165,"cost":{"input":0.001,"output":0.002,"cacheRead":0.0001,"cacheWrite":0.0005,"total":0.0036}},"stopReason":"stop","timestamp":1741514402000}}
{"type":"message","id":"m3","parentId":"m2","timestamp":"2026-03-09T10:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"More"}],"provider":"anthropic","model":"claude-sonnet-4-5","usage":{"input":200,"output":100,"cacheRead":20,"cacheWrite":10,"totalTokens":330,"cost":{"input":0.002,"output":0.004,"cacheRead":0.0002,"cacheWrite":0.001,"total":0.0072}},"stopReason":"stop","timestamp":1741514403000}}
`)
	session, err := ParsePiJSONL(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if session.InputTokens != 300 {
		t.Errorf("InputTokens = %d, want 300", session.InputTokens)
	}
	if session.OutputTokens != 150 {
		t.Errorf("OutputTokens = %d, want 150", session.OutputTokens)
	}
	wantCost := 0.0036 + 0.0072
	if session.CostUSD < wantCost-0.0001 || session.CostUSD > wantCost+0.0001 {
		t.Errorf("CostUSD = %f, want ~%f", session.CostUSD, wantCost)
	}
}
