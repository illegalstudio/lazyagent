package codex

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

func TestParseJSONLLargeRecords(t *testing.T) {
	initial := `{"timestamp":"2026-09-04T13:00:00Z","type":"session_meta","payload":{"id":"large-session","cwd":"/tmp/project"}}` + "\n" +
		`{"timestamp":"2026-09-04T13:00:01Z","type":"event_msg","payload":{"type":"user_message"}}` + "\n"
	// Codex can embed large tool results in item_completed events. Large
	// messages must also be parsed, including the records after them.
	largeText := strings.Repeat("x", 5*1024*1024)
	more := `{"timestamp":"2026-09-04T13:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"output":"` + largeText + `"}}}` + "\n" +
		`{"timestamp":"2026-09-08T17:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"` + largeText + `"}]}}` + "\n" +
		`{"timestamp":"2026-09-08T17:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}` + "\n"

	for _, incremental := range []bool{false, true} {
		name := "full"
		if incremental {
			name = "incremental"
		}
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			var base *model.Session
			var offset int64
			if incremental {
				if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
					t.Fatal(err)
				}
				var err error
				base, offset, err = ParseJSONL(path)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(path, []byte(initial+more), 0o600); err != nil {
				t.Fatal(err)
			}
			got, consumed, err := ParseJSONLIncremental(path, offset, base)
			if err != nil {
				t.Fatal(err)
			}
			if consumed != int64(len(initial)+len(more)) {
				t.Fatalf("consumed = %d, want %d", consumed, len(initial)+len(more))
			}
			wantActivity := time.Date(2026, 9, 8, 17, 0, 1, 0, time.UTC)
			if !got.LastActivity.Equal(wantActivity) || got.Status != model.StatusWaitingForUser {
				t.Fatalf("activity/status = %s/%v, want %s/waiting", got.LastActivity, got.Status, wantActivity)
			}
			if got.UserMessages != 1 || got.AssistantMessages != 1 || got.TotalMessages != 2 {
				t.Fatalf("message counts = %d/%d/%d, want 1/1/2", got.UserMessages, got.AssistantMessages, got.TotalMessages)
			}
			if len(got.RecentMessages) != 2 || got.RecentMessages[0].Text != strings.Repeat("x", 300) || got.RecentMessages[1].Text != "done" {
				t.Fatal("large message preview or subsequent assistant message missing")
			}
			if base != nil && (base.TotalMessages != 0 || !base.LastActivity.Before(wantActivity)) {
				t.Fatal("incremental parse changed the cached base session")
			}
		})
	}
}

func TestParseJSONLIncrementalLineBoundaries(t *testing.T) {
	meta := `{"timestamp":"2026-09-08T17:00:00Z","type":"session_meta","payload":{"id":"boundaries"}}`
	message := `{"timestamp":"2026-09-08T17:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"done"}]}}`
	next := `{"timestamp":"2026-09-08T17:00:02Z","type":"event_msg","payload":{"type":"user_message"}}` + "\n"
	for _, tt := range []struct {
		name     string
		first    string
		more     string
		offset   int
		messages int
	}{
		{"LF", meta + "\n" + message + "\n", next, len(meta) + len(message) + 2, 1},
		{"CRLF", meta + "\r\n" + message + "\r\n", next, len(meta) + len(message) + 4, 1},
		{"no final newline", meta + "\n" + message, "\n" + next, len(meta) + len(message) + 1, 1},
		{"partial final record", meta + "\n" + message[:80], message[80:] + "\n" + next, len(meta) + 1, 0},
		{"malformed complete record", meta + "\ninvalid\n" + message + "\n", next, len(meta) + len(message) + 10, 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(tt.first), 0o600); err != nil {
				t.Fatal(err)
			}
			base, offset, err := ParseJSONL(path)
			if err != nil {
				t.Fatal(err)
			}
			if offset != int64(tt.offset) || base.TotalMessages != tt.messages {
				t.Fatalf("offset/messages = %d/%d, want %d/%d", offset, base.TotalMessages, tt.offset, tt.messages)
			}
			appendRollout(t, path, tt.more)
			got, consumed, err := ParseJSONLIncremental(path, offset, base)
			if err != nil {
				t.Fatal(err)
			}
			if consumed != int64(len(tt.first)+len(tt.more)) || got.TotalMessages != 1 {
				t.Fatalf("offset/messages after append = %d/%d, want %d/1", consumed, got.TotalMessages, len(tt.first)+len(tt.more))
			}
			wantActivity := time.Date(2026, 9, 8, 17, 0, 2, 0, time.UTC)
			if !got.LastActivity.Equal(wantActivity) || got.Status != model.StatusThinking {
				t.Fatalf("activity/status after append = %s/%v, want %s/thinking", got.LastActivity, got.Status, wantActivity)
			}
		})
	}
}

func TestDiscoverSessionsRecoversTruncatedPersistedCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	initial := `{"timestamp":"2026-09-04T13:00:00Z","type":"session_meta","payload":{"id":"cached-session"}}` + "\n" +
		`{"timestamp":"2026-09-04T13:00:01Z","type":"event_msg","payload":{"type":"agent_message"}}` + "\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	base, offset, err := ParseJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	appendRollout(t, path,
		`{"timestamp":"2026-09-04T13:00:02Z","type":"event_msg","payload":{"type":"item_completed","item":{"output":"`+strings.Repeat("x", 5*1024*1024)+`"}}}`+"\n"+
			`{"timestamp":"2026-09-08T17:00:00Z","type":"event_msg","payload":{"type":"user_message"}}`+"\n")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// The old scanner cached the file's current mtime with an offset just
	// before the oversized record and an activity timestamp from days ago.
	cache := model.NewSessionCache()
	cache.Put(path, info.ModTime(), offset, base)
	cachePath := filepath.Join(t.TempDir(), "discovery-codex.json")
	if err := cache.SaveTo(cachePath); err != nil {
		t.Fatal(err)
	}
	loaded := model.NewSessionCache()
	if err := loaded.LoadFrom(cachePath); err != nil {
		t.Fatal(err)
	}
	sessions, err := discoverSessionsFromDir(dir, "", loaded, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	wantActivity := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC)
	if !sessions[0].LastActivity.Equal(wantActivity) || sessions[0].Status != model.StatusThinking {
		t.Fatalf("recovered activity/status = %s/%v, want %s/thinking", sessions[0].LastActivity, sessions[0].Status, wantActivity)
	}
}

func appendRollout(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
}

func TestParseJSONLReaderReadError(t *testing.T) {
	content := `{"timestamp":"2026-09-08T17:00:00Z","type":"event_msg","payload":{"type":"user_message"}}` + "\n"
	readErr := errors.New("rollout read failed")
	for _, suffix := range []string{"", `{"timestamp":"2026-09-08`} {
		reader := io.MultiReader(strings.NewReader(content+suffix), iotest.ErrReader(readErr))
		base := &model.Session{SessionID: "read-error", Status: model.StatusWaitingForUser}
		got, offset, err := parseJSONLReader(reader, "session.jsonl", 123, base)
		if !errors.Is(err, readErr) {
			t.Fatalf("error = %v, want wrapped read failure", err)
		}
		if got != nil || offset != 0 {
			t.Fatal("read failure returned a partial session that could be cached")
		}
		if base.Status != model.StatusWaitingForUser || !base.LastActivity.IsZero() {
			t.Fatal("failed parse changed the cached base session")
		}
	}
}
