---
title: "Session info"
description: "Everything lazyagent surfaces per session and where each field comes from."
sidebar:
  order: 4
---

lazyagent computes a single `Session` record per conversation, regardless of which agent produced it. The record is shown in the detail panel (TUI and GUI), returned by `/api/sessions/{id}`, and reused by session-oriented commands such as `lazyagent sessions`, `prune`, `compact`, and `search`.

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
| Token usage & estimated cost | JSONL + per-model pricing, when the agent records token counters |
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
| Normal and YOLO resume commands | Computed per agent when available |

## Custom names

Every session can be renamed (<kbd>r</kbd> in TUI or GUI, `PUT /api/sessions/{id}/name` in the API). Names persist to `~/.config/lazyagent/session-names.json` and survive restarts. An empty name resets to the default (agent-assigned) label.

## Resume command

Lazyagent builds both normal and YOLO resume commands when the agent exposes a
distinct permissive mode:

| Agent | Normal | YOLO |
|-------|--------|------|
| Claude Code | `claude --resume <id>` | `claude --dangerously-skip-permissions --resume <id>` |
| Codex CLI | `codex resume <id>` | `codex --yolo resume <id>` |
| Amp | `amp threads continue <id>` | `amp --dangerously-allow-all threads continue <id>` |
| pi | `pi --session <id>` | Not available |
| OpenCode | `opencode -s <id>` | `opencode --auto -s <id>` |
| Kilo | `kilo --session=<id>` | `kilo --auto --session=<id>` |
| Cursor | `cursor-agent --resume="<id>"` | `cursor-agent --force --resume="<id>"` |
| Grok | `grok --resume '<id>'` | `grok --yolo --resume '<id>'` |
| Kimi Code | `kimi --resume <id>` | `kimi --yolo --resume <id>` |

`YOLO` is lazyagent's common label for the most permissive mode exposed by
each CLI. The exact guarantees are tool-specific: flags such as `--auto` and
`--force` do not necessarily have the same approval or sandbox semantics as a
full permission bypass, and configured deny rules may still apply. pi has no
separate toggle, so lazyagent exposes only its normal resume command.

In the TUI, <kbd>c</kbd> copies the normal command and <kbd>C</kbd> copies the
YOLO command. The GUI exposes open and copy actions for both variants. The API
returns `resume_command` and `resume_command_yolo` in the session detail.

## Cost estimation

Cost is derived from the token counters already present in the transcript, multiplied by the per-model price list baked into lazyagent. Supported model families include Claude (Opus, Sonnet, Haiku), GPT (4o, 4.1, o1, o3), and Gemini. Unknown models show tokens but no cost. Grok sessions show no per-session token or cost figures because Grok's local transcript data does not expose an input/output/cache token split.

Costs are estimates — the authoritative number is always your provider's billing console.
