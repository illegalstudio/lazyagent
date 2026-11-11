---
title: "Configuration"
description: "Every field in ~/.config/lazyagent/config.json, with defaults and meanings."
sidebar:
  order: 2
---

lazyagent reads `~/.config/lazyagent/config.json` (or `$XDG_CONFIG_HOME/lazyagent/config.json` when set). The file is created with defaults on first run, and any missing keys are back-filled next time lazyagent loads it.

## Default config

```json
{
  "window_minutes": 30,
  "default_filter": "",
  "editor": "",
  "launch_at_login": false,
  "notifications": false,
  "notify_after_sec": 30,
  "agents": {
    "amp": true,
    "claude": true,
    "codex": true,
    "cursor": true,
    "grok": true,
    "kilo": true,
    "kimi": true,
    "opencode": true,
    "pi": true
  },
  "claude_dirs": [],
  "exclude_cwd_substrings": [],
  "api_salt": "lazyagent-api-v1-2CwLr3D6GKbVv5m0Pu1nHQ",
  "tui": {
    "theme": "auto"
  }
}
```

## Fields

### `window_minutes`

Default: `30`. Time window (in minutes) for session visibility. Sessions older than this are hidden from the list but still on disk. Widen in-session with <kbd>+</kbd> / <kbd>-</kbd>.

### `default_filter`

Default: `""` (empty). The activity filter applied on startup. Valid values: `""`, `"active"`, `"waiting"`, `"thinking"`, `"writing"`, … See [Activity states](../concepts/activity-states.md) for the full list.

### `editor`

Default: `""`. Overrides `$VISUAL` / `$EDITOR` when opening session CWDs. See [Editor support](editor-support.md).

### `launch_at_login`

Default: `false`. Auto-start the macOS desktop app at login. Only honored on macOS.

### `notifications`

Default: `false`. Show macOS notifications when a session needs input. Requires `--gui` to be running (the notification permission lives on the GUI process).

### `notify_after_sec`

Default: `30`. Seconds a session must stay in `waiting` before triggering a notification. Avoids noise from short pauses.

### `agents`

Default: all `true`. Per-agent enable flags. Disabled agents are skipped even when `--agent all` is active. Useful if you have sessions you'd rather lazyagent not scan (e.g. a noisy Cursor workspace you don't care about).

```json
{
  "agents": {
    "amp": true,
    "claude": true,
    "codex": true,
    "cursor": false,
    "grok": true,
    "kilo": true,
    "kimi": true,
    "opencode": true,
    "pi": true
  }
}
```

### `claude_dirs`

Default: `[]`. Extra Claude base directories to scan. Each entry must be a directory that contains a `projects/` subfolder.

```json
{
  "claude_dirs": [
    "/path/to/custom-claude-home",
    "/another/claude-home"
  ]
}
```

When empty, lazyagent auto-detects from the `CLAUDE_CONFIG_DIR` environment variable, falling back to `~/.claude`. Use this field if you keep Claude sessions somewhere non-standard and want lazyagent to pick them up without setting env vars every time.

### `exclude_cwd_substrings`

Default: `[]`. List of substrings; any session whose working directory (CWD) contains one of them is hidden from every interface (TUI, tray panel, HTTP API). Matching is a literal substring check on the full CWD path, case-sensitive.

```json
{
  "exclude_cwd_substrings": [
    ".claude-mem/observer-sessions",
    "/tmp/scratch"
  ]
}
```

Useful for hiding background or automated sessions (e.g. observer processes, autonomous loops) that run without user attention and would otherwise clutter the active list. Sessions are still on disk and visible to other tools — this only affects what lazyagent shows.

Substring matching is intentional and broad: `"test"` would also hide `/home/me/projects/latest`. Use a path-shaped fragment (with a `/` in it) when you want to be specific.

### `api_passphrase`

Default: `""` (empty — auth not yet configured). The passphrase used with `api_salt` to derive the bearer token that protects the [HTTP API](../interfaces/http-api.md). Created interactively on the first run of `lazyagent --api`; values shorter than 12 characters are rejected. You can edit it manually here at any time, and the next API server startup will derive a new token from it.

Anyone who can read this file can talk to your API. lazyagent writes the config file with `0600` permissions and the config directory with `0700`, but you should still protect your home directory the same way you protect any other secret-bearing config (`~/.ssh`, `~/.aws`, etc).

The `LAZYAGENT_API_PASSPHRASE` environment variable overrides this field at startup and is never persisted to disk — useful for CI or service-managed deployments.

### `api_salt`

Default: generated once per install. Public salt used with `api_passphrase` for PBKDF2 token derivation. It is also available from `GET /api/auth` so clients can derive tokens without reading this file. It is not secret, but keep it stable; changing it invalidates previously derived tokens until clients fetch the new salt.

### `tui.theme`

Default: `"auto"` for new installations. Supported values:

- `"auto"` — detect the terminal's background color at startup and pick the matching palette
- `"dark"` — Catppuccin-derived palette
- `"light"` — paper-like palette for bright environments

All TUI colors (panels, activity labels, help bar, overlays) are driven by the theme.

**Existing installations are not migrated.** lazyagent writes the config file on first run, so any install predating `auto` already carries `"theme": "dark"` on disk and keeps it. A config with no `tui` block at all expresses no theme choice, though, and picks up `"theme": "auto"` on the next load. Set `"theme": "auto"` by hand to opt in.

Detection is not always possible, and every failure resolves to `dark` — the value the TUI used unconditionally before `auto` existed, so nothing degrades. See [Terminal UI](../interfaces/terminal-ui.md) for the full behavior.

### `card_density`

Default: `"live"`. Card density for the GUI's desktop-mode dashboard (the card grid shown after detaching — see the [macOS](../interfaces/macos-gui.md) or [Linux](../interfaces/linux-gui.md) GUI guide). One of `"compact"`, `"rich"`, or `"live"`. Invalid or missing values fall back to `"live"`.

### `terminal`

Default: `"terminal"` (the platform default). Terminal emulator used by actions that open a terminal window (Resume, terminal `$EDITOR`). One of `"terminal"` or `"kitty"`; unknown values use the platform default. On Linux, lazyagent first uses `xdg-terminal-exec`, then the desktop's alternatives entry and common installed emulators. Also editable from the desktop app's Settings panel.

Kitty note: macOS kitty cannot share windows across instances, so lazyagent keeps its terminal windows in a dedicated kitty instance (instance group `lazyagent`) and raises it automatically after each launch. No kitty configuration is required.

### `detail_width`

Default: `400`. Width in pixels of the desktop-mode detail panel (drag its left edge to resize, double-click to reset). Values outside 300–2000 fall back to `400`.

### `webhooks`

Default: `[]` (empty — no outbound webhooks). A list of HTTP endpoints that receive a POST whenever a session changes activity state. Each entry can filter by event type and agent source, and optionally sign requests with HMAC-SHA256.

```json
{
  "webhooks": [
    {
      "name": "slack-needs-input",
      "url": "https://hooks.slack.com/services/T00/B00/XXX",
      "secret": "abc123",
      "events": ["waiting"],
      "agents": ["claude"]
    }
  ]
}
```

See [Outbound Webhooks](webhooks.md) for the full field reference, payload schema, request headers, HMAC verification, delivery semantics, and troubleshooting tips.

## HTTP API environment variables

These variables are supplied to the process that starts the API server. Restart the API after changing either value. Environment values are not saved to `config.json`.

| Variable | Purpose |
|----------|---------|
| `LAZYAGENT_API_PASSPHRASE` | Overrides `api_passphrase` for client authentication to the lazyagent API. |
| `LAZYAGENT_CURSOR_API_KEY` | Optional Cursor user API key that enables cloud agents in `GET /api/agents`. Without it, that endpoint returns local sessions only. |

For example:

```bash
LAZYAGENT_CURSOR_API_KEY='YOUR_CURSOR_API_KEY' lazyagent --api
```

The Cursor cloud key has no JSON config field or desktop Settings control. For automatic startup, set it in the service or launcher's environment or secret manager. See [Cursor cloud agent setup](../interfaces/http-api.md#cursor-cloud-agent-setup) for key creation, shell usage, and client authentication.

## Where the config file lives

| OS | Path |
|----|------|
| macOS | `~/.config/lazyagent/config.json` |
| Linux | `~/.config/lazyagent/config.json` (or `$XDG_CONFIG_HOME/lazyagent/config.json`) |

Related files in the same directory:

- `session-names.json` — persistent custom session names, one JSON object keyed by session ID. Changes land here whenever you rename a session from any interface.
