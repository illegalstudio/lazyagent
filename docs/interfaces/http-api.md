---
title: "HTTP API"
description: "A REST + Server-Sent Events API for building external clients — mobile apps, dashboards, integrations."
sidebar:
  order: 3
---

```bash
lazyagent --api
```

Starts a read-only HTTP API on `http://127.0.0.1:7421`. If the port is busy the server tries up to 10 sequential ports (7421–7431) and binds to the first available one; the actual address is printed to stderr on startup. Pass `--host` to pin a custom address and disable the fallback.

![lazyagent API playground](../../assets/api.png)

## Interactive playground

Open **http://127.0.0.1:7421/api** in a browser for the interactive playground. You can try every endpoint, inspect payloads, and connect to the SSE stream with a single click — useful while prototyping a client.

## Data freshness

Three mechanisms keep the API current:

1. **File system watcher** — detects JSONL changes with a 200 ms debounce.
2. **Activity ticker (1 s)** — re-evaluates activity states (idle/thinking/waiting transitions).
3. **Safety reload (30 s)** — full rescan as a fallback in case a file event was missed.

SSE clients receive pushes from all three. REST clients see the latest state on each request.

## Network access

By default the server binds to `127.0.0.1` (localhost only). To expose it on the network — e.g. for a mobile companion app on the same Wi-Fi:

```bash
lazyagent --api --host 0.0.0.0:7421
```

> ⚠️ **No authentication.** Only expose the API on trusted networks. The API is read-only apart from session renaming, but exposing internals (paths, prompts, token usage) to the open internet is a bad idea.

## Endpoints

### `GET /api/sessions`

List all visible sessions within the configured [time window](../reference/configuration.md).

**Query parameters** (all optional):

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `search`  | string | Filter by project path (case-insensitive substring match) |
| `filter`  | string | Filter by activity kind (e.g. `thinking`, `writing`, `idle`) |

**Response: `200 OK`**

```json
[
  {
    "session_id": "abc123",
    "cwd": "/Users/me/projects/myapp",
    "short_name": "…/projects/myapp",
    "custom_name": "my-api-project",
    "activity": "thinking",
    "is_active": true,
    "model": "claude-sonnet-4-20250514",
    "git_branch": "main",
    "cost_usd": 0.42,
    "last_activity": "2026-03-08T15:30:00Z",
    "total_messages": 24
  }
]
```

Possible `activity` values: `idle`, `waiting`, `thinking`, `compacting`, `reading`, `writing`, `running`, `searching`, `browsing`, `spawning`. See [Activity states](../concepts/activity-states.md) for meanings.

### `GET /api/sessions/{id}`

Full details for a single session.

**Response: `200 OK`**

```json
{
  "session_id": "abc123",
  "cwd": "/Users/me/projects/myapp",
  "short_name": "…/projects/myapp",
  "custom_name": "my-api-project",
  "activity": "writing",
  "is_active": true,
  "model": "claude-sonnet-4-20250514",
  "git_branch": "feature/api",
  "cost_usd": 1.23,
  "last_activity": "2026-03-08T15:30:00Z",
  "total_messages": 48,
  "version": "1.0.33",
  "is_worktree": false,
  "main_repo": "",
  "input_tokens": 125000,
  "output_tokens": 42000,
  "cache_creation_tokens": 50000,
  "cache_read_tokens": 80000,
  "user_messages": 20,
  "assistant_messages": 28,
  "current_tool": "Edit",
  "last_file_write": "internal/api/server.go",
  "last_file_write_at": "2026-03-08T15:29:50Z",
  "recent_tools": [
    {"name": "Read", "timestamp": "2026-03-08T15:29:30Z"},
    {"name": "Edit", "timestamp": "2026-03-08T15:29:50Z"}
  ],
  "recent_messages": [
    {"role": "user", "text": "Add the API endpoint", "timestamp": "2026-03-08T15:28:00Z"},
    {"role": "assistant", "text": "I'll create the endpoint...", "timestamp": "2026-03-08T15:28:05Z"}
  ],
  "resume_command": "claude --resume abc123"
}
```

Returns **`404 Not Found`** if the session doesn't exist.

### `PUT /api/sessions/{id}/name`

Set a custom name for a session. Names are persisted in `~/.config/lazyagent/session-names.json` and shared across TUI, GUI, and API in real time.

**Request body:**

```json
{ "name": "my-api-project" }
```

An empty `"name"` resets to the default path-based label.

**Response: `200 OK`**

```json
{ "session_id": "abc123", "custom_name": "my-api-project" }
```

Triggers an SSE `update` event to all connected clients.

### `DELETE /api/sessions/{id}/name`

Remove the custom name (reset to default).

**Response: `200 OK`**

```json
{ "session_id": "abc123", "custom_name": "" }
```

Also triggers an SSE `update`.

### `GET /api/stats`

Summary statistics.

**Response: `200 OK`**

```json
{
  "total_sessions": 5,
  "active_sessions": 2,
  "window_minutes": 30
}
```

### `GET /api/config`

Current lazyagent configuration.

**Response: `200 OK`**

```json
{
  "window_minutes": 30,
  "default_filter": "",
  "editor": "",
  "launch_at_login": false,
  "notifications": false,
  "notify_after_sec": 30
}
```

### `GET /api/events`

**Server-Sent Events** stream for real-time updates. The server pushes an `update` event whenever session data changes (file-watcher trigger, activity-state transition, periodic reload). An initial snapshot is sent immediately on connection.

**Event format:**

```
event: update
data: {"sessions":[...],"stats":{"total_sessions":5,"active_sessions":2,"window_minutes":30}}
```

The `data` JSON contains:

| Field      | Type            | Description |
|------------|-----------------|-------------|
| `sessions` | `SessionItem[]` | Same shape as `GET /api/sessions` |
| `stats`    | `StatsResponse` | Same shape as `GET /api/stats` |

## Client examples

### Browser JavaScript

```javascript
const evtSource = new EventSource('http://127.0.0.1:7421/api/events');

evtSource.addEventListener('update', (e) => {
  const { sessions, stats } = JSON.parse(e.data);
  console.log(`${stats.active_sessions} active sessions`);
  sessions.forEach(s => {
    console.log(`${s.short_name}: ${s.activity}`);
  });
});
```

### React Native

```typescript
import EventSource from 'react-native-sse';

const es = new EventSource('http://YOUR_HOST:7421/api/events');

es.addEventListener('update', (event) => {
  const { sessions, stats } = JSON.parse(event.data);
  setSessions(sessions);
  setStats(stats);
});

// Cleanup
es.close();
```

Both examples rely on standard SSE auto-reconnect behavior — if the server restarts, the client reconnects and receives a fresh snapshot.
