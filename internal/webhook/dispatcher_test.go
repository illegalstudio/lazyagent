package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
)

type stubCfg struct{ webhooks []core.WebhookConfig }

func (s stubCfg) Webhooks() []core.WebhookConfig { return s.webhooks }

func TestDispatcher_HappyPath(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	var headers []http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var p map[string]any
		_ = json.Unmarshal(b, &p)
		mu.Lock()
		bodies = append(bodies, p)
		headers = append(headers, r.Header.Clone())
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: 2 * time.Second}, func() string { return "" })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(core.SessionEvent{SessionID: "s1", Agent: "claude", From: core.ActivityIdle, To: core.ActivityWaiting, ProjectPath: "/p", At: time.Now()})

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := len(bodies)
		mu.Unlock()
		if n >= 1 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 1 {
		t.Fatalf("got %d POSTs, want 1", len(bodies))
	}
	if bodies[0]["session_id"] != "s1" || bodies[0]["to"] != "waiting" {
		t.Fatalf("unexpected body: %+v", bodies[0])
	}
	if h := headers[0].Get("X-Lazyagent-Event"); h != "state_transition" {
		t.Errorf("X-Lazyagent-Event = %q", h)
	}
	if h := headers[0].Get("X-Lazyagent-Delivery"); h == "" {
		t.Error("X-Lazyagent-Delivery missing")
	}
	if h := headers[0].Get("Content-Type"); h != "application/json" {
		t.Errorf("Content-Type = %q", h)
	}
}
