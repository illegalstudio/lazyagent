package kilo

import (
	"database/sql"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/illegalstudio/lazyagent/internal/model"
	_ "modernc.org/sqlite"
)

func TestDBPath_UsesKiloDataDir(t *testing.T) {
	t.Setenv("KILO_DATA_DIR", "/tmp/kilo-data")
	got := DBPath()
	want := filepath.Join("/tmp/kilo-data", "kilo.db")
	if got != want {
		t.Fatalf("DBPath() = %q, want %q", got, want)
	}
}

func TestDiscoverSessions_KiloFixture(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("KILO_DATA_DIR", dir)
	loadSQLFixture(t, filepath.Join(dir, "kilo.db"), "testdata/kilo_7_3_12.sql")

	sessions, err := DiscoverSessions(NewSessionCache())
	if err != nil {
		t.Fatalf("DiscoverSessions() error = %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len(sessions) = %d, want 2", len(sessions))
	}

	var main, child *model.Session
	for _, s := range sessions {
		switch s.SessionID {
		case "ses_kilo_main":
			main = s
		case "ses_kilo_child":
			child = s
		}
	}
	if main == nil {
		t.Fatal("missing ses_kilo_main")
	}
	if child == nil {
		t.Fatal("missing ses_kilo_child")
	}

	if main.Agent != "kilo" {
		t.Errorf("main.Agent = %q, want kilo", main.Agent)
	}
	if main.IsSidechain {
		t.Error("main.IsSidechain = true, want false")
	}
	if child.Agent != "kilo" {
		t.Errorf("child.Agent = %q, want kilo", child.Agent)
	}
	if !child.IsSidechain {
		t.Error("child.IsSidechain = false, want true")
	}
	if main.CWD != "/tmp/kilo-project" {
		t.Errorf("main.CWD = %q, want /tmp/kilo-project", main.CWD)
	}
	if main.Name != "Kilo fixture session" {
		t.Errorf("main.Name = %q, want Kilo fixture session", main.Name)
	}
	if main.Version != "7.3.12" {
		t.Errorf("main.Version = %q, want 7.3.12", main.Version)
	}
	if main.Model != "grok-4.3" {
		t.Errorf("main.Model = %q, want grok-4.3", main.Model)
	}
	if main.Status != model.StatusExecutingTool {
		t.Errorf("main.Status = %v, want %v", main.Status, model.StatusExecutingTool)
	}
	if main.CurrentTool != "Edit" {
		t.Errorf("main.CurrentTool = %q, want Edit", main.CurrentTool)
	}
	if main.LastFileWrite != "/tmp/kilo-project/main.go" {
		t.Errorf("main.LastFileWrite = %q, want /tmp/kilo-project/main.go", main.LastFileWrite)
	}
	if main.UserMessages != 1 || main.AssistantMessages != 2 || main.TotalMessages != 3 {
		t.Errorf("message counts = user %d assistant %d total %d, want 1/2/3", main.UserMessages, main.AssistantMessages, main.TotalMessages)
	}
	if main.InputTokens != 60 || main.OutputTokens != 30 || main.CacheReadTokens != 8 || main.CacheCreationTokens != 2 {
		t.Errorf("tokens = in %d out %d read %d write %d, want 60/30/8/2", main.InputTokens, main.OutputTokens, main.CacheReadTokens, main.CacheCreationTokens)
	}
	if math.Abs(main.CostUSD-0.3) > 0.000001 {
		t.Errorf("main.CostUSD = %v, want 0.3", main.CostUSD)
	}
	if len(main.RecentMessages) != 2 {
		t.Fatalf("len(main.RecentMessages) = %d, want 2", len(main.RecentMessages))
	}
	if got := main.RecentMessages[0].Text; got != "Explain this Kilo project" {
		t.Errorf("first recent message = %q, want prompt", got)
	}
	if got := main.RecentMessages[1].Text; got != "It monitors compatible sessions." {
		t.Errorf("second recent message = %q, want assistant text", got)
	}
}

func loadSQLFixture(t *testing.T, dbPath, fixture string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range strings.Split(string(data), ";\n") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" || strings.HasPrefix(stmt, "--") {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("exec fixture statement %q: %v", stmt, err)
		}
	}
}
