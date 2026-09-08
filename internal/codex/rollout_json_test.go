package codex

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

// repeatJSONReader generates large inputs without allocating their contents.
type repeatJSONReader struct {
	pattern string
	offset  int
}

func (r *repeatJSONReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = r.pattern[r.offset]
		r.offset = (r.offset + 1) % len(r.pattern)
	}
	return len(p), nil
}

func TestParseJSONLBoundedMemory(t *testing.T) {
	const size = 64 * 1024 * 1024
	for _, tt := range []struct {
		name, prefix, pattern, suffix string
		messages                      int
	}{
		{"tool output", `{"payload":{"type":"function_call_output","output":"`, "x", `"},"type":"response_item","timestamp":"2026-09-08T17:00:00Z"}`, 0},
		{"message", `{"payload":{"content":[{"text":"`, "x", `","type":"output_text"}],"role":"assistant","type":"message"},"type":"response_item","timestamp":"2026-09-08T17:00:00Z"}`, 1},
		{"structured output", `{"payload":{"type":"function_call_output","output":[`, `{"x":0},`, `null]},"type":"response_item","timestamp":"2026-09-08T17:00:00Z"}`, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			length := int64(size / len(tt.pattern) * len(tt.pattern))
			input := io.MultiReader(strings.NewReader(tt.prefix), io.LimitReader(&repeatJSONReader{pattern: tt.pattern}, length), strings.NewReader(tt.suffix+"\n"))
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			got, offset, err := parseJSONLReader(input, "generated.jsonl", 0, nil)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatal(err)
			}
			// This bounds even cumulative allocation, a stronger check than
			// peak live memory. ReadBytes allocated multiples of the input.
			if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 2*1024*1024 {
				t.Fatalf("allocated %d bytes for a %d-byte record, want < 2 MiB", allocated, length)
			}
			t.Logf("record bytes=%d, allocated bytes=%d", length, after.TotalAlloc-before.TotalAlloc)
			if want := int64(len(tt.prefix)+len(tt.suffix)+1) + length; offset != want {
				t.Fatalf("offset = %d, want %d", offset, want)
			}
			if want := time.Date(2026, 9, 8, 17, 0, 0, 0, time.UTC); !got.LastActivity.Equal(want) {
				t.Fatalf("activity = %s, want %s", got.LastActivity, want)
			}
			if got.TotalMessages != tt.messages {
				t.Fatalf("messages = %d, want %d", got.TotalMessages, tt.messages)
			}
			if tt.messages > 0 && (len(got.RecentMessages) != 1 || got.RecentMessages[0].Text != strings.Repeat("x", 300)) {
				t.Fatal("large message preview was not preserved")
			}
		})
	}
}

func TestLargeRolloutMessagePreview(t *testing.T) {
	for _, text := range []string{
		strings.Repeat(" \n\u2003", rolloutBufferSize) + strings.Repeat("😀", 400),
		"start" + strings.Repeat(" ", rolloutBufferSize) + "end",
		"start" + strings.Repeat(" ", rolloutBufferSize),
	} {
		encoded, err := json.Marshal(text)
		if err != nil {
			t.Fatal(err)
		}
		record := `{"timestamp":"2026-09-08T17:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":` + string(encoded) + `}]}}` + "\n"
		got, offset, err := parseJSONLReader(strings.NewReader(record), "preview.jsonl", 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		want := []rune(strings.TrimSpace(text))
		if len(want) > 300 {
			want = want[:300]
		}
		if offset != int64(len(record)) || len(got.RecentMessages) != 1 || got.RecentMessages[0].Text != string(want) {
			t.Fatal("preview changed after streaming whitespace or Unicode")
		}
	}
}

func TestLargeRolloutContentArray(t *testing.T) {
	ignored := `{"type":"image","text":"ignored"},`
	content := io.MultiReader(
		strings.NewReader(`{"payload":{"type":"message","role":"assistant","content":[`),
		io.LimitReader(&repeatJSONReader{pattern: ignored}, int64(len(ignored)*100000)),
		strings.NewReader(`{"type":"output_text","text":"still visible"}]},"type":"response_item","timestamp":"2026-09-08T17:00:00Z"}`+"\n"),
	)
	got, _, err := parseJSONLReader(content, "array.jsonl", 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.RecentMessages) != 1 || got.RecentMessages[0].Text != "still visible" {
		t.Fatal("content after a large array of non-text blocks was lost")
	}
}

func TestLargeRolloutMetadataLimit(t *testing.T) {
	record := `{"timestamp":"2026-09-08T17:00:00Z","type":"session_meta","payload":{"cwd":"` + strings.Repeat("x", maxMetadataBytes+1) + `","id":"valid-id"}}` + "\n"
	got, offset, err := parseJSONLReader(strings.NewReader(record), "metadata.jsonl", 0, nil)
	if err != nil || offset != int64(len(record)) || got.CWD != "" || got.SessionID != "valid-id" {
		t.Fatalf("oversized metadata produced a truncated identity: session=%#v offset=%d err=%v", got, offset, err)
	}
}

func TestRolloutProjectionMatchesSmallRecords(t *testing.T) {
	for _, record := range []string{
		`{"timestamp":"2026-09-08T17:00:00Z","type":"session_meta","payload":{"id":"s1","cwd":"/tmp/project","cli_version":"1.0","agent_nickname":" ","source":"cli"}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"session_meta","payload":{"id":"s1","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent"}}}}}`,
		`{"TIMESTAMP":"2026-09-08T17:00:00Z","TYPE":"turn_context","PAYLOAD":{"CWD":"/tmp/new","MODEL":"model","GIT":{"BRANCH":"feature"}}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":"  hello\n\uD83D\uDE00\ud800x\udc00","type":"output_text"},{"type":"image","text":"ignored"},{"type":"output_text","text":"  world  "}]}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"text":false,"type":"output_text"}]}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[false]}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"response_item","payload":{"name":"apply_patch","arguments":"ignored","type":"function_call"}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":12,"cached_input_tokens":3,"output_tokens":4,"reasoning_output_tokens":5}}}}`,
		`{"timestamp":"2026-09-08T17:00:00Z","type":"event_msg","payload":{"type":"task_complete","last_agent_message":"\u0020\tfinished"}}`,
	} {
		for _, ending := range []string{"", "\n", "\r\n"} {
			want, _, err := parseJSONLReader(strings.NewReader(record+ending), "session.jsonl", 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			// Whitespace forces the streaming path without changing the JSON.
			large := strings.Repeat(" ", rolloutBufferSize+1) + record + ending
			got, offset, err := parseJSONLReader(strings.NewReader(large), "session.jsonl", 0, nil)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) || offset != int64(len(large)) {
				t.Fatalf("projection changed session or offset for %s (ending %q):\ngot %#v, offset %d\nwant %#v, offset %d", record, ending, got, offset, want, len(large))
			}
		}
	}
}

func TestLargeRolloutPartialAndInvalidRecords(t *testing.T) {
	prefix := `{"ignored":"` + strings.Repeat("x", rolloutBufferSize*2)
	suffix := `","timestamp":"2026-09-08T17:00:00Z","type":"event_msg","payload":{"type":"user_message"}}`
	for _, tt := range []struct {
		name, content string
		terminated    bool
	}{
		{"partial string", prefix, false},
		{"invalid escape", prefix + `\q"}` + "\n", true},
		{"invalid suffix", prefix + suffix + "!\n", true},
		{"deep nesting", strings.Repeat("[", maxJSONDepth+2) + prefix + "\n", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			reader := bufio.NewReaderSize(strings.NewReader(tt.content), rolloutBufferSize)
			projected, consumed, terminated, err := readRolloutRecord(reader)
			if err != nil || projected != nil || consumed != int64(len(tt.content)) || terminated != tt.terminated {
				t.Fatalf("invalid/partial record: projected=%s consumed=%d terminated=%v err=%v", projected, consumed, terminated, err)
			}
		})
	}
	path := t.TempDir() + "/partial.jsonl"
	if err := os.WriteFile(path, []byte(prefix), 0o600); err != nil {
		t.Fatal(err)
	}
	base, offset, err := ParseJSONL(path)
	if err != nil || offset != 0 {
		t.Fatalf("partial parse offset=%d err=%v", offset, err)
	}
	appendRollout(t, path, suffix+"\n")
	got, offset, err := ParseJSONLIncremental(path, offset, base)
	if err != nil || offset != int64(len(prefix)+len(suffix)+1) || got.LastActivity.IsZero() {
		t.Fatalf("completed record offset=%d err=%v session=%#v", offset, err, got)
	}
}

func TestLargeRolloutReadError(t *testing.T) {
	readErr := errors.New("read failure after large prefix")
	for _, prefix := range []string{`{"ignored":"`, `!`} {
		input := io.MultiReader(strings.NewReader(prefix), io.LimitReader(&repeatJSONReader{pattern: "x"}, rolloutBufferSize*2), iotest.ErrReader(readErr))
		got, offset, err := parseJSONLReader(input, "session.jsonl", 0, nil)
		if !errors.Is(err, readErr) || got != nil || offset != 0 {
			t.Fatalf("read failure returned session=%#v offset=%d err=%v", got, offset, err)
		}
	}
}

func FuzzRolloutJSONSyntax(f *testing.F) {
	for _, seed := range []string{
		`{"payload":{"content":[{"type":"input_text","text":"hello"}]}}`,
		`{"a":[0,-12.3e+4,true,false,null,"\ud83d\ude00"]}`,
		`{"a":"\ud800\ud800\udc00"}`, `{"a":"\q"}`, `[01]`, `[1.]`, `[1e]`, `{"a":0,}`, `[]`, `null`,
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		if len(input) > 4096 {
			t.Skip()
		}
		p := rolloutJSONParser{reader: bufio.NewReader(strings.NewReader(input))}
		_, err := p.value(rolloutProjection, 0, nil)
		_, endErr := p.peek()
		valid := err == nil && endErr == io.EOF
		if valid != json.Valid([]byte(input)) {
			t.Fatalf("valid=%v, encoding/json valid=%v for %q", valid, json.Valid([]byte(input)), input)
		}
	})
}
