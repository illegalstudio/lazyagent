package compact

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain sets testGuardRoots to the OS temp directory so that
// compactGrokSession can write to temp dirs created by t.TempDir().
func TestMain(m *testing.M) {
	testGuardRoots = []string{os.TempDir()}
	os.Exit(m.Run())
}

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
	return grokLiveDirBytes(dir)
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

func TestCompactGrokSession_TerminalLog(t *testing.T) {
	dir := writeGrokSessionForCompact(t, "small payload") // chat/updates stay tiny
	logDir := filepath.Join(dir, "terminal")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(logDir, "call-1.log")
	big := strings.Repeat("C", 50*1024) // 50 KiB raw log, above the 10 KiB threshold
	if err := os.WriteFile(logPath, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := compactGrokSession(dir, 10*1024, true); err != nil {
		t.Fatalf("compactGrokSession: %v", err)
	}
	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() >= int64(len(big)) {
		t.Errorf("terminal log did not shrink: size=%d, was %d", info.Size(), len(big))
	}
	if _, err := os.Stat(logPath + ".bak"); err != nil {
		t.Error("expected a .bak backup of the terminal log")
	}
}
