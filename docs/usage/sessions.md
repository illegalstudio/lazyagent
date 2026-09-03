---
title: "Sessions for a directory"
description: "List every recorded session for the current directory — across all agents — and reopen one."
sidebar:
  order: 3
---

`lazyagent sessions` lists every session whose working directory is the
current directory (or a subdirectory of it), across all supported agents,
newest first. Selecting a session resumes it with the originating agent's
own CLI.

## Synopsis

```
lazyagent sessions [--agent NAME] [--json] [--dir PATH] [--yolo]
```

`lazyagent history` is retained as a legacy alias. It accepts the same flags
and behaves exactly like `lazyagent sessions`. The old history-specific table
output and `--all` flag have been removed.

## The picker

```
┌─ Sessions in ~/projects/foo (12) ───────────────────────────┐
│ ▸ claude  2h ago      84  fix build embed placeholder       │
│   codex   yesterday   31  webhook config models             │
│   grok    3d ago      12  docs limits                       │
└─────────────────────────────────────────────────────────────┘
  ↑/↓ move · enter normal · y YOLO · c/C copy normal/YOLO · q quit
```

Each row shows the agent, relative last-activity time, message count, and a
title (your custom session name when set, otherwise a preview of the first
user message).

| Key | Action |
|-----|--------|
| `↑`/`k`, `↓`/`j` | Move the cursor |
| `enter` | Reopen the session in this terminal |
| `n` | Reopen the session in normal mode |
| `y` | Reopen the session in YOLO mode |
| `c` | Copy the resume command to the clipboard |
| `C` | Copy the YOLO resume command to the clipboard |
| `q` / `esc` / `ctrl+c` | Quit without opening |

**Opening** runs the agent's resume command (e.g. `claude --resume <id>`)
with this terminal attached, from the session's own working directory when
it still exists. Lazyagent can execute the resume command directly for every
supported agent. pi has no distinct YOLO flag, so `y`, `C`, or `--yolo` on a
pi session reports that YOLO mode is unavailable.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent NAME` | string | `all` | Restrict the listing to one agent |
| `--json` | bool | `false` | Print the list as JSON on stdout and exit (no picker) |
| `--dir PATH` | string | current dir | List sessions for another directory |
| `--yolo` | bool | `false` | Make YOLO the default mode for opening with `enter`; `n` still opens normally |

## JSON output

`--json` emits an array (possibly `[]`), one object per session:

```json
[
  {
    "agent": "claude",
    "session_id": "abc123",
    "name": "fix-build",
    "cwd": "/Users/me/projects/foo",
    "last_activity": "2026-07-20T09:12:33Z",
    "messages": 84,
    "resume_command": "claude --resume abc123",
    "resume_command_yolo": "claude --dangerously-skip-permissions --resume abc123"
  }
]
```

Every object always contains all eight fields. Fields that do not apply are
emitted as empty strings rather than omitted, so scripts can rely on a stable
shape.

| Field | Type | Meaning |
|-------|------|---------|
| `agent` | string | Agent that owns the session |
| `session_id` | string | Agent-specific session identifier |
| `name` | string | Custom session name, agent-provided name, or `""` when neither exists |
| `cwd` | string | Recorded working directory |
| `last_activity` | string | Last activity timestamp in RFC 3339 format |
| `messages` | integer | Number of messages recorded for the session |
| `resume_command` | string | Command to resume the session, or `""` when the agent exposes none |
| `resume_command_yolo` | string | YOLO resume command, or `""` when the agent has no distinct YOLO mode |

## Performance

### Progressive picker

When you run `lazyagent sessions`, the picker opens immediately and results stream in as each agent's discovery completes. A footer displays `loading agents… (done/total)` during discovery. Once all agents finish, the footer switches to the normal keybinding hint. If discovery finishes with zero sessions, the command prints "No sessions found in …" and exits.

### Discovery cache

All session-discovery surfaces — `lazyagent sessions`, the TUI, the desktop GUI,
and the HTTP API — maintain persistent discovery caches under your system
cache directory. They use the same location and file format, so a warm cache
created by one surface can speed up another:

- **macOS**: `~/Library/Caches/lazyagent/`
- **Linux**: `~/.cache/lazyagent/`
- **Other**: per-platform defaults from `$XDG_CACHE_HOME` or equivalent

Cache files follow the pattern `discovery-<agent>.json` and `cwdindex-<agent>.json` (for example, `discovery-claude.json` and `cwdindex-claude.json` for Claude Code). Files are created with permission `0600`; the directory has `0700`.

Cache contents are **advisory**: deleting these files is always safe and won't
break anything. The next lazyagent surface you start simply re-scans the
session data and rebuilds them.

Repeat runs typically complete in tens of milliseconds when caches are warm; only sessions from files that changed on disk are re-read.

**Privacy note**: cache files may contain short transcript snippets (for example, the first message text for session preview). If you delete your entire cache directory, you also remove these cached snippets from disk.

### Directory-scoped optimization

The listing is optimized to discover sessions for the target directory without reading other directories' data, which speeds up results when your codebase spans multiple large directory hierarchies.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Success — including quitting the picker or an empty listing |
| `1` | Runtime failure (discovery, resume exec, clipboard) |
| `2` | Usage error (unknown `--agent`, `--dir` not a directory, no TTY without `--json`) |
