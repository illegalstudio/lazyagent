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
    "opencode": true,
    "pi": true
  },
  "claude_dirs": [],
  "tui": {
    "theme": "dark"
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

Default: `false`. Auto-start the macOS menu bar app at login. Only honored on macOS.

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

### `tui.theme`

Default: `"dark"`. Supported values:

- `"dark"` — the default, Catppuccin-derived palette
- `"light"` — paper-like palette for bright environments

All TUI colors (panels, activity labels, help bar, overlays) are driven by the theme.

## Where the config file lives

| OS | Path |
|----|------|
| macOS | `~/.config/lazyagent/config.json` |
| Linux | `~/.config/lazyagent/config.json` (or `$XDG_CONFIG_HOME/lazyagent/config.json`) |

Related files in the same directory:

- `session-names.json` — persistent custom session names, one JSON object keyed by session ID. Changes land here whenever you rename a session from any interface.
