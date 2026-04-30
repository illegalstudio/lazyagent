---
title: "Supported agents"
description: "Every agent lazyagent can monitor, where it reads session data from, and the per-agent quirks you should know about."
sidebar:
  order: 2
---

lazyagent supports seven agents out of the box. Each has a dedicated provider that knows the agent's on-disk layout.

| Agent | Path | Format | Prefix |
|-------|------|--------|--------|
| [Claude Code CLI](https://claude.ai/code) | `~/.claude/projects/*/` | JSONL | — |
| [Claude Code Desktop](https://claude.ai/code) | `~/.claude/projects/*/` + `~/Library/Application Support/Claude/claude-code-sessions/` | JSONL + JSON sidecar | `D` |
| [Cursor](https://cursor.com/) | `~/Library/Application Support/Cursor/User/globalStorage/state.vscdb` | SQLite | `C` |
| [Codex CLI](https://developers.openai.com/codex/) | `~/.codex/sessions/YYYY/MM/DD/*.jsonl` + `~/.codex/session_index.jsonl` | JSONL | `X` |
| [Amp CLI](https://ampcode.com/) | `~/.local/share/amp/threads/*.json` | Per-thread JSON | `A` |
| [pi coding agent](https://github.com/badlogic/pi-mono) | `~/.pi/agent/sessions/*/` | JSONL | `π` |
| [OpenCode](https://opencode.ai/) | `~/.local/share/opencode/opencode.db` | SQLite | `O` |

The prefix appears next to each session in the TUI list, the GUI panel, and the API response so you can tell at a glance which agent produced a session.

## Selecting a subset

Use `--agent` to scope the scan:

```bash
lazyagent --agent claude    # Claude Code CLI + Desktop
lazyagent --agent cursor
lazyagent --agent codex
lazyagent --agent amp
lazyagent --agent pi
lazyagent --agent opencode
lazyagent --agent all       # default
```

You can also enable/disable agents permanently via the [`agents` block](../reference/configuration.md#agents) in `config.json`. A disabled agent is skipped even when `--agent all` is active.

## Per-agent notes

### Claude Code (CLI and Desktop)

Both modes share the same `~/.claude/projects/<encoded-cwd>/*.jsonl` files. Desktop adds a metadata sidecar (title, permission mode, creation time) under `~/Library/Application Support/Claude/claude-code-sessions/local_*.json`. lazyagent reads both and merges them: Desktop sessions get the `D` prefix and their custom title.

Extra Claude base directories (e.g. when `CLAUDE_CONFIG_DIR` points elsewhere) can be added via [`claude_dirs`](../reference/configuration.md#claude_dirs).

### Cursor

Cursor stores everything in a single SQLite database (`state.vscdb`) as key-value entries: `composerData:<id>` for session metadata and `bubbleId:<id>:<bubble>` for message blocks. lazyagent polls this file every 3 seconds (no file watcher — WAL-mode writes don't trigger fsevents cleanly) and invalidates its cache based on journal position.

CWD is inferred from the Cursor workspace URI if available, otherwise from the first file path mentioned in the session.

### Codex CLI

Codex writes one JSONL per session under `~/.codex/sessions/YYYY/MM/DD/`. A separate `~/.codex/session_index.jsonl` carries the user-chosen thread names, which lazyagent joins into the session list.

### Amp CLI

Amp keeps a JSON blob per thread under `~/.local/share/amp/threads/*.json`. Newer Amp versions no longer write this locally — they sync from the server on demand. lazyagent works around this by running `amp threads export` every 15 seconds, diffing the result, and refreshing the local cache so you still see live threads.

### pi coding agent

Pi writes JSONL into `~/.pi/agent/sessions/--<encoded-cwd>--/`. The encoding is pi's own (path separators → `-`, surrounding `--`). lazyagent reverses it to surface the original CWD in the UI.

### OpenCode

OpenCode uses SQLite with relational tables (`session`, `message`, `part`). lazyagent polls every 3 seconds and detects sub-agents via `parent_id`. Tool names are normalized to the same activity taxonomy as the other agents.

## What's not supported (yet)

- **Roo Code, Continue, Cline, Aider**, and other agents with their own storage layouts — send an issue or PR with the on-disk format and we'll add a provider.
- The two maintenance commands (`prune`, `compact`) intentionally omit Cursor and OpenCode (third-party SQLite databases) and Amp (remote-resynced local files). See [Prune](../maintenance/prune.md) and [Compact](../maintenance/compact.md) for the reasoning.
- The [`limits`](../maintenance/limits.md) command supports only Claude Code and Codex — they're the only agents lazyagent observes that expose stable 5-hour and weekly windows.
