---
title: "Session info"
description: "Everything lazyagent surfaces per session and where each field comes from."
sidebar:
  order: 4
---

lazyagent computes a single `Session` record per conversation, regardless of which agent produced it. The record is shown in the detail panel (TUI and GUI), returned by `/api/sessions/{id}`, and used as the input to both maintenance subcommands.

## The fields

| Field | Source |
|-------|--------|
| Session ID | JSONL / SQLite (per agent) |
| Working directory | JSONL / SQLite |
| Git branch | JSONL / SQLite |
| Agent version | JSONL |
| Model used | JSONL |
| Is git worktree | `git rev-parse` at discovery time |
| Main repo path (if worktree) | `git worktree` |
| Message count (user / assistant) | JSONL / SQLite |
| Token usage & estimated cost | JSONL + per-model pricing |
| Activity sparkline (last N minutes) | JSONL entry timestamps |
| Last file written | Tool call inspection |
| Recent conversation (last 5 messages) | JSONL / SQLite |
| Last 20 tools used | JSONL / SQLite |
| Last activity timestamp | JSONL / SQLite |
| Custom session name | `~/.config/lazyagent/session-names.json` |
| Session source (CLI / Desktop) | Claude Desktop metadata |
| Desktop session title | Claude Desktop metadata |
| Permission mode (Desktop) | Claude Desktop metadata |
| Remote control URL | JSONL (Claude `bridge_status` entries) |
| Resume command | Computed per agent |

## Custom names

Every session can be renamed (<kbd>r</kbd> in TUI or GUI, `PUT /api/sessions/{id}/name` in the API). Names persist to `~/.config/lazyagent/session-names.json` and survive restarts. An empty name resets to the default (agent-assigned) label.

## Resume command

For every supported agent, lazyagent builds the exact shell command that would resume the selected session:

- Claude Code: `claude --resume <session-id> --cwd <cwd>`
- Codex CLI: `codex resume <session-id>`
- Amp: `amp thread continue <thread-id>`
- pi: `pi agent resume <session-id>`
- OpenCode: `opencode --session <id>`
- Cursor: opens in Cursor IDE via the `cursor` CLI (if installed)

In the TUI, <kbd>c</kbd> copies the command to the clipboard. The GUI has a copy button next to the command. The API exposes it as `resume_command` on the session detail response.

## Cost estimation

Cost is derived from the token counters already present in the transcript, multiplied by the per-model price list baked into lazyagent. Supported model families include Claude (Opus, Sonnet, Haiku), GPT (4o, 4.1, o1, o3), and Gemini. Unknown models show tokens but no cost.

Costs are estimates — the authoritative number is always your provider's billing console.
