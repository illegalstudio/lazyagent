package search

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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
