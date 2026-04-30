---
title: "How it works"
description: "lazyagent is purely observational — it reads the session data each agent already writes and derives everything from there."
sidebar:
  order: 1
---

lazyagent doesn't wrap, inject, or modify any agent. It reads whatever each agent already writes to disk — JSONL transcripts, SQLite databases, thread JSON files — and reconstructs state from there. Your agents stay on their own paths; lazyagent is a listener.

## The pipeline

```
  Agent CLIs / IDEs      Session files on disk           lazyagent
  ─────────────────     ───────────────────────       ──────────────
  claude, codex, …  →   ~/.claude/projects/…     →    SessionProvider
  pi, amp, …            ~/.codex/sessions/…            ↓
  cursor, opencode      …state.vscdb, ….db             Session model
                                                        ↓
                                                       TUI / GUI / API
```

Each agent has a **provider** that knows where its session data lives and how to parse it into a common `Session` struct. Providers can be file-watched (fsnotify) or polled (for SQLite-backed agents), and new sessions are merged into the shared view as they appear.

## The shared core

Everything useful — the activity state machine, the file watcher, session caching, cost estimation, configuration — lives in one place (`internal/core`). The three interfaces (TUI, GUI, API) and the maintenance subcommands (`prune`, `compact`, `limits`) all import it, so there's no behavioral drift between them.

Sessions are cached by file path + mtime + size, so subsequent scans only re-parse what changed. For large JSONL transcripts the parser resumes from the last byte offset rather than re-reading the whole file.

## Activity inference

lazyagent classifies each session into a state (`idle`, `thinking`, `writing`, `running`, …) by looking at the last few entries in the transcript: which tool fired, whether its output has arrived, how long ago the last entry was. The full set of states is documented in [Activity states](activity-states.md).

## What lazyagent never does

- It doesn't talk to any LLM. The one outbound network call lazyagent ever makes is `lazyagent limits --agent claude`, which queries Anthropic's `/api/oauth/usage` for the user's own rate-limit numbers — and only when explicitly invoked. Everything else (monitoring, prune, compact, Codex limits) is purely local.
- It doesn't interrupt or control agents. You can't kill a session from lazyagent; it only watches.
- It doesn't move or copy session files — except when you explicitly run `prune` or `compact`, which operate on the same files the agents read.
- It doesn't send telemetry. No analytics, no crash reporter, no phone-home.

## What this implies

Because every piece of state comes from a file on disk:

- Closing lazyagent never disrupts a running agent session.
- Multiple lazyagent processes (e.g. GUI + TUI on the same machine) are always consistent — they read the same files.
- Moving a session folder (or deleting a project directory) is reflected on the next scan. This is also how the `--orphaned` filter in `prune` works.
