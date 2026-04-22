---
title: "Activity states"
description: "The color-coded state machine lazyagent uses to classify each session at a glance."
sidebar:
  order: 3
---

lazyagent tags every session with an activity state inferred from the last few entries in the transcript. States show up as color-coded labels in the TUI list, the GUI panel, and the `activity` field of the HTTP API.

## The states

| State | Meaning |
|-------|---------|
| **idle** | Session file exists but nothing has been written for a while |
| **waiting** | The agent replied and is waiting for your next input. A 10-second grace period avoids flapping while the model finishes streaming |
| **thinking** | The agent is generating a response |
| **compacting** | Context compaction is in progress (agent-driven summarization of earlier turns) |
| **reading** | Reading files via `Read`, `Glob`, `Grep`, or the agent's equivalent |
| **writing** | Writing files via `Edit`, `Write`, `NotebookEdit`, or equivalent |
| **running** | Executing shell commands (Bash, shell, exec) |
| **searching** | Searching the codebase (grep, semantic search) |
| **browsing** | Web browsing / fetching URLs |
| **spawning** | Delegating to a sub-agent |

Sub-agent transcripts themselves inherit their parent's project CWD and are distinguished by an `Is sidechain` flag in the session detail.

## How the state is derived

Each provider parses the last N JSONL entries (or SQLite rows) of a session and classifies:

1. **Did the last entry come from the agent or the user?** If from the agent, state is `waiting` (unless the user already replied).
2. **Is there an unfinished tool call?** If so, the state reflects the tool type (`reading`, `writing`, `running`, …) normalized across agents.
3. **When was the last activity?** Older than the configured window → `idle`.

The mapping from tool name → state is normalized per agent. For example, Cursor's `Read_file_v2` and Claude's `Read` both map to `reading`; Codex's `apply_patch` and Claude's `Write` both map to `writing`.

## The time window

Sessions older than `window_minutes` (default **30**) are hidden from the default view but still exist on disk — change the value in [Configuration](../reference/configuration.md) or temporarily widen it with <kbd>+</kbd> / <kbd>-</kbd> in the TUI and GUI.

## Filtering

Press <kbd>f</kbd> in the TUI or GUI to cycle through state filters (`all`, `active`, `waiting`, `thinking`, …). In the HTTP API, pass `?filter=thinking` on `/api/sessions`. The default filter can be set via [`default_filter`](../reference/configuration.md) in the config.
