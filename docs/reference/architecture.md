---
title: "Architecture"
description: "Module map of the lazyagent codebase, from the shared core to the per-interface wiring."
sidebar:
  order: 3
---

lazyagent is a single Go binary with an optional Svelte 5 frontend embedded for the macOS and Linux desktop app. Everything shares one core package; the three interfaces and the command packages are thin consumers of it.

## Module map

```
lazyagent/
├── main.go                     # Entry point: --tui / --gui / --api / --agent + subcommands
├── internal/
│   ├── core/                   # Shared: watcher, activity, session, config
│   │   └── provider.go         # SessionProvider interface + Multi/Live/Pi/OpenCode/Kilo/Cursor/Codex/Amp/Grok/Kimi providers
│   ├── model/                  # Shared session types + incremental/persistent cache state
│   ├── diskcache/              # Atomic, permission-safe JSON cache I/O
│   ├── amp/                    # Amp CLI thread parsing and session discovery
│   ├── claude/                 # Claude Code JSONL parsing, Desktop sidecar, session discovery
│   ├── codex/                  # Codex CLI JSONL parsing and session discovery
│   ├── cursor/                 # Cursor IDE session discovery from state.vscdb (SQLite)
│   ├── grok/                   # Grok CLI session-directory parsing and discovery
│   ├── kimi/                   # Kimi Code CLI session-directory parsing and discovery
│   ├── kilo/                   # Kilo SQLite discovery wrapper
│   ├── pi/                     # pi coding agent JSONL parsing, session discovery
│   ├── opencode/               # OpenCode SQLite discovery wrapper
│   ├── opencodefamily/         # Shared OpenCode/Kilo SQLite parser
│   ├── api/                    # HTTP API server (REST + SSE)
│   ├── apiauth/                # Bearer-token derivation (PBKDF2) + auth middleware
│   ├── webhook/                # Outbound webhook dispatcher (EventBus → filtered HTTP POST)
│   ├── ui/                     # TUI rendering (bubbletea + lipgloss, dark/light themes)
│   ├── tray/                   # macOS/Linux desktop app (Wails v3, build-tagged)
│   ├── chatops/                # Shared CLI helpers: agent picker, tables, notices, safety
│   ├── prune/                  # `lazyagent prune` — delete old or orphaned chat files
│   ├── compact/                # `lazyagent compact` — truncate oversized session payloads
│   ├── search/                 # `lazyagent search` — local transcript full-text search
│   ├── sessions/               # `lazyagent sessions` — directory-scoped listing and picker
│   ├── limits/                 # `lazyagent limits` — rate-limit / billing snapshots
│   ├── demo/                   # Fake session data for screenshots
│   └── assets/                 # Embedded frontend dist (go:embed)
├── frontend/                   # Svelte 5 + Tailwind 4 desktop GUI
│   ├── src/
│   │   ├── App.svelte
│   │   ├── lib/                # SessionList, SessionDetail, Sparkline
│   │   └── bindings/           # Auto-generated Wails TS bindings
│   └── app.css                 # Tailwind 4 @theme (Catppuccin Mocha)
├── docs/                       # Documentation (source of truth, synced into lazyagent.dev)
└── Makefile
```

## Key packages

### `internal/core`

The shared engine: session provider interface, file watcher (fsnotify-based, with polling fallback), activity-state classifier, cost estimation, config loading, and a typed in-process `EventBus` that publishes activity-state transitions (consumed by `internal/webhook`). Most feature packages build on it.

### `internal/model`, `internal/diskcache`

`internal/model` owns `Session`, `ToolCall`, `ConversationMessage`,
`DesktopMeta`, and the `SessionCache` that backs incremental JSONL parsing.
The cache can be saved and restored between processes; `internal/diskcache`
provides the shared atomic JSON read/write mechanics and enforces restrictive
file and directory permissions.

### Per-agent providers (`internal/amp`, `claude`, `codex`, `cursor`, `grok`, `kilo`, `kimi`, `pi`, `opencode`)

Each owns the on-disk layout and parsing for its agent. They expose discovery functions integrated through `SessionProvider` in `core/provider.go`; optional capability interfaces add directory-scoped discovery, streaming batches, and persistent caches where the provider can implement them efficiently.

OpenCode and Kilo share the `internal/opencodefamily` parser because their local SQLite schemas are compatible; their provider packages only declare data directories, database names, and agent keys.

Cursor reads conversation headers from the inline composer list or, when it is empty, absent, or null, from `composerConvHeaders` chunks. Chunks are fetched with one indexed key-range query. If a chunk is missing or unreadable, the readable prefix still supplies messages and activity state; the partial session is not cached and is retried on the next poll.

### `internal/ui`, `internal/tray`, `internal/api`

The three interfaces consume a `SessionManager`, which coordinates discovery,
filtering, activity updates, watchers, and persistent provider caches. The TUI
and GUI use streaming discovery for their first load so sessions appear as
providers finish; the API performs a synchronous first load so it never serves
a partial initial snapshot. Later updates use the same watcher/polling paths.
The interfaces remain decoupled enough that `--tui --gui --api` runs them all
concurrently.

### `internal/sessions`

Implements `lazyagent sessions`: directory matching, deterministic sorting,
the progressive Bubble Tea picker, stable JSON output, resume/copy behavior,
and the one-shot command's cache lifecycle. Directory matching helpers live
in `internal/core` so the HTTP API's `GET /api/sessions?dir=` filter uses the
same semantics.

### `internal/chatops`

A small toolbox of CLI helpers shared by session and maintenance commands: the interactive agent picker, tables, notices, the destructive-operation disclaimer, the "all clean" zen box, `y/N` confirmation, `EnsureWithin` path guard, and `HumanBytes` formatter.

### `internal/prune`, `internal/compact`

The destructive maintenance commands. Both are thin orchestrators:

- **`prune`** discovers candidates via the standard providers, applies age/orphan filters, and deletes files. Per-agent deletion handles sidecar metadata (Claude Desktop) and name-index rewrites (Codex).
- **`compact`** rewrites JSONL transcripts plus Grok/Kimi session directories, applies per-agent truncation rules to oversized fields, and rewrites atomically with validation.

## Activity state machine

Each provider produces sessions with a `status` enum derived from the last few entries of the transcript. The mapping is agent-specific (Codex `function_call_output` vs Claude `tool_result` vs pi `toolCall` blocks) but the output vocabulary is shared, so the UI can treat every session uniformly.

## File watcher

`internal/core` uses `fsnotify` when the agent writes to a real filesystem. For agents that write to WAL-mode SQLite (Cursor, OpenCode, Kilo) the provider polls on a ~3 s interval instead — file events are unreliable for WAL journals.

Events are **debounced** at 200 ms so a burst of writes during a tool call doesn't swamp the UI thread.

## Build layout

- `make tui` — builds the TUI binary only (no Node.js required, no Wails, no embedded frontend).
- `make build` — builds the full binary including desktop support. Requires Node.js 18+ for the Svelte build and the platform's Wails libraries.
- `make linux-packages VERSION=x.y.z` — builds DEB, RPM, Arch, and AppImage desktop artifacts.
- `make dev` — dev cycle: rebuild the binary and relaunch the tray app.

## Cost estimation

Per-model pricing tables live in `internal/core/costs.go` (Claude, GPT, Gemini families). Estimates are derived from token counters already present in the transcript — lazyagent never calls any LLM. Grok sessions report no per-session cost because their local files do not expose the token split needed for that calculation.
