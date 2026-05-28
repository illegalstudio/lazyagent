package opencodefamily

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
	_ "modernc.org/sqlite"
)

func TestDiscoverSessionsFor_OpenCodeCompatibleSource(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TEST_KILO_DATA_DIR", dir)

	source := Source{
		Agent:      "kilo",
		EnvVar:     "TEST_KILO_DATA_DIR",
		DataSubdir: "kilo",
		DBFile:     "kilo.db",
	}
	writeCompatDB(t, filepath.Join(dir, "kilo.db"))

	sessions, err := DiscoverSessionsFor(source, NewSessionCache())
	if err != nil {
		t.Fatalf("DiscoverSessionsFor() error = %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len(sessions) = %d, want 1", len(sessions))
	}

	got := sessions[0]
	if got.Agent != "kilo" {
		t.Fatalf("Agent = %q, want kilo", got.Agent)
	}
	if got.SessionID != "ses_test" {
		t.Errorf("SessionID = %q, want ses_test", got.SessionID)
	}
	if got.CWD != "/tmp/project" {
		t.Errorf("CWD = %q, want /tmp/project", got.CWD)
	}
	if got.Name != "Test session" {
		t.Errorf("Name = %q, want Test session", got.Name)
	}
	if got.Version != "7.3.12" {
		t.Errorf("Version = %q, want 7.3.12", got.Version)
	}
	if got.Model != "grok-4.3" {
		t.Errorf("Model = %q, want grok-4.3", got.Model)
	}
	if got.Status != model.StatusExecutingTool {
		t.Errorf("Status = %v, want %v", got.Status, model.StatusExecutingTool)
	}
	if got.CurrentTool != "Write" {
		t.Errorf("CurrentTool = %q, want Write", got.CurrentTool)
	}
	if got.LastFileWrite != "/tmp/project/main.go" {
		t.Errorf("LastFileWrite = %q, want /tmp/project/main.go", got.LastFileWrite)
	}
	if got.UserMessages != 1 || got.AssistantMessages != 1 || got.TotalMessages != 2 {
		t.Errorf("message counts = user %d assistant %d total %d, want 1/1/2", got.UserMessages, got.AssistantMessages, got.TotalMessages)
	}
	if got.InputTokens != 10 || got.OutputTokens != 20 || got.CacheReadTokens != 3 || got.CacheCreationTokens != 4 {
		t.Errorf("tokens = in %d out %d read %d write %d, want 10/20/3/4", got.InputTokens, got.OutputTokens, got.CacheReadTokens, got.CacheCreationTokens)
	}
	if got.CostUSD != 0.5 {
		t.Errorf("CostUSD = %v, want 0.5", got.CostUSD)
	}
	if len(got.RecentMessages) != 1 || got.RecentMessages[0].Text != "hello" {
		t.Fatalf("RecentMessages = %#v, want one user text", got.RecentMessages)
	}
	if len(got.RecentTools) != 1 || got.RecentTools[0].Name != "Write" {
		t.Fatalf("RecentTools = %#v, want one Write tool", got.RecentTools)
	}

	wantToolTime := time.UnixMilli(1700000001000)
	if !got.LastFileWriteAt.Equal(wantToolTime) {
		t.Errorf("LastFileWriteAt = %v, want %v", got.LastFileWriteAt, wantToolTime)
	}
	if !got.LastActivity.Equal(time.UnixMilli(1700000002000)) {
		t.Errorf("LastActivity = %v, want session time_updated", got.LastActivity)
	}
}

func TestDataDirFor_UsesEnvOverride(t *testing.T) {
	t.Setenv("TEST_AGENT_DATA_DIR", "/tmp/test-agent")
	got := DataDirFor(Source{Agent: "test-agent", EnvVar: "TEST_AGENT_DATA_DIR"})
	if got != "/tmp/test-agent" {
		t.Fatalf("DataDirFor() = %q, want /tmp/test-agent", got)
	}
}

func writeCompatDB(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	stmts := []string{
		`CREATE TABLE session (
			id text PRIMARY KEY,
			project_id text NOT NULL,
			parent_id text,
			directory text NOT NULL,
			title text NOT NULL,
			version text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			time_compacting integer,
			time_archived integer
		)`,
		`CREATE TABLE message (
			id text PRIMARY KEY,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`CREATE TABLE part (
			id text PRIMARY KEY,
			message_id text NOT NULL,
			session_id text NOT NULL,
			time_created integer NOT NULL,
			time_updated integer NOT NULL,
			data text NOT NULL
		)`,
		`INSERT INTO session (id, project_id, parent_id, directory, title, version, time_created, time_updated, time_compacting, time_archived)
		 VALUES ('ses_test', 'proj_test', NULL, '/tmp/project', 'Test session', '7.3.12', 1700000000000, 1700000002000, NULL, NULL)`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_user', 'ses_test', 1700000000000, 1700000000000, '{"role":"user","time":{"created":1700000000000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_user_text', 'msg_user', 'ses_test', 1700000000000, 1700000000000, '{"type":"text","text":"hello"}')`,
		`INSERT INTO message (id, session_id, time_created, time_updated, data)
		 VALUES ('msg_assistant', 'ses_test', 1700000001000, 1700000001000, '{"role":"assistant","modelID":"grok-4.3","providerID":"xai","cost":0.5,"tokens":{"input":10,"output":20,"cache":{"read":3,"write":4}},"finish":"tool-calls","time":{"created":1700000001000}}')`,
		`INSERT INTO part (id, message_id, session_id, time_created, time_updated, data)
		 VALUES ('part_tool', 'msg_assistant', 'ses_test', 1700000001000, 1700000001000, '{"type":"tool","tool":"write","state":{"input":{"file_path":"/tmp/project/main.go"}}}')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec %q: %v", stmt, err)
		}
	}
}
