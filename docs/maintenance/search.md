---
title: "Search chat transcripts"
description: "Full-text search over local agent transcripts, with highlighted snippets, an incremental SQLite FTS5 index, and an interactive resume picker."
sidebar:
  order: 4
---

`lazyagent search` finds messages across every chat transcript on your machine. Run a query, get a ranked list of sessions with highlighted snippets, optionally pick one and resume it directly with the originating agent.

It works with the agents that store transcripts as plain text files: **Claude Code** (CLI and Desktop), **Codex CLI**, **pi**, and **Amp**. Cursor and OpenCode are excluded because they keep history inside third-party SQLite databases that lazyagent doesn't index.

## Synopsis

```
lazyagent search [QUERY] [--agent LIST]
                 [--limit N] [--snippets N]
                 [--reindex]
```

`QUERY` is the search expression. If omitted in an interactive terminal, lazyagent prompts for it; piped input is read from stdin instead.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent LIST` | string | `all` | Comma-separated subset: `claude,codex,pi,amp`, or `all` |
| `--limit N` | int | `20` | Maximum chat sessions to show |
| `--snippets N` | int | `2` | Maximum snippet lines per session |
| `--reindex` | bool | `false` | Drop the local index and rebuild it before searching |
| `--db PATH` | string | *unset* | Override the index file location (testing only) |

## Quick reference

```bash
lazyagent search "race condition"               # all agents
lazyagent search --agent codex "parser bug"     # one agent
lazyagent search --agent claude,codex "auth"    # subset
lazyagent search --limit 5 "regex"              # tighter result list
lazyagent search --snippets 4 "OAuth"           # more context per session
lazyagent search --reindex "config"             # force a full rebuild
echo "deadlock" | lazyagent search              # query from stdin
```

## How it works

The index lives at `<user-cache>/lazyagent/search.sqlite` (typically `~/.cache/lazyagent/search.sqlite` on Linux, `~/Library/Caches/lazyagent/search.sqlite` on macOS). It's a SQLite database with two tables:

- `sources` — one row per transcript file, keyed by `(agent, source_id)`, with `mtime_ns` and `size` for invalidation
- `chunks` — a virtual FTS5 table indexing every user/assistant text block, tokenized with `unicode61`

On every run, lazyagent walks the supported agents' session roots and extracts text content from each transcript. A file is re-indexed only if its `(mtime_ns, size)` changed since the last run, so warm runs are fast. Sessions that are no longer present on disk get pruned from the index automatically.

Ranking uses FTS5's built-in `bm25(chunks)` — the most relevant matches appear first. Whitespace inside indexed messages is collapsed to single spaces before storage so multi-line code blocks search the same as flowing prose.

## Output

For each matching session lazyagent prints a header (agent, project path, session name) and up to `--snippets` highlighted snippets — pieces of the conversation that contain the query terms. The agent is shown with its single-letter prefix (C, X, π, A) for visual scanning across mixed result sets.

Pipe-safe behavior: when stdout is not a terminal the interactive resume prompt is skipped, so `lazyagent search query | grep ...` and `| jq` work cleanly. Headers and snippets still go to stdout; warnings (e.g. "indexing failed for X session: …") go to stderr.

## Interactive resume

After printing results in an interactive terminal, lazyagent shows:

```
Open a chat? Enter result #, or press Enter to quit:
```

Type a 1-based result number to open that session in the originating agent. lazyagent runs the right resume command for you:

| Agent | Resume command |
|-------|----------------|
| claude | `claude --resume <session-id>` |
| codex | `codex resume <session-id>` |
| amp | `amp threads continue <session-id>` |
| pi | `pi --session <session-id>` |

The command runs from the session's original CWD when that directory still exists, otherwise from the current shell directory. Pressing <kbd>Enter</kbd> on an empty line exits without opening anything.

## Index management

The index updates *incrementally* on every run — there's normally no reason to think about it. Two situations call for a manual rebuild:

- After a major lazyagent upgrade if the index schema changed
- If the index gets corrupted (rare) and queries return errors

```bash
lazyagent search --reindex "anything"
```

`--reindex` drops every row in the index, walks every supported agent from scratch, and re-tokenizes every transcript. On a year of accumulated history this can take 30 seconds or so; subsequent runs are back to milliseconds.

To wipe the index completely, just delete the file:

```bash
rm ~/.cache/lazyagent/search.sqlite                   # Linux
rm ~/Library/Caches/lazyagent/search.sqlite           # macOS
```

The next `lazyagent search` invocation will rebuild it.

## Supported agents

- **claude** — Claude Code CLI and Desktop (`~/.claude/projects/`)
- **codex** — Codex CLI (`~/.codex/sessions/`)
- **pi** — pi coding agent (`~/.pi/agent/sessions/`)
- **amp** — Amp CLI (`~/.local/share/amp/threads/`)

Not supported:

- **Cursor** and **OpenCode** — sessions live inside third-party SQLite databases. Reading from them at search time would require coupling to their schema; for now they're left to their own in-app search.

## Examples

```bash
# Quick lookup with the default 20-result limit
lazyagent search "websocket reconnect"

# Narrow to one agent and pull more context per session
lazyagent search --agent claude --snippets 5 "OAuth flow"

# Pipe results to jq-style processing (no resume prompt)
lazyagent search "TODO" | grep -i security

# Read query from stdin
fzf --print-query | lazyagent search

# Force a full rebuild before searching (after a major version bump)
lazyagent search --reindex "anything"
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Search ran successfully (including "no matches") |
| `1`  | Indexing or query error — details on stderr |
| `2`  | Invalid flags, empty query, or invalid resume selection |

## See also

- [`lazyagent prune`](prune.md) — delete entire chat files (destructive, complementary)
- [`lazyagent compact`](compact.md) — shrink chat files in place (destructive, complementary)
- [`lazyagent limits`](limits.md) — show 5-hour and weekly rate-limit usage
