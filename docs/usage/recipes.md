---
title: "Recipes"
description: "End-to-end walkthroughs for common lazyagent workflows."
sidebar:
  order: 2
---

Concrete, copy-pasteable recipes for the workflows people actually run.

## Daily driver setup (macOS)

You want the menu bar app always running, the API available for a mobile companion, and the TUI available in the terminal when you need a denser view.

```bash
# In a LaunchAgent or at terminal startup — detaches immediately
lazyagent --gui --api

# When you want a denser session list in the terminal:
lazyagent --tui
```

The tray and API share the same engine, so the TUI sees the same data they do. Quitting the TUI doesn't touch the tray or API. See [macOS GUI](../interfaces/macos-gui.md) and [HTTP API](../interfaces/http-api.md).

## Monitor only one agent

Scope the scan to a single agent with `--agent`:

```bash
lazyagent --agent claude   # Claude Code CLI and Desktop
lazyagent --agent codex    # Codex CLI only
```

To permanently hide an agent without passing `--agent` every time, set it to `false` in the [`agents` config block](../reference/configuration.md#agents).

## Mobile companion on the same Wi-Fi

```bash
# On your laptop
lazyagent --api --host 0.0.0.0:7421

# On your phone (same network)
# Connect to http://<laptop-ip>:7421/api/events
```

The API exposes an SSE stream that updates in real time. See the [React Native example](../interfaces/http-api.md#react-native) for a full client snippet.

> ⚠️ **No authentication**. Only expose the API on networks you trust.

## Quick status check from the shell

When you just want to see active sessions without opening a full UI:

```bash
curl -s http://127.0.0.1:7421/api/stats
# → {"total_sessions":5,"active_sessions":2,"window_minutes":30}

curl -s 'http://127.0.0.1:7421/api/sessions?filter=active' \
  | jq -r '.[] | "\(.activity)\t\(.short_name)"'
# → thinking  …/projects/myapp
# → writing   …/projects/worker
```

Requires that `lazyagent --api` is running somewhere on your machine.

## Find sessions waiting for your input

```bash
# TUI: press `f` until the filter lands on "waiting"

# API:
curl -s 'http://127.0.0.1:7421/api/sessions?filter=waiting' \
  | jq '[.[].short_name]'
```

Pair with `notifications: true` in the [config](../reference/configuration.md) so the GUI nudges you proactively.

## Weekly tidy-up

The [Maintenance](../maintenance/prune.md) commands shine when run on a cadence. A safe weekly routine:

```bash
# 1. Preview what's stale — projects you haven't touched in a month
lazyagent prune --days 30 --orphaned --dry-run

# 2. If you like what you see, actually delete
lazyagent prune --days 30 --orphaned

# 3. Shrink whatever's left but still bulky (>512 KiB)
lazyagent compact --days 14 --dry-run

# 4. Commit to it
lazyagent compact --days 14
```

Each step asks for confirmation before mutating anything. Compact writes `.bak` sidecars by default — if a specific rewrite causes trouble, `mv session.jsonl.bak session.jsonl` rolls it back.

## Reclaim disk space aggressively

For maximum savings on Claude transcripts with large embedded images and tool snapshots:

```bash
lazyagent compact \
  --agent claude \
  --min-size-kb 100 \
  --threshold-kb 5 \
  --dry-run
```

Then drop `--dry-run`. Typical savings on a full `~/.claude/projects/` tree are 60–80% on the biggest transcripts.

## Purge a single project

You deleted `~/projects/abandoned-app` and want every session associated with it gone:

```bash
# Preview
lazyagent prune --orphaned --dry-run-verbose | grep abandoned-app

# Commit
lazyagent prune --orphaned --agent claude,codex,pi
```

`--orphaned` catches anything whose CWD no longer resolves — the exact case here.

## Running in a scheduled task

`cron`, `launchd`, or a shell loop can automate maintenance. Always include `--yes` to skip the interactive disclaimer:

```bash
# In crontab: compact Claude sessions every Sunday at 03:00
0 3 * * 0  /usr/local/bin/lazyagent compact --agent claude --days 7 --yes >> ~/.local/state/lazyagent-compact.log 2>&1
```

Script-friendly invariants:

- Exit code `0` on success (including "nothing to do"), `1` on partial failure, `2` on invalid flags — pair with `set -e` safely.
- stderr carries warnings and failures; stdout carries the summary tables.
- `--agent` is accepted, so non-interactive use never has to deal with the picker.

## Rebuilding session display names

You renamed a session via the TUI and regret it:

```bash
# TUI / GUI: press `r` on the session, submit empty → resets to default
# API:
curl -X DELETE http://127.0.0.1:7421/api/sessions/<session-id>/name
```

Session names live in `~/.config/lazyagent/session-names.json` and are shared across all interfaces in real time.

## Dev loop on lazyagent itself

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent
make tui                           # builds the TUI-only binary
./lazyagent --demo                 # quick sanity check with fake data
./lazyagent --tui --api            # real data + API
```

Full build instructions (including the Svelte frontend for the tray) live in [Development](../reference/development.md).
