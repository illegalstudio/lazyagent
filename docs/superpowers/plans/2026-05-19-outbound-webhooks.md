# Outbound Webhooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add outbound HTTP webhooks that fire on session activity state transitions, with per-webhook event/agent filters, optional HMAC-SHA256 signing, and async best-effort delivery.

**Architecture:** New typed `core.EventBus` published from `ActivityTracker.Update` when previous activity differs from new; new `internal/webhook/` package subscribes to the bus, applies per-webhook filters, and delivers via a bounded-queue worker pool with retry. The dispatcher dedupes duplicate transitions from multiple in-process managers within a short window.

**Tech Stack:** Go 1.21+, stdlib only (`net/http`, `crypto/hmac`, `crypto/sha256`, `encoding/json`, `context`, `sync`). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-05-19-outbound-webhooks-design.md`

---

## File Map

**Create:**
- `internal/core/eventbus.go` — `EventBus`, `SessionEvent`
- `internal/core/eventbus_test.go`
- `internal/webhook/dispatcher.go` — `Dispatcher`, `ConfigSource`, lifecycle
- `internal/webhook/dispatcher_test.go`
- `internal/webhook/payload.go` — `Payload`, marshal helper
- `internal/webhook/payload_test.go`
- `internal/webhook/filter.go` — `Matches(WebhookConfig, SessionEvent) bool`
- `internal/webhook/filter_test.go`
- `internal/webhook/hmac.go` — `Sign(secret, body []byte) string`
- `internal/webhook/hmac_test.go`
- `docs/reference/webhooks.md` — user-facing reference page

**Modify:**
- `internal/core/activity.go` — `ActivityTracker` gains `bus *EventBus` + `SetEventBus`; `Update` emits transitions
- `internal/core/activity_test.go` — new test cases
- `internal/core/config.go` — `WebhookConfig` type, `Webhooks []WebhookConfig` on `Config`, validation
- `internal/core/config_test.go` — validation tests
- `internal/core/session.go` — `SetEventBus` on `SessionManager`, propagates to tracker
- `internal/ui/app.go` — accept and wire bus
- `internal/tray/service.go` — accept and wire bus (guarded by `!notray`)
- `internal/api/server.go` — accept and wire bus
- `main.go` — construct bus, wire to managers, start dispatcher when `cfg.Webhooks` non-empty
- `docs/reference/configuration.md` — document `webhooks` field
- `docs/reference/roadmap.md` — add `v0.10 — Outbound webhooks` section (or leave to upstream merger)
- `README.md` — one-line mention under features

---

## Task 1: EventBus core type

**Files:**
- Create: `internal/core/eventbus.go`
- Test: `internal/core/eventbus_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// internal/core/eventbus_test.go
package core

import (
	"sync"
	"testing"
	"time"
)

func TestEventBus_PublishSubscribe(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(4)
	defer bus.Unsubscribe(ch)

	want := SessionEvent{SessionID: "s1", From: ActivityIdle, To: ActivityThinking, At: time.Unix(0, 0)}
	bus.Publish(want)

	select {
	case got := <-ch:
		if got != want {
			t.Fatalf("got %+v, want %+v", got, want)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive event")
	}
}

func TestEventBus_DropOnFullSubscriber(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1)
	defer bus.Unsubscribe(ch)

	bus.Publish(SessionEvent{SessionID: "a"})
	bus.Publish(SessionEvent{SessionID: "b"}) // dropped
	bus.Publish(SessionEvent{SessionID: "c"}) // dropped

	got := <-ch
	if got.SessionID != "a" {
		t.Fatalf("got %q, want %q", got.SessionID, "a")
	}
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra event: %+v", extra)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestEventBus_UnsubscribeIdempotent(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1)
	bus.Unsubscribe(ch)
	bus.Unsubscribe(ch) // must not panic

	// Publish after unsubscribe must not block or send to the closed channel.
	done := make(chan struct{})
	go func() {
		bus.Publish(SessionEvent{SessionID: "x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish after Unsubscribe blocked")
	}
}

func TestEventBus_ConcurrentPublish(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(1024)
	defer bus.Unsubscribe(ch)

	var wg sync.WaitGroup
	const n = 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			bus.Publish(SessionEvent{SessionID: "x"})
		}(i)
	}
	wg.Wait()

	count := 0
	for {
		select {
		case <-ch:
			count++
		case <-time.After(50 * time.Millisecond):
			if count != n {
				t.Fatalf("received %d events, want %d", count, n)
			}
			return
		}
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/core/ -run TestEventBus -v`
Expected: compile error — `EventBus`, `SessionEvent`, `NewEventBus` undefined.

- [ ] **Step 3: Implement the EventBus**

```go
// internal/core/eventbus.go
package core

import (
	"sync"
	"time"
)

// SessionEvent is published when a session's resolved activity changes.
type SessionEvent struct {
	SessionID   string
	Agent       string
	From        ActivityKind
	To          ActivityKind
	At          time.Time
	ProjectPath string
}

// EventBus is a minimal typed pub-sub for in-process subscribers.
// Publish never blocks; events are dropped for subscribers whose channel is full.
type EventBus struct {
	mu   sync.RWMutex
	subs []chan SessionEvent
}

// NewEventBus returns a ready-to-use EventBus.
func NewEventBus() *EventBus { return &EventBus{} }

// Subscribe registers a new subscriber and returns its channel.
// buf is the channel buffer; pick a size matching the subscriber's drain rate.
func (b *EventBus) Subscribe(buf int) <-chan SessionEvent {
	if buf < 1 {
		buf = 1
	}
	ch := make(chan SessionEvent, buf)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes the channel from the bus. Safe to call multiple times.
// The caller must not read from the channel after Unsubscribe returns.
func (b *EventBus) Unsubscribe(ch <-chan SessionEvent) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for i, sub := range b.subs {
		if sub == ch {
			b.subs = append(b.subs[:i], b.subs[i+1:]...)
			return
		}
	}
}

// Publish sends e to every subscriber. Non-blocking: subscribers whose channel
// is full miss this event.
func (b *EventBus) Publish(e SessionEvent) {
	b.mu.RLock()
	subs := b.subs
	b.mu.RUnlock()
	for _, ch := range subs {
		select {
		case ch <- e:
		default:
			// dropped; subscribers are responsible for keeping up
		}
	}
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/core/ -run TestEventBus -race -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/core/eventbus.go internal/core/eventbus_test.go
git commit -m "feat(core): add typed EventBus for in-process pub-sub"
```

---

## Task 2: ActivityTracker emits transitions

**Files:**
- Modify: `internal/core/activity.go`
- Modify: `internal/core/activity_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/core/activity_test.go`:

```go
func TestActivityTracker_EmitsTransitionOnChange(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(8)
	defer bus.Unsubscribe(ch)

	tr := NewActivityTracker()
	tr.SetEventBus(bus)

	now := time.Now()
	s := &model.Session{SessionID: "s1", Agent: "claude", CWD: "/p", LastActivity: now, Status: model.StatusThinking}
	tr.Update([]*model.Session{s}, now)

	// First Update: new session emits Unknown→Thinking.
	select {
	case ev := <-ch:
		if ev.SessionID != "s1" || ev.Agent != "claude" || ev.From != "" || ev.To != ActivityThinking || ev.ProjectPath != "/p" {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event emitted")
	}

	// Same activity: no event.
	tr.Update([]*model.Session{s}, now)
	select {
	case ev := <-ch:
		t.Fatalf("unexpected event on unchanged state: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}

	// Status flips to waiting (after grace).
	s.Status = model.StatusWaitingForUser
	tr.Update([]*model.Session{s}, now.Add(WaitingGrace+time.Second))
	select {
	case ev := <-ch:
		if ev.From != ActivityThinking || ev.To != ActivityWaiting {
			t.Fatalf("unexpected transition: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event on change")
	}
}

func TestActivityTracker_NilBusSafe(t *testing.T) {
	tr := NewActivityTracker()
	// No SetEventBus call. Must not panic.
	s := &model.Session{SessionID: "s1", Agent: "claude", LastActivity: time.Now(), Status: model.StatusThinking}
	tr.Update([]*model.Session{s}, time.Now())
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/core/ -run TestActivityTracker_ -v`
Expected: compile error — `SetEventBus` undefined, plus the new test cases fail.

- [ ] **Step 3: Modify ActivityTracker to emit transitions**

Edit `internal/core/activity.go`:

Replace the `ActivityTracker` struct and `NewActivityTracker`:

```go
// ActivityTracker manages sticky activity states with grace period logic.
// When an EventBus is attached, transitions are published on Update.
type ActivityTracker struct {
	activities   map[string]*ActivityEntry
	waitingSince map[string]time.Time
	bus          *EventBus            // optional, nil-safe
	agents       map[string]string    // session_id → agent name (for events)
	projects     map[string]string    // session_id → CWD (for events)
}

// NewActivityTracker creates a new ActivityTracker.
func NewActivityTracker() *ActivityTracker {
	return &ActivityTracker{
		activities:   make(map[string]*ActivityEntry),
		waitingSince: make(map[string]time.Time),
		agents:       make(map[string]string),
		projects:     make(map[string]string),
	}
}

// SetEventBus attaches a bus so Update will publish transition events.
// Passing nil clears any previously attached bus.
func (t *ActivityTracker) SetEventBus(bus *EventBus) {
	t.bus = bus
}
```

Then replace the `Update` method body — the new logic compares previous vs new and publishes:

```go
// Update resolves and stores the current activity for each session.
// Applies a grace period before showing ActivityWaiting to avoid false positives.
// If an EventBus is attached, transitions are published.
func (t *ActivityTracker) Update(sessions []*model.Session, now time.Time) {
	activeIDs := make(map[string]struct{}, len(sessions))
	for _, s := range sessions {
		id := s.SessionID
		if id == "" {
			continue
		}
		activeIDs[id] = struct{}{}
		activity := ResolveActivity(s, now)

		if activity == ActivityWaiting {
			if _, seen := t.waitingSince[id]; !seen {
				t.waitingSince[id] = now
			}
			if now.Sub(t.waitingSince[id]) < WaitingGrace {
				continue
			}
		} else {
			delete(t.waitingSince, id)
		}

		var prev ActivityKind
		if e, ok := t.activities[id]; ok {
			prev = e.Kind
		}
		t.activities[id] = &ActivityEntry{Kind: activity, LastSeen: now}
		t.agents[id] = s.Agent
		t.projects[id] = s.CWD

		if t.bus != nil && prev != activity {
			t.bus.Publish(SessionEvent{
				SessionID:   id,
				Agent:       s.Agent,
				From:        prev,
				To:          activity,
				At:          now,
				ProjectPath: s.CWD,
			})
		}
	}
	for id := range t.activities {
		if _, ok := activeIDs[id]; !ok {
			delete(t.activities, id)
			delete(t.waitingSince, id)
			delete(t.agents, id)
			delete(t.projects, id)
		}
	}
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/core/ -race -v`
Expected: all PASS (including pre-existing tracker tests).

- [ ] **Step 5: Commit**

```bash
git add internal/core/activity.go internal/core/activity_test.go
git commit -m "feat(core): publish SessionEvent on activity transitions"
```

---

## Task 3: SessionManager wires bus into tracker

**Files:**
- Modify: `internal/core/session.go`

- [ ] **Step 1: Write failing test**

Append to `internal/core/session_test.go` (create if missing):

```go
package core

import (
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/model"
)

type stubProvider struct{ sessions []*model.Session }

func (p stubProvider) DiscoverSessions() ([]*model.Session, error) { return p.sessions, nil }
func (p stubProvider) UseWatcher() bool                            { return false }
func (p stubProvider) RefreshInterval() time.Duration              { return 0 }
func (p stubProvider) WatchDirs() []string                         { return nil }

func TestSessionManager_SetEventBus_PropagatesToTracker(t *testing.T) {
	bus := NewEventBus()
	ch := bus.Subscribe(4)
	defer bus.Unsubscribe(ch)

	now := time.Now()
	p := stubProvider{sessions: []*model.Session{{SessionID: "s1", Agent: "claude", LastActivity: now, Status: model.StatusThinking}}}
	m := NewSessionManager(60, p)
	m.SetEventBus(bus)

	if err := m.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	select {
	case ev := <-ch:
		if ev.SessionID != "s1" || ev.To != ActivityThinking {
			t.Fatalf("unexpected event: %+v", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("no event after Reload")
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/core/ -run TestSessionManager_SetEventBus -v`
Expected: compile error — `SetEventBus` undefined on `*SessionManager`.

- [ ] **Step 3: Add SetEventBus to SessionManager**

In `internal/core/session.go`, add the method (near `SetExcludeCWDSubstrings`):

```go
// SetEventBus attaches an event bus so activity transitions are published
// to subscribers. Pass nil to detach.
func (m *SessionManager) SetEventBus(bus *EventBus) {
	m.mu.Lock()
	m.tracker.SetEventBus(bus)
	m.mu.Unlock()
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/core/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/session.go internal/core/session_test.go
git commit -m "feat(core): SessionManager.SetEventBus propagates bus to tracker"
```

---

## Task 4: WebhookConfig type and validation

**Files:**
- Modify: `internal/core/config.go`
- Modify: `internal/core/config_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/core/config_test.go`:

```go
func TestWebhookConfig_ValidateOK(t *testing.T) {
	tr := true
	w := WebhookConfig{
		Name:    "slack",
		URL:     "https://example.com/hook",
		Events:  []string{"waiting"},
		Agents:  []string{"claude"},
		Enabled: &tr,
	}
	if err := w.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if !w.IsEnabled() {
		t.Fatal("IsEnabled should be true")
	}
}

func TestWebhookConfig_Validate_RejectsMissingFields(t *testing.T) {
	cases := []struct {
		name string
		w    WebhookConfig
	}{
		{"no name", WebhookConfig{URL: "https://x"}},
		{"no url", WebhookConfig{Name: "x"}},
		{"bad scheme", WebhookConfig{Name: "x", URL: "ftp://x"}},
		{"unparseable", WebhookConfig{Name: "x", URL: "::"}},
		{"unknown event", WebhookConfig{Name: "x", URL: "https://x", Events: []string{"nope"}}},
		{"unknown agent", WebhookConfig{Name: "x", URL: "https://x", Agents: []string{"nope"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.w.Validate(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestWebhookConfig_IsEnabled_DefaultTrue(t *testing.T) {
	w := WebhookConfig{Name: "x", URL: "https://x"}
	if !w.IsEnabled() {
		t.Fatal("absent Enabled should default to true")
	}
}

func TestConfig_ValidWebhooks_SkipsInvalid(t *testing.T) {
	cfg := Config{Webhooks: []WebhookConfig{
		{Name: "ok", URL: "https://x"},
		{Name: "bad", URL: "ftp://x"},
	}}
	got := cfg.ValidWebhooks()
	if len(got) != 1 || got[0].Name != "ok" {
		t.Fatalf("got %+v, want only 'ok'", got)
	}
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/core/ -run "TestWebhookConfig|TestConfig_ValidWebhooks" -v`
Expected: compile error — types/methods undefined.

- [ ] **Step 3: Add WebhookConfig and validation**

Add to `internal/core/config.go`:

```go
// WebhookConfig is a single outbound webhook destination.
type WebhookConfig struct {
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret,omitempty"`
	Events  []string `json:"events,omitempty"`
	Agents  []string `json:"agents,omitempty"`
	Enabled *bool    `json:"enabled,omitempty"` // absent = true
}

// IsEnabled returns true unless Enabled is explicitly set to false.
func (w WebhookConfig) IsEnabled() bool {
	return w.Enabled == nil || *w.Enabled
}

// knownActivityNames lists the canonical activity names accepted in config.
var knownActivityNames = map[string]ActivityKind{
	"idle":       ActivityIdle,
	"waiting":    ActivityWaiting,
	"thinking":   ActivityThinking,
	"compacting": ActivityCompacting,
	"reading":    ActivityReading,
	"writing":    ActivityWriting,
	"running":    ActivityRunning,
	"searching":  ActivitySearching,
	"browsing":   ActivityBrowsing,
	"spawning":   ActivitySpawning,
}

// knownAgentNames lists the agent names accepted in config.
var knownAgentNames = map[string]struct{}{
	"claude": {}, "codex": {}, "pi": {}, "cursor": {}, "amp": {}, "opencode": {},
}

// Validate returns nil if the webhook is well-formed.
func (w WebhookConfig) Validate() error {
	if strings.TrimSpace(w.Name) == "" {
		return fmt.Errorf("webhook: name is required")
	}
	if strings.TrimSpace(w.URL) == "" {
		return fmt.Errorf("webhook %q: url is required", w.Name)
	}
	u, err := url.Parse(w.URL)
	if err != nil {
		return fmt.Errorf("webhook %q: url parse: %w", w.Name, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webhook %q: url scheme must be http or https, got %q", w.Name, u.Scheme)
	}
	for _, ev := range w.Events {
		if _, ok := knownActivityNames[strings.ToLower(ev)]; !ok {
			return fmt.Errorf("webhook %q: unknown event %q", w.Name, ev)
		}
	}
	for _, ag := range w.Agents {
		if _, ok := knownAgentNames[strings.ToLower(ag)]; !ok {
			return fmt.Errorf("webhook %q: unknown agent %q", w.Name, ag)
		}
	}
	return nil
}
```

Also add a `Webhooks []WebhookConfig` field to the `Config` struct (alphabetically placed), and the convenience helper:

```go
// ValidWebhooks returns the subset of webhooks that pass Validate.
// Invalid webhooks are logged once at load time; this method just filters.
func (c Config) ValidWebhooks() []WebhookConfig {
	out := make([]WebhookConfig, 0, len(c.Webhooks))
	for _, w := range c.Webhooks {
		if err := w.Validate(); err == nil && w.IsEnabled() {
			out = append(out, w)
		}
	}
	return out
}
```

Add the import `"net/url"` to `internal/core/config.go`.

The `LoadConfig` function should log each invalid webhook once at load time. Find the section that returns `cfg` after parsing and add (immediately before the return that follows successful JSON unmarshal):

```go
for _, w := range cfg.Webhooks {
	if err := w.Validate(); err != nil {
		log.Printf("config: %v (skipped)", err)
	}
}
```

Add `"log"` to the imports if not present.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/core/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/core/config.go internal/core/config_test.go
git commit -m "feat(core): add WebhookConfig type with validation"
```

---

## Task 5: webhook.Payload

**Files:**
- Create: `internal/webhook/payload.go`
- Create: `internal/webhook/payload_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/webhook/payload_test.go
package webhook

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
)

func TestPayload_MarshalContainsExpectedFields(t *testing.T) {
	p := Payload{
		ID:          "f47ac10b-58cc-4372-a567-0e02b2c3d479",
		Event:       "state_transition",
		SessionID:   "abc",
		Agent:       "claude",
		From:        string(core.ActivityIdle),
		To:          string(core.ActivityWaiting),
		ProjectPath: "/p",
		Timestamp:   time.Date(2026, 5, 19, 14, 30, 0, 0, time.UTC),
		API: &APILinks{
			SessionURL: "http://127.0.0.1:7421/api/sessions/abc",
			DetailURL:  "http://127.0.0.1:7421/api/sessions/abc/full",
		},
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{
		`"id":"f47ac10b`, `"event":"state_transition"`, `"session_id":"abc"`,
		`"agent":"claude"`, `"from":"idle"`, `"to":"waiting"`,
		`"project_path":"/p"`, `"timestamp":"2026-05-19T14:30:00Z"`,
		`"api":{`, `"session_url":"http://127.0.0.1:7421/api/sessions/abc"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in %s", want, s)
		}
	}
}

func TestPayload_MarshalOmitsAPIWhenNil(t *testing.T) {
	p := Payload{ID: "x", Event: "state_transition", SessionID: "s"}
	b, _ := json.Marshal(p)
	if strings.Contains(string(b), `"api"`) {
		t.Fatalf("api field should be omitted: %s", b)
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/webhook/ -v`
Expected: compile error.

- [ ] **Step 3: Implement Payload**

```go
// internal/webhook/payload.go
package webhook

import "time"

// Payload is the JSON body sent on every webhook delivery.
type Payload struct {
	ID          string    `json:"id"`
	Event       string    `json:"event"`
	SessionID   string    `json:"session_id"`
	Agent       string    `json:"agent"`
	From        string    `json:"from"`
	To          string    `json:"to"`
	ProjectPath string    `json:"project_path"`
	Timestamp   time.Time `json:"timestamp"`
	API         *APILinks `json:"api,omitempty"`
}

// APILinks point back to the local lazyagent API server for full details.
// Present only when the API server is running.
type APILinks struct {
	SessionURL string `json:"session_url"`
	DetailURL  string `json:"detail_url"`
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/webhook/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/payload.go internal/webhook/payload_test.go
git commit -m "feat(webhook): payload schema with optional API links"
```

---

## Task 6: webhook.Filter

**Files:**
- Create: `internal/webhook/filter.go`
- Create: `internal/webhook/filter_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/webhook/filter_test.go
package webhook

import (
	"testing"

	"github.com/illegalstudio/lazyagent/internal/core"
)

func TestMatches(t *testing.T) {
	ev := core.SessionEvent{Agent: "claude", To: core.ActivityWaiting}

	cases := []struct {
		name   string
		w      core.WebhookConfig
		matches bool
	}{
		{"empty filters match all", core.WebhookConfig{Name: "x", URL: "https://x"}, true},
		{"matching event", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"waiting"}}, true},
		{"non-matching event", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"thinking"}}, false},
		{"matching agent", core.WebhookConfig{Name: "x", URL: "https://x", Agents: []string{"claude"}}, true},
		{"non-matching agent", core.WebhookConfig{Name: "x", URL: "https://x", Agents: []string{"codex"}}, false},
		{"event AND agent both match", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"waiting"}, Agents: []string{"claude"}}, true},
		{"event matches, agent doesn't", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"waiting"}, Agents: []string{"codex"}}, false},
		{"case-insensitive event", core.WebhookConfig{Name: "x", URL: "https://x", Events: []string{"WAITING"}}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Matches(c.w, ev); got != c.matches {
				t.Fatalf("got %v, want %v", got, c.matches)
			}
		})
	}
}
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/webhook/ -run TestMatches -v`
Expected: compile error.

- [ ] **Step 3: Implement Matches**

```go
// internal/webhook/filter.go
package webhook

import (
	"strings"

	"github.com/illegalstudio/lazyagent/internal/core"
)

// Matches returns true when the event passes the webhook's event/agent filters.
// Empty filter slices match everything.
func Matches(w core.WebhookConfig, ev core.SessionEvent) bool {
	if len(w.Events) > 0 {
		want := strings.ToLower(string(ev.To))
		ok := false
		for _, e := range w.Events {
			if strings.ToLower(e) == want {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	if len(w.Agents) > 0 {
		want := strings.ToLower(ev.Agent)
		ok := false
		for _, a := range w.Agents {
			if strings.ToLower(a) == want {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/webhook/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/filter.go internal/webhook/filter_test.go
git commit -m "feat(webhook): event + agent filter matching"
```

---

## Task 7: webhook.Sign (HMAC-SHA256)

**Files:**
- Create: `internal/webhook/hmac.go`
- Create: `internal/webhook/hmac_test.go`

- [ ] **Step 1: Write failing test with known vector**

```go
// internal/webhook/hmac_test.go
package webhook

import "testing"

func TestSign_KnownVector(t *testing.T) {
	// HMAC-SHA256("it's a secret", `{"foo":"bar"}`) hex digest.
	// Verified independently: echo -n '{"foo":"bar"}' | openssl dgst -sha256 -hmac "it's a secret"
	const want = "sha256=5d1eaa4e0d72b46cef0ecbf3a8ab06d7c3e0c89c0c4d4f10907ba87baa11d97a"
	got := Sign("it's a secret", []byte(`{"foo":"bar"}`))
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestSign_EmptySecret(t *testing.T) {
	if Sign("", []byte("x")) == "" {
		t.Fatal("Sign with empty secret should still return a valid signature string")
	}
}
```

> Note for implementer: if the test vector above doesn't match, regenerate it
> with `echo -n '{"foo":"bar"}' | openssl dgst -sha256 -hmac "it's a secret"`
> and update the constant. The point of the test is that the format is fixed.

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/webhook/ -run TestSign -v`
Expected: compile error — `Sign` undefined.

- [ ] **Step 3: Implement Sign**

```go
// internal/webhook/hmac.go
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the HMAC-SHA256 of body keyed by secret, formatted as
// "sha256=<hex>" — the same convention used by GitHub webhooks.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/webhook/ -v`
Expected: PASS. If `TestSign_KnownVector` fails because the test vector is wrong, regenerate as noted and re-run.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/hmac.go internal/webhook/hmac_test.go
git commit -m "feat(webhook): HMAC-SHA256 signing of payloads"
```

---

## Task 8: Dispatcher happy path (POST 2xx)

**Files:**
- Create: `internal/webhook/dispatcher.go`
- Create: `internal/webhook/dispatcher_test.go`

- [ ] **Step 1: Write failing test**

```go
// internal/webhook/dispatcher_test.go
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
	// Give Start a moment to subscribe to the bus.
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
```

- [ ] **Step 2: Run test, verify it fails**

Run: `go test ./internal/webhook/ -run TestDispatcher_HappyPath -v`
Expected: compile error — `New`, `Dispatcher.Start` undefined.

- [ ] **Step 3: Implement Dispatcher skeleton + happy-path delivery**

```go
// internal/webhook/dispatcher.go
package webhook

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/illegalstudio/lazyagent/internal/core"
	"github.com/illegalstudio/lazyagent/internal/version"
)

// ConfigSource provides the current set of webhook configurations.
// Implementations may return a different slice on each call (e.g. after
// config reload); the dispatcher reads it once per incoming event.
type ConfigSource interface {
	Webhooks() []core.WebhookConfig
}

// Dispatcher consumes SessionEvents from a bus and delivers them as HTTP
// POSTs to configured webhooks.
type Dispatcher struct {
	bus     *core.EventBus
	cfg     ConfigSource
	client  *http.Client
	apiAddr func() string

	queueSize int
	workers   int
	backoffs  []time.Duration
}

// deliveryJob is one POST attempt against a specific webhook.
type deliveryJob struct {
	webhook    core.WebhookConfig
	body       []byte
	deliveryID string
}

// New creates a Dispatcher. The HTTP client should have a sensible timeout
// set (e.g. 10s). apiAddr returns the API server base URL (e.g.
// "http://127.0.0.1:7421") or "" if no API server is running.
func New(bus *core.EventBus, cfg ConfigSource, client *http.Client, apiAddr func() string) *Dispatcher {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	if apiAddr == nil {
		apiAddr = func() string { return "" }
	}
	return &Dispatcher{
		bus:       bus,
		cfg:       cfg,
		client:    client,
		apiAddr:   apiAddr,
		queueSize: 256,
		workers:   4,
		backoffs:  []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second},
	}
}

// Start subscribes to the bus and runs the fan-out + worker goroutines until
// ctx is cancelled. Returns the context error on exit.
func (d *Dispatcher) Start(ctx context.Context) error {
	events := d.bus.Subscribe(256)
	defer d.bus.Unsubscribe(events)

	queue := make(chan deliveryJob, d.queueSize)

	// Worker pool
	workerDone := make(chan struct{}, d.workers)
	for i := 0; i < d.workers; i++ {
		go func() {
			defer func() { workerDone <- struct{}{} }()
			for {
				select {
				case <-ctx.Done():
					return
				case job := <-queue:
					d.deliver(ctx, job)
				}
			}
		}()
	}

	// Fan-out loop
	for {
		select {
		case <-ctx.Done():
			close(queue)
			for i := 0; i < d.workers; i++ {
				<-workerDone
			}
			return ctx.Err()
		case ev := <-events:
			d.fanout(ev, queue)
		}
	}
}

// fanout marshals the payload once, walks the configured webhooks, and
// enqueues one deliveryJob per match. Drops jobs when the queue is full.
func (d *Dispatcher) fanout(ev core.SessionEvent, queue chan<- deliveryJob) {
	webhooks := d.cfg.Webhooks()
	if len(webhooks) == 0 {
		return
	}
	deliveryID := newDeliveryID()
	payload := Payload{
		ID:          deliveryID,
		Event:       "state_transition",
		SessionID:   ev.SessionID,
		Agent:       ev.Agent,
		From:        string(ev.From),
		To:          string(ev.To),
		ProjectPath: ev.ProjectPath,
		Timestamp:   ev.At.UTC(),
	}
	if base := d.apiAddr(); base != "" {
		payload.API = &APILinks{
			SessionURL: fmt.Sprintf("%s/api/sessions/%s", base, ev.SessionID),
			DetailURL:  fmt.Sprintf("%s/api/sessions/%s/full", base, ev.SessionID),
		}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		log.Printf("webhook: marshal payload: %v", err)
		return
	}
	for _, w := range webhooks {
		if !w.IsEnabled() || !Matches(w, ev) {
			continue
		}
		select {
		case queue <- deliveryJob{webhook: w, body: body, deliveryID: deliveryID}:
		default:
			log.Printf("webhook: queue full, dropping delivery for %q", w.Name)
		}
	}
}

// deliver performs the POST with no retry (retry added in Task 9).
func (d *Dispatcher) deliver(ctx context.Context, job deliveryJob) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.webhook.URL, bytes.NewReader(job.body))
	if err != nil {
		log.Printf("webhook %q: build request: %v", job.webhook.Name, err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "lazyagent/"+version.String())
	req.Header.Set("X-Lazyagent-Event", "state_transition")
	req.Header.Set("X-Lazyagent-Delivery", job.deliveryID)
	if job.webhook.Secret != "" {
		req.Header.Set("X-Lazyagent-Signature", Sign(job.webhook.Secret, job.body))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		log.Printf("webhook %q: POST: %v", job.webhook.Name, err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	log.Printf("webhook %q: %d %s — %s", job.webhook.Name, resp.StatusCode, resp.Status, string(snippet))
}

func newDeliveryID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	// Format as RFC 4122-ish UUIDv4 string.
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/webhook/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/dispatcher.go internal/webhook/dispatcher_test.go
git commit -m "feat(webhook): dispatcher with fan-out and HTTP POST delivery"
```

---

## Task 9: Dispatcher retry on 5xx, no retry on 4xx

**Files:**
- Modify: `internal/webhook/dispatcher.go`
- Modify: `internal/webhook/dispatcher_test.go`

- [ ] **Step 1: Write failing tests**

Append to `internal/webhook/dispatcher_test.go`:

```go
import "sync/atomic"

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
	time.Sleep(200 * time.Millisecond) // wait long enough for any retries to fire

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
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/webhook/ -run "TestDispatcher_Retry|TestDispatcher_NoRetry|TestDispatcher_AllAttempts" -v`
Expected: FAIL — only one attempt is made.

- [ ] **Step 3: Add retry loop in deliver**

Replace the body of `deliver` in `internal/webhook/dispatcher.go`:

```go
// deliver performs the POST, retrying on transient failures.
// 4xx is treated as permanent. 5xx, network errors, and timeouts retry
// with backoff up to len(d.backoffs) times (total attempts = 1 + retries).
func (d *Dispatcher) deliver(ctx context.Context, job deliveryJob) {
	for attempt := 0; attempt <= len(d.backoffs); attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d.backoffs[attempt-1]):
			}
		}
		status, transient, err := d.doOnce(ctx, job)
		if err == nil && !transient {
			return
		}
		if err == nil && status >= 400 && status < 500 {
			return // permanent
		}
		if attempt == len(d.backoffs) {
			log.Printf("webhook %q: giving up after %d attempts", job.webhook.Name, attempt+1)
		}
	}
}

// doOnce performs a single POST. Returns (status, transient, err).
//   - 2xx: status=2xx, transient=false, err=nil → success
//   - 4xx: status=4xx, transient=false, err=nil → permanent
//   - 5xx: status=5xx, transient=true, err=nil → retry
//   - network/timeout: status=0, transient=true, err=non-nil → retry
func (d *Dispatcher) doOnce(ctx context.Context, job deliveryJob) (int, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, job.webhook.URL, bytes.NewReader(job.body))
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "lazyagent/"+version.String())
	req.Header.Set("X-Lazyagent-Event", "state_transition")
	req.Header.Set("X-Lazyagent-Delivery", job.deliveryID)
	if job.webhook.Secret != "" {
		req.Header.Set("X-Lazyagent-Signature", Sign(job.webhook.Secret, job.body))
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, false, nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	log.Printf("webhook %q: %d %s — %s", job.webhook.Name, resp.StatusCode, resp.Status, string(snippet))
	if resp.StatusCode >= 500 {
		return resp.StatusCode, true, nil
	}
	return resp.StatusCode, false, nil
}
```

- [ ] **Step 4: Run all webhook tests, verify they pass**

Run: `go test ./internal/webhook/ -race -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/dispatcher.go internal/webhook/dispatcher_test.go
git commit -m "feat(webhook): retry transient failures, no retry on 4xx"
```

---

## Task 10: Dispatcher HMAC signature header (integration test)

**Files:**
- Modify: `internal/webhook/dispatcher_test.go`

The Sign function is already used in `doOnce` when a secret is configured. This task adds an integration test that verifies the header is set correctly end-to-end (covers wire format, not just the Sign function).

- [ ] **Step 1: Write failing test**

Append to `internal/webhook/dispatcher_test.go`:

```go
func TestDispatcher_HMACHeaderWhenSecretSet(t *testing.T) {
	var sigHeader string
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigHeader = r.Header.Get("X-Lazyagent-Signature")
		body, _ = io.ReadAll(r.Body)
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

	if sigHeader == "" {
		t.Fatal("X-Lazyagent-Signature missing")
	}
	if want := Sign("hello", body); sigHeader != want {
		t.Fatalf("got %q, want %q", sigHeader, want)
	}
}

func TestDispatcher_NoHMACWhenSecretEmpty(t *testing.T) {
	var sigHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sigHeader = r.Header.Get("X-Lazyagent-Signature")
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

	if sigHeader != "" {
		t.Fatalf("X-Lazyagent-Signature should be absent, got %q", sigHeader)
	}
}
```

- [ ] **Step 2: Run tests, verify they pass**

Run: `go test ./internal/webhook/ -run TestDispatcher_HMAC -race -v`
Expected: PASS (HMAC logic is already implemented in Task 8 + 9).

- [ ] **Step 3: Commit**

```bash
git add internal/webhook/dispatcher_test.go
git commit -m "test(webhook): cover HMAC header wire format end-to-end"
```

---

## Task 11: Dispatcher dedupe duplicate transitions

When TUI, GUI, and API run in the same process, each builds its own `SessionManager` and `ActivityTracker`. All three publish the same `(session, from, to)` transition. This task adds a small last-seen map to the dispatcher to coalesce duplicates emitted within a short window.

**Files:**
- Modify: `internal/webhook/dispatcher.go`
- Modify: `internal/webhook/dispatcher_test.go`

- [ ] **Step 1: Write failing test**

Append:

```go
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
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `go test ./internal/webhook/ -run TestDispatcher_Dedup -race -v`
Expected: FAIL — duplicates produce 3 POSTs.

- [ ] **Step 3: Add dedup in fanout**

In `dispatcher.go`, add fields to the struct and a helper:

```go
type Dispatcher struct {
	bus     *core.EventBus
	cfg     ConfigSource
	client  *http.Client
	apiAddr func() string

	queueSize int
	workers   int
	backoffs  []time.Duration

	dedupWindow time.Duration

	mu          sync.Mutex
	lastSeen    map[string]lastSeenEntry // key: session_id
}

type lastSeenEntry struct {
	from core.ActivityKind
	to   core.ActivityKind
	at   time.Time
}
```

In `New`:

```go
return &Dispatcher{
	bus:         bus,
	cfg:         cfg,
	client:      client,
	apiAddr:     apiAddr,
	queueSize:   256,
	workers:     4,
	backoffs:    []time.Duration{1 * time.Second, 5 * time.Second, 30 * time.Second},
	dedupWindow: 2 * time.Second,
	lastSeen:    make(map[string]lastSeenEntry),
}
```

At the top of `fanout`, add a dedup check:

```go
func (d *Dispatcher) fanout(ev core.SessionEvent, queue chan<- deliveryJob) {
	if d.shouldDedup(ev) {
		return
	}
	// ... existing logic
}

func (d *Dispatcher) shouldDedup(ev core.SessionEvent) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	prev, ok := d.lastSeen[ev.SessionID]
	now := time.Now()
	if ok && prev.from == ev.From && prev.to == ev.To && now.Sub(prev.at) < d.dedupWindow {
		return true
	}
	d.lastSeen[ev.SessionID] = lastSeenEntry{from: ev.From, to: ev.To, at: now}
	return false
}
```

Add `"sync"` to the import list of `dispatcher.go`.

- [ ] **Step 4: Run tests, verify they pass**

Run: `go test ./internal/webhook/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/webhook/dispatcher.go internal/webhook/dispatcher_test.go
git commit -m "feat(webhook): dedup duplicate transitions across managers"
```

---

## Task 12: Dispatcher graceful shutdown drains workers

**Files:**
- Modify: `internal/webhook/dispatcher_test.go`

The fan-out + worker loop already respects `ctx.Done()`. This task adds a test that exercises the shutdown path under load to guard against future regressions.

- [ ] **Step 1: Write test**

Append:

```go
func TestDispatcher_ContextCancelStopsCleanly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	bus := core.NewEventBus()
	cfg := stubCfg{webhooks: []core.WebhookConfig{{Name: "test", URL: srv.URL}}}
	d := New(bus, cfg, &http.Client{Timeout: time.Second}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = d.Start(ctx)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)

	for i := 0; i < 50; i++ {
		bus.Publish(core.SessionEvent{SessionID: fmt.Sprintf("s%d", i), To: core.ActivityWaiting, At: time.Now()})
	}
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not stop after context cancel")
	}
}
```

Add `"fmt"` to the test file imports if missing.

- [ ] **Step 2: Run, verify pass**

Run: `go test ./internal/webhook/ -run TestDispatcher_ContextCancel -race -v`
Expected: PASS.

- [ ] **Step 3: Commit**

```bash
git add internal/webhook/dispatcher_test.go
git commit -m "test(webhook): cover graceful shutdown under load"
```

---

## Task 13: Wire bus into TUI manager

**Files:**
- Modify: `internal/ui/app.go`

- [ ] **Step 1: Add bus parameter to NewModel**

In `internal/ui/app.go:115`, change:

```go
func NewModel(provider core.SessionProvider) Model {
```

to:

```go
func NewModel(provider core.SessionProvider, bus *core.EventBus) Model {
```

After `mgr := core.NewSessionManager(cfg.WindowMinutes, provider)` (line 118), insert:

```go
if bus != nil {
    mgr.SetEventBus(bus)
}
```

Update callers: `main.go:210` (`ui.NewModel(provider)` → `ui.NewModel(provider, nil)` — `main.go` will pass the real bus in Task 16) and `internal/ui/app_test.go:31` (already uses a local construction pattern; pass `nil`).

- [ ] **Step 2: Build, verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run all tests**

Run: `go test ./... -race`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/ui/app.go
git commit -m "feat(ui): accept optional EventBus for transition publishing"
```

---

## Task 14: Wire bus into API manager

**Files:**
- Modify: `internal/api/server.go`

- [ ] **Step 1: Add bus param to api.New**

In `internal/api/server.go`, change the signature of `New` to accept a `*core.EventBus` (place after the existing params). Inside `New`, after `manager.SetExcludeCWDSubstrings(...)`:

```go
if bus != nil {
    manager.SetEventBus(bus)
}
```

Update all callers (`main.go`, plus any tests in `internal/api/`). Pass `nil` in tests to preserve current behavior.

- [ ] **Step 2: Build, verify it compiles**

Run: `go build ./...`
Expected: success.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/ -race -v`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/api/server.go
git commit -m "feat(api): accept optional EventBus for transition publishing"
```

---

## Task 15: Wire bus into GUI tray service

**Files:**
- Modify: `internal/tray/service.go`

- [ ] **Step 1: Inspect existing tray service.go signature**

Read the file to find where `core.NewSessionManager` is called (around line 50). The tray runs in a separate detached process and uses build tag `!notray`. The bus passed by the parent process is not accessible — tray needs its own bus inside its process if it wants to emit webhooks.

For MVP simplicity: the tray process will construct its own bus and dispatcher when its config has webhooks. This is the same code as the main-process path but happens in the tray process.

Add a `bus *core.EventBus` field to the tray's manager wiring. Inside the tray's startup (likely a `Run` function), after constructing the manager:

```go
cfg := core.LoadConfig()
if len(cfg.ValidWebhooks()) > 0 {
    bus := core.NewEventBus()
    s.manager.SetEventBus(bus)
    httpClient := &http.Client{Timeout: 10 * time.Second}
    d := webhook.New(bus, &cfgSource{cfg: cfg}, httpClient, func() string { return "" })
    go func() { _ = d.Start(ctx) }()
}
```

where `cfgSource` is a small wrapper implementing `webhook.ConfigSource`:

```go
type cfgSource struct{ cfg core.Config }
func (c *cfgSource) Webhooks() []core.WebhookConfig { return c.cfg.ValidWebhooks() }
```

Place `cfgSource` in `internal/webhook/configsource.go` so both `main.go` and `internal/tray/service.go` can use it. (Alternative: keep it private to each caller; pick the simpler option in the implementation.)

The tray must guard the import of `internal/webhook` so the `notray` build still compiles — but since this code only exists in `service.go` which is already `//go:build !notray`-only, it is naturally guarded.

- [ ] **Step 2: Build with and without notray**

Run: `go build ./...` and `go build -tags notray ./...`
Expected: both succeed.

- [ ] **Step 3: Commit**

```bash
git add internal/tray/service.go internal/webhook/configsource.go
git commit -m "feat(tray): start webhook dispatcher when configured"
```

---

## Task 16: Main process wiring

**Files:**
- Modify: `main.go`

- [ ] **Step 1: Construct bus and start dispatcher in main**

In `main.go`, after `cfg := core.LoadConfig()` and after the provider is built, add:

```go
// EventBus + webhook dispatcher (started when at least one valid webhook is configured).
var eventBus *core.EventBus
var dispatcherStop context.CancelFunc
if len(cfg.ValidWebhooks()) > 0 {
    eventBus = core.NewEventBus()
    dispatcherCtx, cancel := context.WithCancel(context.Background())
    dispatcherStop = cancel
    httpClient := &http.Client{Timeout: 10 * time.Second}
    apiAddr := func() string {
        // Returns "http://<host>" or "" if not running. Populated below
        // once the API server is started, via an atomic.Value or a small
        // helper. For MVP, capture the resolved bind address after srv.Run
        // begins, or pass a static empty string until the API server
        // exposes an Addr() method.
        return ""
    }
    d := webhook.New(eventBus, &cfgSource{cfg: cfg}, httpClient, apiAddr)
    go func() { _ = d.Start(dispatcherCtx) }()
}
```

Where `cfgSource` is the type added in Task 15. Add the imports `"net/http"`, `"github.com/illegalstudio/lazyagent/internal/webhook"`, and `"context"` if missing.

Then pass `eventBus` into:

- The TUI entry point (where it constructs its manager).
- `api.New(...)` (added in Task 14).
- The tray fork (tray runs in its own process and constructs its own bus — no change here).

On shutdown, before `defer cancel()` returns, call `dispatcherStop()` if non-nil.

A clean refactor: extract a small helper `setupWebhooks(cfg)` that returns `(*core.EventBus, func(), apiAddrSetter)`. Decision left to implementation taste.

For the `apiAddr` capture: when the API server is constructed, store its bind address in an `atomic.Value` (or similar) and have `apiAddr` read from it. If the API is never started, the value stays empty and the `api` object is omitted from payloads — which is the desired behavior.

- [ ] **Step 2: Build and run smoke test**

```bash
make tui
./build/lazyagent --help
```

Expected: builds cleanly, help output unchanged.

- [ ] **Step 3: Configure a webhook and verify end-to-end with a local test server**

```bash
# In one terminal:
go run ./testdata/webhook-sink   # if no such tool exists, use python -m http.server or netcat
# Or:
while true; do echo -e "HTTP/1.1 200 OK\n\n" | nc -l 9999; done
```

Edit `~/.config/lazyagent/config.json`:

```json
{
  "webhooks": [
    {"name": "local", "url": "http://127.0.0.1:9999"}
  ]
}
```

Run `lazyagent --tui` against real Claude sessions (or `--demo`), trigger a state change in any session (e.g. complete a task or send a message in Claude). Verify a POST hits the local server with the expected body.

If using `--demo`, the demo provider emits synthetic data — verify the dispatcher fires on the synthetic state changes.

- [ ] **Step 4: Commit**

```bash
git add main.go
git commit -m "feat: start webhook dispatcher in main when webhooks configured"
```

---

## Task 17: User-facing documentation

**Files:**
- Create: `docs/reference/webhooks.md`
- Modify: `docs/reference/configuration.md`
- Modify: `docs/reference/roadmap.md`
- Modify: `README.md`

- [ ] **Step 1: Write the dedicated webhooks page**

Create `docs/reference/webhooks.md`. Follow the Astro Starlight frontmatter style used by the rest of `docs/reference/` (see `docs/reference/configuration.md` for the exact format).

Cover:
- Why webhooks (use cases: Slack notifications, dashboards, CI triggers)
- Configuration example (full JSON, copy-pasteable)
- Payload schema (table of fields, sample body)
- Headers table
- HMAC verification with a 10-line Python snippet:

  ```python
  import hmac, hashlib
  secret = b"abc123sharedwithslack"
  body = request.get_data()
  sig = "sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest()
  if not hmac.compare_digest(sig, request.headers["X-Lazyagent-Signature"]):
      abort(401)
  ```

- Delivery semantics (async best-effort, retry, drops, dedup window)
- Troubleshooting:
  - "I see no POSTs" → check `webhooks: []` length, check `lazyagent` logs
  - "I see duplicate POSTs" → mention the 2 s dedup window
  - "4xx in logs" → consumer is rejecting; not retried by design

- [ ] **Step 2: Update configuration.md**

In `docs/reference/configuration.md`, add a section documenting the new `webhooks` field. Reference `webhooks.md` for full details.

- [ ] **Step 3: Update roadmap**

In `docs/reference/roadmap.md`:
- Remove "Outbound webhooks on status changes" from "Future ideas".
- Add a new `v0.10` section listing the shipped capabilities (event bus, dispatcher, HMAC, dedup).

- [ ] **Step 4: Update README**

In `README.md`, under the features list (find the existing one-liner style and match), add:

```
- Outbound webhooks on session state transitions (Slack, dashboards, CI)
```

- [ ] **Step 5: Commit**

```bash
git add docs/reference/webhooks.md docs/reference/configuration.md docs/reference/roadmap.md README.md
git commit -m "docs: document outbound webhooks"
```

---

## Final Verification

- [ ] Run the full test suite with race detector:
  ```bash
  go test ./... -race
  ```
  Expected: all PASS.

- [ ] Build all targets:
  ```bash
  go build ./...
  go build -tags notray ./...
  ```
  Expected: both succeed.

- [ ] Run `gofmt -l .` and `go vet ./...` — both must report no issues.

- [ ] Manual end-to-end test with a real webhook receiver (or `httpbin.org/anything` for a low-stakes check) as described in Task 16, Step 3.

- [ ] Open a PR against `illegalstudio/lazyagent:main` from a feature branch `feature/outbound-webhooks`. Reference the spec in the PR body. Include a sample config snippet and the HMAC verification example.

---

## Notes for the Implementer

- **TDD strictness:** Every task uses red→green TDD. If you find a step where you "know" the implementation and the test feels like a formality, write the test anyway — it documents intent and catches future regressions.
- **No retroactive features:** If you find yourself wanting to add structured logging, metrics, persistence, or a CLI subcommand, stop and confirm scope. The spec explicitly excludes them.
- **Two-process reality:** When `--gui` is used, the tray runs as a forked process. Task 15 handles tray webhooks separately. Cross-process webhooks fire independently; the dedup window only covers in-process duplicates.
- **`apiAddr` is best-effort:** If wiring it neatly takes more than 30 minutes, ship Task 16 with `apiAddr: func() string { return "" }` and add the proper hookup as a follow-up commit. The payload's `api` object is optional by design.
