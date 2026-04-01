# lazyagent API

lazyagent exposes an HTTP API for monitoring and managing Claude Code sessions, designed for building external clients (mobile apps, dashboards, integrations).

## Starting the API server

```bash
# Default: http://127.0.0.1:7421
lazyagent --api

# Custom address
lazyagent --api --host :8080
lazyagent --api --host 0.0.0.0:7421

# Combined with TUI or GUI
lazyagent --tui --api
lazyagent --gui --api
lazyagent --tui --gui --api
```

The default port is **7421**. If it's busy, the server tries up to 10 sequential ports (7421–7431) and binds to the first available one. The actual address is printed to stderr on startup.

When `--host` is specified, it binds to that exact address with no fallback.

## Interactive playground

Open **http://127.0.0.1:7421/api** in a browser to access the interactive API playground. It lets you test all endpoints and connect to the SSE stream with a single click.

## Endpoints

### GET /api/sessions

List all visible sessions within the configured time window.

**Query parameters** (optional):

| Parameter | Type   | Description |
|-----------|--------|-------------|
| `search`  | string | Filter by project path (case-insensitive substring match) |
| `filter`  | string | Filter by activity kind (e.g. `thinking`, `writing`, `idle`) |

**Response:** `200 OK`

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

**Activity values:** `idle`, `waiting`, `thinking`, `compacting`, `reading`, `writing`, `running`, `searching`, `browsing`, `spawning`

---

### GET /api/sessions/{id}

Get full details for a specific session.

**Response:** `200 OK`

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

**Response:** `404 Not Found` if session doesn't exist.

---

### PUT /api/sessions/{id}/name

Set a custom name for a session. Names are persisted in `~/.config/lazyagent/session-names.json` and synced across TUI, tray, and API in real-time.

**Request body:**

```json
{
  "name": "my-api-project"
}
```

An empty `"name"` resets the session to its default path-based name.

**Response:** `200 OK`

```json
{
  "session_id": "abc123",
  "custom_name": "my-api-project"
}
```

Triggers an SSE `update` event to all connected clients.

---

### DELETE /api/sessions/{id}/name

Remove the custom name from a session (reset to default path-based name).

**Response:** `200 OK`

```json
{
  "session_id": "abc123",
  "custom_name": ""
}
```

Triggers an SSE `update` event to all connected clients.

---

### GET /api/stats

Summary statistics.

**Response:** `200 OK`

```json
{
  "total_sessions": 5,
  "active_sessions": 2,
  "window_minutes": 30
}
```

---

### GET /api/config

Current lazyagent configuration.

**Response:** `200 OK`

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

---

### GET /api/events

**Server-Sent Events (SSE)** stream for real-time updates. The server pushes an `update` event whenever session data changes (file watcher triggers, activity state changes, or periodic reload).

An initial snapshot is sent immediately upon connection.

**Event format:**

```
event: update
data: {"sessions":[...],"stats":{"total_sessions":5,"active_sessions":2,"window_minutes":30}}
```

The `data` field contains a JSON object with:

| Field      | Type            | Description |
|------------|-----------------|-------------|
| `sessions` | `SessionItem[]` | Same format as `GET /api/sessions` |
| `stats`    | `StatsResponse` | Same format as `GET /api/stats` |

**JavaScript example:**

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

**React Native example:**

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

**Notes:**
- The connection auto-reconnects (standard SSE behavior)
- Events are sent when data changes (file watcher, activity state transitions) and on the 30-second safety reload tick (even if nothing changed)

## Data freshness

The API server uses three mechanisms to keep data current:

1. **File system watcher** — detects JSONL file changes in `~/.claude/projects/` with 200ms debounce
2. **Activity ticker** (1s) — re-evaluates activity states (idle/thinking/waiting transitions)
3. **Safety reload** (30s) — full rescan as fallback

SSE clients receive push notifications from all three sources. REST clients see the latest state on each request.

## Network access

By default the server binds to `127.0.0.1` (localhost only). To expose it on the network (e.g. for a mobile app on the same WiFi):

```bash
lazyagent --api --host 0.0.0.0:7421
```

> **Warning:** There is no authentication. Only expose the API on trusted networks.
