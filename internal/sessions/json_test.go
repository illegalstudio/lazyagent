package sessions

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestWriteJSONFields(t *testing.T) {
	last := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	list := []*model.Session{{
		Agent: "claude", SessionID: "abc", CWD: "/proj",
		LastActivity: last, TotalMessages: 5,
	}}
	var buf bytes.Buffer
	if err := writeJSON(&buf, list, func(*model.Session) string { return "my-alias" }); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("want 1 entry, got %d", len(out))
	}
	e := out[0]
	if e["agent"] != "claude" || e["session_id"] != "abc" || e["cwd"] != "/proj" {
		t.Errorf("identity fields wrong: %v", e)
	}
	if e["name"] != "my-alias" {
		t.Errorf("name = %v, want my-alias", e["name"])
	}
	if e["messages"] != float64(5) {
		t.Errorf("messages = %v, want 5", e["messages"])
	}
	if e["resume_command"] != "claude --resume abc" {
		t.Errorf("resume_command = %v", e["resume_command"])
	}
	if e["resume_command_yolo"] != "claude --dangerously-skip-permissions --resume abc" {
		t.Errorf("resume_command_yolo = %v", e["resume_command_yolo"])
	}
	if _, ok := e["last_activity"]; !ok {
		t.Error("missing last_activity")
	}
}

func TestWriteJSONAllFieldsPresentEvenWhenEmpty(t *testing.T) {
	last := time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)
	list := []*model.Session{
		{Agent: "grok", SessionID: "g1", CWD: "/proj/grok", LastActivity: last, TotalMessages: 1},
		{Agent: "claude", SessionID: "c1", CWD: "/proj/claude", LastActivity: last, TotalMessages: 2},
	}
	wantKeys := []string{"agent", "session_id", "name", "cwd", "last_activity", "messages", "resume_command", "resume_command_yolo"}
	var buf bytes.Buffer
	// nameFor returns "" for every session (unnamed), exercising the omitted-name case too.
	if err := writeJSON(&buf, list, func(*model.Session) string { return "" }); err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("want 2 entries, got %d", len(out))
	}
	// Rows must appear in input order.
	if out[0]["session_id"] != "g1" || out[1]["session_id"] != "c1" {
		t.Errorf("rows out of input order: %v", out)
	}
	for i, row := range out {
		for _, k := range wantKeys {
			v, ok := row[k]
			if !ok {
				t.Errorf("row %d missing key %q: %v", i, k, row)
			}
			_ = v
		}
	}
	// Neither session has a name, so both names must be present as empty
	// strings rather than omitted.
	if out[0]["name"] != "" {
		t.Errorf("grok row name = %v, want empty string", out[0]["name"])
	}
	if out[0]["resume_command"] != "grok --resume 'g1'" {
		t.Errorf("grok row resume_command = %v, want grok --resume 'g1'", out[0]["resume_command"])
	}
	if out[0]["resume_command_yolo"] != "grok --yolo --resume 'g1'" {
		t.Errorf("grok row resume_command_yolo = %v", out[0]["resume_command_yolo"])
	}
	if out[1]["name"] != "" {
		t.Errorf("claude row name = %v, want empty string", out[1]["name"])
	}
	if out[1]["resume_command"] != "claude --resume c1" {
		t.Errorf("claude row resume_command = %v, want claude --resume c1", out[1]["resume_command"])
	}
}

func TestWriteJSONEmptyList(t *testing.T) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, nil, func(*model.Session) string { return "" }); err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(buf.String()); got != "[]" {
		t.Errorf("empty list must encode as [], got %q", got)
	}
}
