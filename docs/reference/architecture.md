---
title: "Architecture"
description: "Module map of the lazyagent codebase, from the shared core to the per-interface wiring."
sidebar:
  order: 3
---

lazyagent is a single Go binary with an optional Svelte 5 frontend embedded for the macOS menu bar app. Everything shares one core package; the three interfaces and the two maintenance commands are thin consumers of it.

## Module map

```
lazyagent/
├── main.go                     # Entry point: --tui / --gui / --api / --agent + prune / compact subcommands
├── internal/
│   ├── core/                   # Shared: watcher, activity, session, config
│   │   └── provider.go         # SessionProvider interface + Multi/Live/Pi/OpenCode/Cursor/Codex/Amp providers
│   ├── model/                  # Shared types (Session, ToolCall, DesktopMeta, …)
│   ├── amp/                    # Amp CLI thread parsing and session discovery
│   ├── claude/                 # Claude Code JSONL parsing, Desktop sidecar, session discovery
│   ├── codex/                  # Codex CLI JSONL parsing and session discovery
│   ├── cursor/                 # Cursor IDE session discovery from state.vscdb (SQLite)
│   ├── pi/                     # pi coding agent JSONL parsing, session discovery
│   ├── opencode/               # OpenCode SQLite parsing, session discovery
│   ├── api/                    # HTTP API server (REST + SSE)
│   ├── ui/                     # TUI rendering (bubbletea + lipgloss, dark/light themes)
│   ├── tray/                   # macOS menu bar (Wails v3, build-tagged)
│   ├── chatops/                # Shared CLI helpers: agent picker, tables, notices, safety
│   ├── prune/                  # `lazyagent prune` — delete old or orphaned chat files
│   ├── compact/                # `lazyagent compact` — truncate oversized JSONL payloads
│   ├── demo/                   # Fake session data for screenshots
│   └── assets/                 # Embedded frontend dist (go:embed)
├── frontend/                   # Svelte 5 + Tailwind 4 (menu bar UI)
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

The shared engine: session provider interface, file watcher (fsnotify-based, with polling fallback), activity-state classifier, cost estimation, config loading. Every other package imports it.

### `internal/model`

Pure types — `Session`, `ToolCall`, `ConversationMessage`, `DesktopMeta`, and the `SessionCache` that backs incremental JSONL parsing. No behavior, no imports beyond `time` and `sync`.

### Per-agent providers (`internal/amp`, `claude`, `codex`, `cursor`, `pi`, `opencode`)

Each owns the on-disk layout and parsing for its agent. They expose discovery functions that return `[]*model.Session`, integrated via the `SessionProvider` interface in `core/provider.go`.

### `internal/ui`, `internal/tray`, `internal/api`

The three interfaces. Each consumes `SessionProvider.DiscoverSessions()` on a loop and renders the result. They're decoupled enough that `--tui --gui --api` runs them all concurrently without coordination overhead.

### `internal/chatops`

A small toolbox of CLI helpers shared by the two maintenance commands: the interactive agent picker, tables, the destructive-operation disclaimer, the "all clean" zen box, `y/N` confirmation, `EnsureWithin` path guard, and `HumanBytes` formatter.

### `internal/prune`, `internal/compact`

The two maintenance commands. Both are thin orchestrators:

- **`prune`** discovers candidates via the standard providers, applies age/orphan filters, and deletes files. Per-agent deletion handles sidecar metadata (Claude Desktop) and name-index rewrites (Codex).
- **`compact`** reads each JSONL line, applies a per-agent `lineMutator` that truncates oversized fields, and rewrites atomically with line-count validation.

## Activity state machine

Each provider produces sessions with a `status` enum derived from the last few entries of the transcript. The mapping is agent-specific (Codex `function_call_output` vs Claude `tool_result` vs pi `toolCall` blocks) but the output vocabulary is shared, so the UI can treat every session uniformly.

## File watcher

`internal/core` uses `fsnotify` when the agent writes to a real filesystem. For agents that write to WAL-mode SQLite (Cursor, OpenCode) the provider polls on a ~3 s interval instead — file events are unreliable for WAL journals.

Events are **debounced** at 200 ms so a burst of writes during a tool call doesn't swamp the UI thread.

## Build layout

- `make tui` — builds the TUI binary only (no Node.js required, no Wails, no embedded frontend).
- `make build` — builds the full binary including the macOS menu bar app. Requires Node.js 18+ for the Svelte build.
- `make dev` — dev cycle: rebuild the binary and relaunch the tray app.

## Cost estimation

Per-model pricing tables live in `internal/core/costs.go` (Claude, GPT, Gemini families). Estimates are derived from token counters already present in the transcript — lazyagent never calls any LLM.
