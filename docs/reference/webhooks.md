---
title: "Outbound Webhooks"
description: "Send session state transitions to Slack, dashboards, or CI pipelines via HTTP POST."
sidebar:
  order: 3
---

Outbound webhooks let lazyagent push a JSON payload to any HTTP endpoint whenever a session changes activity state. Common uses include posting to a Slack channel when an agent is waiting for input, feeding a custom dashboard, or triggering a CI step when a long-running session goes idle.

## Configuration

Add a `webhooks` array to `~/.config/lazyagent/config.json`:

```json
{
  "webhooks": [
    {
      "name": "slack-needs-input",
      "url": "https://hooks.slack.com/services/T00/B00/XXX",
      "secret": "abc123sharedwithslack",
      "events": ["waiting"],
      "agents": ["claude", "codex"]
    },
    {
      "name": "dashboard-everything",
      "url": "https://my-dashboard.local/api/lazyagent"
    }
  ]
}
```

The first entry fires only when a Claude Code or Codex session enters the `waiting` state, and signs each request with an HMAC-SHA256 header. The second entry receives every transition from every agent, unsigned.

## Field reference

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Human-readable identifier used in log lines. |
| `url` | string | yes | Destination endpoint. `http://` and `https://` are both accepted. |
| `secret` | string | no | When set, each request carries an `X-Lazyagent-Signature` header (see [HMAC verification](#hmac-verification)). |
| `events` | string array | no | Activity kinds to deliver. Empty or absent means all events. Valid values: `idle`, `waiting`, `thinking`, `compacting`, `reading`, `writing`, `running`, `searching`, `browsing`, `spawning`. |
| `agents` | string array | no | Agent sources to deliver. Empty or absent means all agents. Valid values: `claude`, `codex`, `pi`, `cursor`, `amp`, `opencode`. |
| `enabled` | boolean | no | Defaults to `true`. Set to `false` to disable the entry without removing it. |

## Payload schema

Every delivery is an HTTP POST with a JSON body:

```json
{
  "id": "f47ac10b-58cc-4372-a567-0e02b2c3d479",
  "event": "state_transition",
  "session_id": "abc123",
  "agent": "claude",
  "from": "idle",
  "to": "waiting",
  "project_path": "/Users/foo/code/bar",
  "timestamp": "2026-05-19T14:30:00Z",
  "api": {
    "session_url": "http://127.0.0.1:7421/api/sessions/abc123"
  }
}
```

The `api` object is present only when lazyagent is running with `--api` and the HTTP server is bound. Omit any logic that depends on it when running without `--api`.

## Request headers

| Header | Value |
|---|---|
| `Content-Type` | `application/json` |
| `User-Agent` | `lazyagent/<version>` |
| `X-Lazyagent-Event` | `state_transition` |
| `X-Lazyagent-Delivery` | UUID matching the `id` field in the body |
| `X-Lazyagent-Signature` | `sha256=<hex>` (only when `secret` is configured) |

## HMAC verification

When `secret` is set, the signature is computed over the raw request body using HMAC-SHA256. Verify it on the receiving side before trusting the payload:

```python
import hmac, hashlib

secret = b"abc123sharedwithslack"
body = request.get_data()
sig = "sha256=" + hmac.new(secret, body, hashlib.sha256).hexdigest()
if not hmac.compare_digest(sig, request.headers["X-Lazyagent-Signature"]):
    abort(401)
```

Always use a constant-time comparison (`hmac.compare_digest` or equivalent) to avoid timing attacks.

## Delivery semantics

- **Asynchronous, best-effort.** Webhooks are dispatched in the background and never block session monitoring.
- **Bounded queue.** Each dispatcher holds up to 256 pending deliveries. If the queue is full, new events are dropped and a log line is emitted.
- **Retry on transient failures.** HTTP 5xx responses and network errors trigger exponential backoff: 1 s, 5 s, 30 s. Maximum 4 attempts total.
- **No retry on 4xx.** Client errors (wrong URL, bad auth, malformed payload on the consumer side) are logged with the status code and a body snippet, then discarded.
- **Dedup window.** Duplicate transitions within 2 seconds are coalesced. This prevents double-delivery when multiple in-process managers (e.g. `--tui` and `--gui` running together) each observe the same transition.
- **`api.*` URLs.** Present only when `--api` is active and the server is bound; absent otherwise.

## Troubleshooting

**I see no POSTs.**
Verify that the `webhooks` array is non-empty and well-formed JSON. lazyagent logs invalid webhook entries on startup with a line like `config: webhook "name": ...`. Also confirm the `events` and `agents` filters match what you expect.

**I see duplicate deliveries.**
Check whether you are running more than one lazyagent process simultaneously (e.g. `--tui` in one terminal and `--gui` in the background). Each process has its own dispatcher and can emit independent POSTs for the same transition. The 2-second dedup window covers duplicate detection within a single process only.

**4xx errors appear in the log.**
The consumer is rejecting the request. lazyagent does not retry 4xx responses by design — fix the consumer endpoint (URL, auth headers, expected payload shape) and the next transition will deliver cleanly.
