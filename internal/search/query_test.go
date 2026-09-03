package search

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
)

func TestFTSQuery(t *testing.T) {
	got := ftsQuery("Cache race, cache!")
	want := "cache* AND race*"
	if got != want {
		t.Fatalf("ftsQuery() = %q, want %q", got, want)
	}
}

func TestIndexSearch(t *testing.T) {
	idx, err := openIndex(filepath.Join(t.TempDir(), "search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer idx.close()

	src := sourceState{
		Agent:   "codex",
		ID:      "s1",
		Path:    "/tmp/s1.jsonl",
		MTimeNS: 123,
		Size:    456,
	}
	chunks := []chunk{{
		Source:    src,
		SessionID: "s1",
		CWD:       "/repo",
		Name:      "debug cache",
		Role:      "user",
		Timestamp: time.Date(2026, 4, 23, 10, 0, 0, 0, time.UTC),
		Text:      "There is a race condition in the cache layer.",
	}}
	if err := idx.replaceSource(src, chunks); err != nil {
		t.Fatal(err)
	}

	current, err := idx.sourceCurrent(src)
	if err != nil {
		t.Fatal(err)
	}
	if !current {
		t.Fatal("source should be current after replaceSource")
	}

	hits, err := idx.search("cache race", []string{"codex"}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits = %d, want 1", len(hits))
	}
	if hits[0].SessionID != "s1" || hits[0].CWD != "/repo" {
		t.Fatalf("unexpected hit: %+v", hits[0])
	}
}

func TestMakeSnippetHighlightsMatch(t *testing.T) {
	snippet := makeSnippet("The cache layer has a subtle race condition.", []string{"race"}, 80)
	if !strings.Contains(snippet, "race") {
		t.Fatalf("snippet %q does not contain match", snippet)
	}
}

func TestNormalizeArgsAllowsFlagsAfterQuery(t *testing.T) {
	got := strings.Join(normalizeArgs([]string{"code", "--limit", "2", "--snippets=1"}), " ")
	want := "--limit 2 --snippets=1 code"
	if got != want {
		t.Fatalf("normalizeArgs() = %q, want %q", got, want)
	}
}

func TestNormalizeArgsHoistsYoloAfterQuery(t *testing.T) {
	got := strings.Join(normalizeArgs([]string{"code", "review", "--yolo"}), " ")
	want := "--yolo code review"
	if got != want {
		t.Fatalf("normalizeArgs() = %q, want %q", got, want)
	}
}

func TestResumeCommand(t *testing.T) {
	tests := []struct {
		agent, executable, wantArgs, wantDisplay string
	}{
		{"codex", "codex", "codex resume abc123", "codex resume abc123"},
		{"grok", "grok", "grok --resume abc123", "grok --resume 'abc123'"},
	}
	for _, tt := range tests {
		t.Run(tt.agent, func(t *testing.T) {
			cmd, display := resumeCommand(tt.agent, "abc123", core.ResumeNormal)
			if cmd == nil {
				t.Fatal("resumeCommand returned nil")
			}
			if filepath.Base(cmd.Path) != tt.executable {
				t.Fatalf("cmd.Path = %q, want %s executable", cmd.Path, tt.executable)
			}
			got := strings.Join(cmd.Args, " ")
			if got != tt.wantArgs {
				t.Fatalf("args = %q, want %q", got, tt.wantArgs)
			}
			if display != tt.wantDisplay {
				t.Fatalf("display = %q, want %q", display, tt.wantDisplay)
			}
		})
	}
}

func TestYoloResumeCommand(t *testing.T) {
	cmd, display := resumeCommand("cursor", "abc123", core.ResumeYolo)
	if cmd == nil {
		t.Fatal("resumeCommand returned nil")
	}
	if got := strings.Join(cmd.Args, " "); got != "cursor-agent --force --resume=abc123" {
		t.Fatalf("args = %q", got)
	}
	if display != `cursor-agent --force --resume="abc123"` {
		t.Fatalf("display = %q", display)
	}
}
