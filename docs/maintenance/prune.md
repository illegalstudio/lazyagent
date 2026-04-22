---
title: "Prune old sessions"
description: "Delete chat files that are old, orphaned, or both — with dry-run previews and a destructive-op disclaimer."
sidebar:
  order: 1
---

`lazyagent prune` deletes entire chat session files. It's the right tool when you want to **get rid of** sessions you no longer care about — old conversations for projects you've archived, orphaned transcripts whose folders you deleted months ago, or just the oldest N% of everything.

For *shrinking* sessions you want to keep, see [Compact](compact.md) instead.

## Synopsis

```
lazyagent prune  [--days N] [--orphaned]
                 [--agent LIST]
                 [--dry-run | --dry-run-verbose]
                 [--yes]
```

At least one of `--days` or `--orphaned` is required.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--days N` | int | *unset* | Include sessions idle more than N days |
| `--orphaned` | bool | `false` | Include sessions whose project folder is gone |
| `--agent LIST` | string | *unset* | Comma-separated subset: `claude,pi,codex`. Empty opens the picker |
| `--dry-run` | bool | `false` | Print a grouped summary, delete nothing |
| `--dry-run-verbose` | bool | `false` | Print one row per file, delete nothing |
| `--yes` | bool | `false` | Skip the destructive-op disclaimer |

## Quick reference

```bash
lazyagent prune --days 30                       # sessions idle >30 days
lazyagent prune --orphaned                      # sessions whose project folder is gone
lazyagent prune --days 30 --orphaned            # both filters (OR)
lazyagent prune --days 30 --dry-run             # preview: group by project
lazyagent prune --days 30 --dry-run-verbose     # preview: one row per file
lazyagent prune --days 30 --agent claude,codex  # limit to specific agents
lazyagent prune --days 30 --yes                 # skip the confirmation prompt
```

## Filters

At least one of `--days` or `--orphaned` is required. They combine with **OR**: a session is a candidate if it matches *either* filter.

- **`--days N`** — `LastActivity` is older than N days. Sessions without a recorded timestamp are skipped.
- **`--orphaned`** — the session's `CWD` no longer resolves to an existing directory on disk. An empty CWD is treated as orphaned (the session can't be attributed to a project anyway).

## Agent selection

If `--agent` is omitted, a bordered interactive picker opens with a checkbox for each supported agent:

```
  Select agents to prune
  Only agents with plain-text file storage are shown.

  ┌───────────────────────────────┐
  │ ▸ ○  ●  Claude Code    claude │
  │   ○  ●  pi coding agent pi    │
  │   ○  ●  Codex CLI      codex  │
  └───────────────────────────────┘
   ↑/↓ move   space toggle   a toggle-all   enter confirm   q cancel
```

- <kbd>space</kbd> toggles the row under the cursor.
- <kbd>a</kbd> toggles all.
- <kbd>enter</kbd> confirms. If nothing is selected, it selects the row under the cursor and moves on.
- <kbd>q</kbd> / <kbd>ctrl+c</kbd> aborts.

Passing `--agent claude,codex` (comma-separated, no spaces) skips the picker entirely. Unknown agent keys produce an error.

## Dry runs

Always dry-run first. Two flavours:

### `--dry-run`

Grouped summary, one row per `(agent, project)`:

```
AGENT    PROJECT                                        FILES  OLDEST            NEWEST
claude   /Users/me/projects/old-app                     3      2025-11-14 18:22  2025-12-02 09:41
codex    /Users/me/projects/side-quest                  7      2025-10-03 21:15  2025-12-28 14:07
…
Total: 26 session(s) across 17 project group(s) — 3.9 MiB on disk.
```

### `--dry-run-verbose`

One row per file, with the reason tags (`orphaned`, `old`, or both):

```
AGENT    LAST ACTIVITY      REASON   PROJECT                   FILE
claude   2025-11-14 18:22   old      /Users/me/old-app         abc123.jsonl
claude   2025-12-01 09:41   old+orph /Users/me/deleted-project def456.jsonl
…
Total: 26 session(s) — 3.9 MiB on disk.
```

The two flags are mutually exclusive.

## Confirmation and safety

Real runs print the summary first, then a red disclaimer box, then a `y/N` prompt. Answering anything other than `y`/`yes` aborts.

The disclaimer can be skipped with `--yes` for scripted runs.

### What's safe

- **Active sessions** touched in the last 5 minutes are never deleted — the originating agent might still be writing.
- **Sub-agent transcripts** are skipped to avoid breaking their parent's file.
- A **path guard** refuses to delete anything outside the known agent roots (`~/.claude/projects`, `~/.pi/agent/sessions`, `~/.codex/sessions`).
- **Codex index**: the per-session name index at `~/.codex/session_index.jsonl` is rewritten atomically (temp file + rename) so no dangling names remain.
- **Claude Desktop sidecars**: when a CLI JSONL is deleted, the matching desktop metadata JSON (`~/Library/Application Support/Claude/claude-code-sessions/local_*.json`) is cleaned up alongside.
- **Empty project folders** left behind after deletions are removed — but never the agent roots themselves.

## Supported agents

- **claude** — including Claude Desktop sidecars
- **pi**
- **codex** — including the session-name index

Not supported:

- **Amp** — its local files are re-synced from the remote. Deleting them just triggers a re-download on the next sync.
- **Cursor** and **OpenCode** — sessions live inside third-party SQLite databases. Deletion would require mutating those databases, which is deferred to a future version.

## Examples

```bash
# Delete anything idle for more than a quarter
lazyagent prune --days 90

# Kill orphans across every supported agent, but preview first
lazyagent prune --orphaned --dry-run-verbose

# Scheduled weekly sweep (script-safe)
lazyagent prune --days 30 --orphaned --agent claude,codex,pi --yes

# Target a single noisy agent
lazyagent prune --agent codex --days 14

# See per-file details before committing
lazyagent prune --days 30 --dry-run-verbose
lazyagent prune --days 30                          # the real run
```

See [Recipes](../usage/recipes.md) for automation tips, including cron examples.

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success (including "nothing to do") |
| `1`  | At least one deletion failed |
| `2`  | Invalid flags or combinations |
