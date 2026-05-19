package webhook

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
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

func TestDispatcher_Retry500Then200(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&attempts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)
	d.backoffs = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(core.SessionEvent{SessionID: "s1", To: core.ActivityWaiting, At: time.Now()})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&attempts) >= 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a := atomic.LoadInt32(&attempts); a != 2 {
		t.Fatalf("got %d attempts, want 2", a)
	}
}

func TestDispatcher_NoRetryOn400(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)
	d.backoffs = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(core.SessionEvent{SessionID: "s1", To: core.ActivityWaiting, At: time.Now()})
	time.Sleep(200 * time.Millisecond)

	if a := atomic.LoadInt32(&attempts); a != 1 {
		t.Fatalf("got %d attempts, want 1 (no retry on 4xx)", a)
	}
}

func TestDispatcher_AllAttemptsFail(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)
	d.backoffs = []time.Duration{10 * time.Millisecond, 10 * time.Millisecond, 10 * time.Millisecond}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(core.SessionEvent{SessionID: "s1", To: core.ActivityWaiting, At: time.Now()})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&attempts) >= 4 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if a := atomic.LoadInt32(&attempts); a != 4 {
		t.Fatalf("got %d attempts, want 4 (initial + 3 retries)", a)
	}
}

func TestDispatcher_HMACHeaderWhenSecretSet(t *testing.T) {
	var mu sync.Mutex
	var sigHeader string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("X-Lazyagent-Signature")
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sigHeader = sig
		body = b
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL, Secret: "hello"}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(core.SessionEvent{SessionID: "s1", To: core.ActivityWaiting, At: time.Now()})
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	sig := sigHeader
	b := body
	mu.Unlock()

	if sig == "" {
		t.Fatal("X-Lazyagent-Signature missing")
	}
	if want := Sign("hello", b); sig != want {
		t.Fatalf("got %q, want %q", sig, want)
	}
}

func TestDispatcher_NoHMACWhenSecretEmpty(t *testing.T) {
	var mu sync.Mutex
	var sigHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig := r.Header.Get("X-Lazyagent-Signature")
		mu.Lock()
		sigHeader = sig
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	bus.Publish(core.SessionEvent{SessionID: "s1", To: core.ActivityWaiting, At: time.Now()})
	time.Sleep(200 * time.Millisecond)

	mu.Lock()
	sig := sigHeader
	mu.Unlock()

	if sig != "" {
		t.Fatalf("X-Lazyagent-Signature should be absent, got %q", sig)
	}
}

func TestDispatcher_DedupesSameTransitionWithinWindow(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	now := time.Now()
	ev := core.SessionEvent{SessionID: "s1", From: core.ActivityIdle, To: core.ActivityWaiting, At: now}
	bus.Publish(ev)
	bus.Publish(ev) // duplicate from a second manager
	bus.Publish(ev) // duplicate from a third

	time.Sleep(300 * time.Millisecond)

	if a := atomic.LoadInt32(&attempts); a != 1 {
		t.Fatalf("got %d POSTs, want 1 (dedup)", a)
	}
}

func TestDispatcher_DistinctTransitionsNotDeduped(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Start(ctx) }()
	time.Sleep(20 * time.Millisecond)

	now := time.Now()
	bus.Publish(core.SessionEvent{SessionID: "s1", From: core.ActivityIdle, To: core.ActivityWaiting, At: now})
	bus.Publish(core.SessionEvent{SessionID: "s1", From: core.ActivityWaiting, To: core.ActivityThinking, At: now})

	time.Sleep(300 * time.Millisecond)

	if a := atomic.LoadInt32(&attempts); a != 2 {
		t.Fatalf("got %d POSTs, want 2 (distinct transitions)", a)
	}
}
