---
title: "Quickstart"
description: "Launch lazyagent for the first time, pick an interface, and combine them."
sidebar:
  order: 2
---

## First launch

With no arguments, lazyagent opens the terminal UI and scans every supported agent:

```bash
lazyagent
```

You'll see a list of sessions on the left and a detail panel on the right. Use <kbd>↑</kbd>/<kbd>↓</kbd> or <kbd>j</kbd>/<kbd>k</kbd> to navigate, <kbd>tab</kbd> to switch focus between panels, and <kbd>q</kbd> or <kbd>ctrl+c</kbd> to quit. The full list of shortcuts lives in the [Terminal UI](../interfaces/terminal-ui.md) page.

## Pick a single agent

Scope the scan to one agent with `--agent`:

```bash
lazyagent --agent claude         # Claude Code CLI + Desktop
lazyagent --agent cursor         # Cursor IDE
lazyagent --agent codex          # Codex CLI
lazyagent --agent amp            # Amp CLI
lazyagent --agent pi             # pi coding agent
lazyagent --agent opencode       # OpenCode
lazyagent --agent all            # everything (default)
```

You can also disable agents permanently in [Configuration](../reference/configuration.md#agents).

## Pick a different interface

lazyagent is one binary with three interfaces, selectable by flag:

```bash
lazyagent --gui                  # macOS menu bar app (detaches from terminal)
lazyagent --api                  # HTTP API on http://127.0.0.1:7421
lazyagent --api --host :8080     # API on a custom address
```

And they're combinable:

```bash
lazyagent --tui --api            # TUI in foreground, API in background
lazyagent --gui --api            # menu bar app + API
lazyagent --tui --gui --api      # everything at once
```

## Maintenance at a glance

Two subcommands keep chat transcripts under control:

```bash
lazyagent prune --days 30        # delete sessions idle for >30 days
lazyagent compact                # shrink session files in place
```

Both support `--dry-run` and an interactive agent picker; see [Prune](../maintenance/prune.md) and [Compact](../maintenance/compact.md) for the full reference.

## Next steps

- Learn the mental model in [How it works](../concepts/how-it-works.md)
- Skim the [Supported agents](../concepts/supported-agents.md) table
- Tweak the time window, theme, and per-agent toggles in [Configuration](../reference/configuration.md)
