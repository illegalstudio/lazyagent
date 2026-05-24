package compact

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeKimiSessionForCompact(t *testing.T, big string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kimi-work", "sess-1")
	subDir := filepath.Join(dir, "subagents", "sub-1")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatal(err)
	}

	args := jsonString(`{"path":"README.md","extra":"` + big + `"}`)
	wire := strings.Join([]string{
		`{"type":"metadata","protocol_version":"1"}`,
		`{"type":"message","message":{"type":"TurnBegin","payload":{"user_input":[{"type":"text","text":` + jsonString(big) + `}]}}}`,
		`{"type":"message","message":{"type":"ContentPart","payload":{"type":"text","think":` + jsonString(big) + `,"text":"assistant text"}}}`,
		`{"type":"message","message":{"type":"ToolCall","payload":{"id":"call-1","function":{"name":"ReadFile","arguments":` + args + `}}}}`,
		`{"type":"message","message":{"type":"ToolResult","payload":{"tool_call_id":"call-1","return_value":{"output":` + jsonString(big) + `,"nested":["ok",` + jsonString(big) + `]}}}}`,
		`{"type":"message","message":{"type":"SubagentEvent","payload":{"subagent_id":"sub-1","event":{"type":"ToolResult","payload":{"tool_call_id":"sub-call-1","return_value":{"output":` + jsonString(big) + `}}}}}}`,
		`{"type":"message","message":{"type":"TurnEnd","payload":{}}}`,
	}, "\n") + "\n"

	context := strings.Join([]string{
		`{"role":"user","content":[{"type":"text","text":` + jsonString(big) + `}]}`,
		`{"role":"assistant","content":[{"type":"thinking","thinking":` + jsonString(big) + `},{"type":"text","text":"small"}],"tool_calls":[{"id":"call-1","function":{"name":"ReadFile","arguments":` + args + `}}]}`,
		`{"role":"tool","tool_call_id":"call-1","content":` + jsonString(big) + `}`,
	}, "\n") + "\n"

	files := map[string]string{
		filepath.Join(dir, "wire.jsonl"):                 wire,
		filepath.Join(dir, "context.jsonl"):              context,
		filepath.Join(dir, "state.json"):                 `{"custom_title":"Kimi session"}`,
		filepath.Join(subDir, "wire.jsonl"):              strings.ReplaceAll(wire, "call-1", "sub-call-1"),
		filepath.Join(subDir, "context.jsonl"):           strings.ReplaceAll(context, "call-1", "sub-call-1"),
		filepath.Join(subDir, "output"):                  big,
		filepath.Join(subDir, "prompt.txt"):              "prompt-stays",
		filepath.Join(subDir, "meta.json"):               `{"id":"sub-1"}`,
		filepath.Join(subDir, "unrelated.compact.tmp"):   "ignored",
		filepath.Join(subDir, "previous-output.log.bak"): "ignored",
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestCompactKimiSession(t *testing.T) {
	big := strings.Repeat("K", 50*1024)
	dir := writeKimiSessionForCompact(t, big)

	before := kimiLiveDirBytes(dir)
	newSize, err := compactKimiSession(dir, 10*1024, true)
	if err != nil {
		t.Fatalf("compactKimiSession: %v", err)
	}
	if newSize >= before {
		t.Errorf("size did not shrink: before=%d after=%d", before, newSize)
	}

	if n := countLines(t, filepath.Join(dir, "wire.jsonl")); n != 7 {
		t.Errorf("wire.jsonl line count = %d, want 7", n)
	}
	if n := countLines(t, filepath.Join(dir, "context.jsonl")); n != 3 {
		t.Errorf("context.jsonl line count = %d, want 3", n)
	}
	if n := countLines(t, filepath.Join(dir, "subagents", "sub-1", "wire.jsonl")); n != 7 {
		t.Errorf("subagent wire.jsonl line count = %d, want 7", n)
	}
	if n := countLines(t, filepath.Join(dir, "subagents", "sub-1", "context.jsonl")); n != 3 {
		t.Errorf("subagent context.jsonl line count = %d, want 3", n)
	}

	data, _ := os.ReadFile(filepath.Join(dir, "wire.jsonl"))
	if !strings.Contains(string(data), `"tool_call_id":"call-1"`) {
		t.Error("tool_call_id linkage was lost")
	}
	if !strings.Contains(string(data), `"id":"call-1"`) {
		t.Error("tool call id was lost")
	}

	for _, path := range []string{
		filepath.Join(dir, "wire.jsonl.bak"),
		filepath.Join(dir, "context.jsonl.bak"),
		filepath.Join(dir, "subagents", "sub-1", "wire.jsonl.bak"),
		filepath.Join(dir, "subagents", "sub-1", "context.jsonl.bak"),
		filepath.Join(dir, "subagents", "sub-1", "output.bak"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("expected backup %s", path)
		}
	}

	outputInfo, err := os.Stat(filepath.Join(dir, "subagents", "sub-1", "output"))
	if err != nil {
		t.Fatal(err)
	}
	if outputInfo.Size() >= int64(len(big)) {
		t.Errorf("subagent output did not shrink: size=%d, was %d", outputInfo.Size(), len(big))
	}
	if _, err := os.Stat(filepath.Join(dir, "subagents", "sub-1", "prompt.txt.bak")); !os.IsNotExist(err) {
		t.Error("prompt.txt should not be compacted or backed up")
	}
}

func TestEstimateKimiSession(t *testing.T) {
	big := strings.Repeat("M", 50*1024)
	dir := writeKimiSessionForCompact(t, big)
	before := kimiLiveDirBytes(dir)

	after, err := estimateKimiSession(dir, 10*1024)
	if err != nil {
		t.Fatalf("estimateKimiSession: %v", err)
	}
	if after >= before {
		t.Errorf("estimate did not shrink: before=%d after=%d", before, after)
	}
	if kimiLiveDirBytes(dir) != before {
		t.Error("estimateKimiSession modified the session on disk")
	}
	if _, err := os.Stat(filepath.Join(dir, "wire.jsonl.bak")); !os.IsNotExist(err) {
		t.Error("estimateKimiSession wrote a backup")
	}
}
