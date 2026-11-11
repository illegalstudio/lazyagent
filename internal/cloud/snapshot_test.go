package cloud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSnapshotRefreshAndClose(t *testing.T) {
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			close(started)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		fmt.Fprint(w, `{"items":[{"id":"one","status":"ACTIVE","repos":[]}]}`)
	}))
	defer upstream.Close()
	c := NewCursor(upstream.URL, "test-key")
	defer c.Close()
	items, state := c.Snapshot()
	if len(items) != 0 || !state.Configured || !state.Loading {
		t.Fatalf("initial snapshot: %+v %+v", items, state)
	}
	<-started
	for range 10 {
		c.Refresh()
		c.Snapshot()
	}
	if calls.Load() != 1 {
		t.Fatal("in-flight snapshots must coalesce")
	}
	close(release)
	if _, err := c.List(); err != nil {
		t.Fatal(err)
	}
	items, state = c.Snapshot()
	if len(items) != 1 || state.Loading || state.Revision != 1 {
		t.Fatalf("completed snapshot: %+v %+v", items, state)
	}
	items[0].Name = "mutation"
	if got, _ := c.Snapshot(); got[0].Name == "mutation" {
		t.Fatal("snapshot aliases client cache")
	}
	c.Refresh()
	deadline := time.Now().Add(time.Second)
	for state.Revision != 2 && time.Now().Before(deadline) {
		_, state = c.Snapshot()
		time.Sleep(time.Millisecond)
	}
	if state.Revision != 2 || calls.Load() != 2 {
		t.Fatal("explicit refresh must bypass TTL")
	}
	c.Close()
	c.Refresh()
	if _, state := c.Snapshot(); state.Loading {
		t.Fatal("closed client restarted")
	}
}

func TestAgentURL(t *testing.T) {
	for _, raw := range []string{"https://cursor.com/agents/one", "https://cursor.com/agents/one?tab=files"} {
		if _, err := AgentURL(raw); err != nil {
			t.Errorf("rejected %q: %v", raw, err)
		}
	}
	for _, raw := range []string{"javascript:alert(1)", "file:///tmp/one", "https://evil.example/agents/one", "https://cursor.com.evil.example/agents/one", "https://user@cursor.com/agents/one", "https://cursor.com:1234/agents/one", "http://cursor.com/agents/one", "https://cursor.com/dashboard"} {
		if _, err := AgentURL(raw); err == nil {
			t.Errorf("accepted unsafe URL %q", raw)
		}
	}
}
