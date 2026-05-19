# Outbound Webhooks for Session State Transitions

## Summary

Add outbound HTTP webhooks that fire when a monitored session changes activity
state (e.g., `Idle → WaitingForUser`). Users configure one or more endpoints in
`~/.config/lazyagent/config.json` with filters on event type and agent. Delivery
is asynchronous, best-effort, with optional HMAC-SHA256 signing.

This is the first lazyagent feature with internal pub-sub. To deliver it we add
a small typed event bus in `internal/core/`, which the existing SSE handler can
later migrate onto. The webhook dispatcher lives in a new `internal/webhook/`
package and depends only on the bus and `*http.Client` — no file I/O.

## Goals

- POST a JSON payload to user-configured URLs on session state transitions.
- Filter per webhook by event type and agent.
- Optional HMAC-SHA256 signing so the consumer can verify the sender.
- Asynchronous delivery with bounded queue, retry on transient failures, and
  drop on overflow. The main session loop is never blocked by a slow consumer.
- Zero behavior change when no webhook is configured.

## Non-goals (MVP)

- Persisting undelivered events across restarts. Lazyagent is read-only on
  disk today and this feature does not change that.
- Webhooks for non-transition events (file changes, cost updates, new
  sessions). These can be added later as new bus event types.
- CLI subcommand for managing webhooks. Users edit `config.json` directly,
  consistent with the rest of the project.
- Per-project / CWD-based filtering. The MVP filter is `events + agents`.
- Replacing the existing SSE "pulse" used by `/api/events`. The new bus is
  additive in this PR; an SSE refactor is a follow-up.

## Data Source

Activity state is computed by `internal/core/activity.go` and stored in
`ActivityTracker`. Today `ActivityTracker.Update(sessions, now)` overwrites
its map with newly computed activities and discards the previous value — no
transition is ever observed.

The webhook system needs `(previous, next)` per session. We change `Update` to
emit an event whenever the previous activity differs from the next.

## Architecture

```
core.SessionManager
   └─ ActivityTracker.Update()
         └─ compare prev vs next
               └─ if changed: bus.Publish(SessionEvent{...})
                       │
                       ▼
                core.EventBus  (new)
                       │
                       ▼  Subscribe(buf=256)
                webhook.Dispatcher  (new)
                       ├─ filter (event + agent)
                       ├─ enqueue deliveryJob
                       ├─ worker pool (4 goroutines)
                       │     └─ HTTP POST with retry + HMAC
                       └─ context-aware shutdown
```

Boundaries:

- `core` does not know about HTTP. It only publishes typed events.
- `webhook` does not know about files, JSONL, or SQLite. It consumes events
  and speaks HTTP.
- Both components are independently testable: bus with a synthetic subscriber,
  dispatcher with a fake bus and `httptest.Server`.

## Components

### 1. `internal/core/eventbus.go` (new)

A minimal typed pub-sub for in-process subscribers.

```go
type SessionEvent struct {
    SessionID   string
    Agent       string         // "claude", "codex", "pi", ...
    From        ActivityKind
    To          ActivityKind
    At          time.Time
    ProjectPath string
}

type EventBus struct {
    mu   sync.RWMutex
    subs []chan SessionEvent
}

func NewEventBus() *EventBus
func (b *EventBus) Subscribe(buf int) <-chan SessionEvent
func (b *EventBus) Unsubscribe(ch <-chan SessionEvent)
func (b *EventBus) Publish(e SessionEvent) // non-blocking, drops on full subscriber
```

Invariants:

- `Publish` never blocks. If a subscriber channel is full, the event is
  dropped for that subscriber and a debug log is emitted (rate-limited).
- `Unsubscribe` is idempotent and safe to call from any goroutine.
- Subscribers receive events in publish order; ordering across subscribers is
  not guaranteed.

### 2. `internal/core/activity.go` change

`ActivityTracker` gains an optional `*EventBus` reference:

```go
type ActivityTracker struct {
    current map[string]ActivityKind
    bus     *EventBus            // optional, nil-safe
}

func (t *ActivityTracker) SetEventBus(bus *EventBus)
```

`Update(sessions, now)` is changed so that, for each session, after computing
the new activity, it compares against the previous value in `current`. If they
differ and `bus != nil`, it calls `bus.Publish` with a `SessionEvent`.

Transition cases:

- Session not seen before: emit `From: ActivityUnknown, To: <computed>`.
- Session seen before, activity changed: emit `From: prev, To: next`.
- Session seen before, activity unchanged: no event.
- Session disappears from the input slice: no event in MVP.

`SessionManager` wires the bus at construction time:

```go
manager := NewSessionManager(...)
manager.SetEventBus(bus)
```

`SetEventBus` propagates the reference into the tracker.

### 3. `internal/core/config.go` change

Add a `Webhooks []WebhookConfig` field to `Config`, plus the type:

```go
type WebhookConfig struct {
    Name    string   `json:"name"`              // required, used in logs
    URL     string   `json:"url"`               // required, https/http
    Secret  string   `json:"secret,omitempty"`  // optional HMAC-SHA256 key
    Events  []string `json:"events,omitempty"`  // empty = all activity kinds
    Agents  []string `json:"agents,omitempty"`  // empty = all agents
    Enabled *bool    `json:"enabled,omitempty"` // default true; pointer so absence = default
}
```

Validation during load:

- `name` non-empty and unique within the slice.
- `url` parses with `net/url.Parse` and scheme is `http` or `https`.
- Each `events[i]` matches a known `ActivityKind` (case-insensitive, e.g.
  `waiting_for_user`, `idle`, `thinking`, `executing_tool`, `processing_result`).
- Each `agents[i]` matches a known agent name (`claude`, `codex`, `pi`,
  `cursor`, `amp`, `opencode`).
- Invalid webhooks are skipped with a warning at load time. They do not
  prevent other webhooks (or the rest of the config) from loading. This
  matches the existing "silent error handling" pattern in the providers.

### 4. `internal/webhook/` (new package)

```
internal/webhook/
  dispatcher.go      // Dispatcher type, Start/Stop, fan-out + workers
  payload.go         // Payload struct, marshal helper
  filter.go          // matches(WebhookConfig, SessionEvent) bool
  hmac.go            // sign(secret, body []byte) string
  dispatcher_test.go
  filter_test.go
  hmac_test.go
  payload_test.go
```

Dispatcher shape:

```go
type Dispatcher struct {
    bus     *core.EventBus
    cfg     ConfigSource         // small interface, see below
    client  *http.Client
    apiAddr func() string        // optional, returns "" if API server not up
    queue   chan deliveryJob
    workers int
}

type ConfigSource interface {
    Webhooks() []core.WebhookConfig
}

type deliveryJob struct {
    webhook core.WebhookConfig
    body    []byte       // pre-marshaled payload
    deliveryID string    // uuid v4
    attempt int
}

func New(bus *core.EventBus, cfg ConfigSource, client *http.Client, apiAddr func() string) *Dispatcher
func (d *Dispatcher) Start(ctx context.Context) error
```

Lifecycle:

1. `Start` subscribes to the bus with buffer 256 and spawns one fan-out
   goroutine plus `workers` worker goroutines (default 4).
2. Fan-out reads events from the bus channel. For each event, it walks
   `cfg.Webhooks()`, applies `filter.matches` per webhook, marshals the
   payload once, then pushes a `deliveryJob` per matching webhook onto the
   shared queue. If the queue is full, the job is dropped and a counter is
   incremented; the counter is logged once per second at warn level.
3. Workers read jobs from the queue and perform a POST with `client.Do`
   (timeout 10s set on the `http.Client`). Result handling:
   - 2xx → success.
   - 4xx → permanent failure, log at warn level with status code and body
     snippet (truncated to 200 bytes), do not retry.
   - 5xx, network error, or timeout → retry with backoff `[1s, 5s, 30s]` up
     to 3 retries (4 attempts total). On final failure, log warn.
4. `ctx` cancellation drains the queue: fan-out stops accepting new events,
   workers finish their current job (respecting the per-request timeout),
   then return.

`ConfigSource` is a small interface so the dispatcher does not depend on the
full `core.Config` type and is trivial to fake in tests.

`apiAddr` returns the API server bind address (e.g. `http://127.0.0.1:7421`)
when the API mode is active, or `""` otherwise. Used to populate the `api.*`
URLs in the payload.

### 5. `main.go` wiring

The dispatcher starts when `webhooks` is non-empty in the config, regardless
of which interface (TUI, GUI, API) is active. It runs in the background as a
goroutine of the main process. If only the GUI is active, the dispatcher
still runs because session monitoring is happening in the parent process.

```go
bus := core.NewEventBus()
manager.SetEventBus(bus)

if len(cfg.Webhooks) > 0 {
    dispatcher := webhook.New(bus, cfg, httpClient, apiAddrFunc)
    go dispatcher.Start(rootCtx)
}
```

`rootCtx` is the existing process-lifetime context; cancellation on shutdown
flows naturally into the dispatcher.

## Payload

The body of every POST:

```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "event": "state_transition",
  "session_id": "abc123",
  "agent": "claude",
  "from": "Idle",
  "to": "WaitingForUser",
  "project_path": "/Users/foo/code/bar",
  "timestamp": "2026-05-19T14:30:00Z",
  "api": {
    "session_url": "http://127.0.0.1:7421/api/sessions/abc123",
    "detail_url": "http://127.0.0.1:7421/api/sessions/abc123/full"
  }
}
```

Field semantics:

- `id` — UUID v4 generated per delivery. Same `id` is reused across retries
  of the same job so consumers can deduplicate.
- `event` — currently always `state_transition`. Future event types (e.g.
  `session_created`, `cost_threshold_crossed`) would add new values.
- `from` / `to` — string names of `ActivityKind`. The canonical names match
  the values accepted in `config.events`.
- `timestamp` — RFC 3339 UTC.
- `api` — present only if the API server is running and its address is
  known. Absent (object omitted entirely) otherwise. Consumers that always
  need the URL should run lazyagent with `--api`.

Outgoing HTTP headers:

| Header | Value |
| --- | --- |
| `Content-Type` | `application/json` |
| `User-Agent` | `lazyagent/<version>` |
| `X-Lazyagent-Event` | `state_transition` |
| `X-Lazyagent-Delivery` | `<id>` (matches body `id`) |
| `X-Lazyagent-Signature` | `sha256=<hex>` (only if `secret` configured) |

The signature is `hex.EncodeToString(hmac.New(sha256, secret).Sum(body))`,
computed over the exact serialized body bytes. This is the same convention
used by GitHub webhooks; consumers can reuse their existing receivers.

## Configuration Example

```json
{
  "agents": ["all"],
  "exclude_cwd_substrings": [],
  "webhooks": [
    {
      "name": "slack-needs-input",
      "url": "https://hooks.slack.com/services/T00/B00/XXX",
      "secret": "abc123sharedwithslack",
      "events": ["waiting_for_user"],
      "agents": ["claude", "codex"]
    },
    {
      "name": "dashboard-everything",
      "url": "https://my-dashboard.local/api/lazyagent",
      "events": [],
      "agents": []
    }
  ]
}
```

The first webhook only fires for `claude` or `codex` sessions transitioning
to `WaitingForUser`. The second fires for every transition of every agent.

## Error Handling

| Situation | Behavior |
| --- | --- |
| Subscriber channel full at `Publish` | Drop event for that subscriber; debug log, rate-limited counter |
| Queue full at fan-out | Drop job; warn log once per second with dropped-since-last-log count |
| HTTP 2xx | Success; debug log |
| HTTP 4xx | No retry; warn log with status + body snippet (≤200 bytes) |
| HTTP 5xx / network / timeout | Retry with backoff `[1s, 5s, 30s]`, max 3 retries (4 attempts); warn on final failure |
| Invalid webhook config | Skipped at load time with warning; other webhooks unaffected |
| Config reload mid-flight | In-flight jobs finish under old config; new events use new config |
| Process shutdown | `ctx` cancellation: fan-out stops, workers drain current job within per-request timeout |

## Observability

- Standard log lines via `log` package (consistent with the rest of the
  codebase), prefixed `webhook:`.
- Counters for delivered / failed / dropped per webhook name, logged once per
  minute at info level if non-zero. Kept in-memory only.
- No metrics endpoint and no Prometheus exposition in MVP; that would
  expand scope beyond what upstream review would accept in one PR.

## Testing

`internal/core/eventbus_test.go`

- Publish/Subscribe basic delivery order.
- Drop-on-full: subscriber with `buf=1`, publish 3 events, verify exactly the
  first is received and no goroutine is blocked.
- Unsubscribe is idempotent and safe under concurrent publish.
- Race detector clean (`go test -race`).

`internal/core/activity_test.go` additions

- Update emits transition event on changed activity, with correct
  `From`/`To`/`SessionID`/`Agent`/`ProjectPath`.
- Update emits no event when activity is unchanged.
- New session emits `From: ActivityUnknown`.
- Disappearing session emits nothing in MVP.

`internal/webhook/filter_test.go`

- Empty `events` matches any event kind.
- Empty `agents` matches any agent.
- Both specified: AND across the two filters.
- Unknown event kind in config is ignored at filter time (defense in depth
  even though load-time validation should prevent this).

`internal/webhook/hmac_test.go`

- Known test vector: secret `"it's a secret"`, body `{"foo":"bar"}` →
  expected signature is a fixed hex string. This guards against accidental
  changes to the signing format.

`internal/webhook/dispatcher_test.go`

- POST to `httptest.Server` succeeds, payload body and headers match.
- 500 then 200: exactly one retry, success.
- 500 throughout: 4 attempts total (initial + 3 retries), then give up.
- 400: 1 attempt, no retry.
- Queue full: events beyond capacity are dropped, dropped-counter increments.
- Graceful shutdown: `cancel(ctx)`, current job completes, no panic.

Integration test: configure a single webhook pointing at a test server, push
an event through a real `EventBus` and `ActivityTracker.Update`, assert the
server received the expected POST.

## Documentation

- New page `docs/reference/webhooks.md` (Astro Starlight format matching the
  rest of `docs/`) with: motivation, config example, payload schema, signing
  reference (including a 10-line Python verification snippet), delivery
  semantics, troubleshooting (queue drops, 4xx).
- README: one-line mention under "Features".
- `docs/reference/configuration.md`: document the new `webhooks` field.
- `docs/reference/roadmap.md`: move "Outbound webhooks on status changes"
  from "Future ideas" to a new `v0.10` section once shipped.

## Rollout

- Default `webhooks: []` (or field absent) → no dispatcher started, no
  behavior change.
- Existing SSE behavior unchanged; the new bus is additive.
- No config schema version bump needed (the field is additive and absent in
  old configs).

## Open Questions

None that block implementation. Two design notes for the implementation plan:

1. Whether the dispatcher logs to `log` or to a dedicated logger is left to
   the implementation plan. The codebase currently uses `log` everywhere; we
   should match that unless the implementer has a reason to introduce
   structured logging in this PR.
2. The `agents` field's "agent name" is currently a `string` in
   `model.Session.Agent`. We rely on those values being stable. If the
   project later introduces an `AgentKind` enum, the validation list and
   webhook filter should use it. This is a future refactor, not a blocker.
