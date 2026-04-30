---
title: "Roadmap"
description: "Shipped features per version, plus what's under consideration for the future."
sidebar:
  order: 5
---

## v0.1 — Core TUI

- ✅ Discover all Claude Code sessions from `~/.claude/projects/`
- ✅ Parse JSONL to determine session status
- ✅ Detect worktrees
- ✅ Show tool history
- ✅ FSEvents-based file watcher with debouncing
- ✅ Fallback 30 s polling

## v0.2 — Richer session info

- ✅ Conversation preview (last 5 messages)
- ✅ Last file written with age
- ✅ Filter by activity type
- ✅ Search by project path
- ✅ Time window control
- ✅ Color-coded activity states
- ✅ Memory-efficient single-pass JSONL parsing
- ✅ Activity sparkline graph
- ✅ Token usage and cost estimation
- ✅ Animated braille spinner
- ✅ Open CWD in editor (<kbd>o</kbd> key)
- ✅ Rename sessions (<kbd>r</kbd> key)
- ⬜ Display file diff for last written file

## v0.3 — macOS menu bar app

- ✅ Core library extraction
- ✅ Shared config system
- ✅ Wails v3 + Svelte 5 + Tailwind 4 frontend
- ✅ System tray with attached panel
- ✅ Real-time session updates
- ✅ SVG sparkline, activity badges
- ✅ Keyboard shortcuts
- ✅ Open in editor
- ⬜ Dynamic tray icon (active count)
- ⬜ macOS notifications
- ⬜ Launch at Login
- ⬜ Code signing & notarization
- ⬜ DMG distribution
- ⬜ Homebrew cask

## v0.4 — HTTP API

- ✅ REST API server
- ✅ Session list, detail, stats, config endpoints
- ✅ Server-Sent Events (SSE)
- ✅ Interactive API playground
- ✅ Default port with fallback (7421–7431)
- ✅ Custom bind address
- ✅ Combinable with TUI and GUI
- ✅ Session rename endpoints

## v0.5 — Multi-agent support

- ✅ pi coding agent session discovery (`~/.pi/agent/sessions/`)
- ✅ Pi JSONL parser (tree-structured format → shared Session struct)
- ✅ `--agent` flag (`claude`, `cursor`, `codex`, `amp`, `pi`, `opencode`, `all`)
- ✅ MultiProvider merging sessions from multiple agents
- ✅ Agent type indicator (π prefix in list, Agent row in detail)
- ✅ Pi tool name normalization (snake_case → PascalCase)
- ✅ Multi-directory file watcher
- ✅ Cost estimation for Gemini and GPT model families
- ✅ Claude Code Desktop support (title, permissions, source badge)
- ✅ Shared session types extracted to `internal/model`

## v0.6 — OpenCode support

- ✅ OpenCode session discovery from SQLite
- ✅ `--agent opencode` flag
- ✅ Polling-based refresh (5 s interval)
- ✅ Tool name normalization and activity mapping
- ✅ Subagent detection via `parent_id`

## v0.7 — Cursor support

- ✅ Cursor session discovery from `state.vscdb`
- ✅ `--agent cursor` flag
- ✅ WAL-based cache invalidation for real-time updates
- ✅ CWD inference from file URIs
- ✅ Cursor tool name normalization
- ✅ Open in Cursor IDE (with fallback)
- ✅ Per-agent enable/disable in config

## v0.7.9 — Codex support

- ✅ Codex CLI session discovery from `~/.codex/sessions/`
- ✅ Codex JSONL parser with incremental reading
- ✅ `--agent codex` flag
- ✅ Session name index support
- ✅ X prefix in session list

## v0.7.10 — Remote control & performance

- ✅ Extract and display remote control URL from Claude Code sessions
- ✅ Remote URL shown in TUI detail, GUI detail panel, and REST API
- ✅ Lazy JSONL message parsing (skip expensive deserialization for non-user/assistant entries)
- ✅ Parallel session discovery for Codex and Pi providers

## v0.8 — Amp support

- ✅ Amp CLI session discovery from `~/.local/share/amp/threads/*.json`
- ✅ Amp thread JSON parser with incremental parsing
- ✅ `--agent amp` flag
- ✅ Parallel session discovery for Amp provider
- ✅ A prefix in session list

## v0.8.1 — TUI themes

- ✅ Dark and light theme support for the TUI
- ✅ Theme selection via `tui.theme` in config (`"dark"` or `"light"`)
- ✅ All TUI colors driven by theme (panels, activity states, help bar, overlays)

## v0.8.2 — Resume command

- ✅ Resume command builder for all supported agents
- ✅ Cross-platform clipboard support (macOS, Wayland, X11)
- ✅ <kbd>c</kbd> key in TUI to copy resume command to clipboard
- ✅ Resume command shown in GUI session detail with copy button
- ✅ `resume_command` field in REST API session detail response

## v0.8.3 — Amp remote sync

- ✅ Sync remote Amp threads via `amp threads export` for newer Amp versions
- ✅ Throttled sync (every 15 seconds) with smart change detection
- ✅ Direct file writing to avoid pipe buffer truncation

## v0.9 — Maintenance commands

- ✅ `lazyagent prune` — delete chat files older than N days or whose project folder no longer exists
- ✅ Interactive agent picker with colored checkboxes when `--agent` is omitted
- ✅ Dry-run summary and verbose tables with before/after disk sizes
- ✅ Destructive-op disclaimer box before the y/N confirmation
- ✅ `lazyagent compact` — rewrite JSONL sessions in place, truncating bulky tool outputs, thinking blocks, and embedded images
- ✅ Per-agent field-path mutators for Claude Code, pi, and Codex
- ✅ Line-count validation, `.bak` sidecars, preserved file permissions, path guards
- ✅ Shared `chatops` package powering the interactive UI for both subcommands

## v0.9.x — Rate-limit visibility

- ✅ `lazyagent limits` — on-demand snapshot of 5-hour and weekly rate-limit windows
- ✅ Pace indicator comparing actual consumption to a perfectly linear pace (under / on track / over)
- ✅ Claude Code via `/api/oauth/usage` (the same endpoint Claude Code's `/status` uses), token resolved from env / macOS keychain / `~/.claude/.credentials.json`
- ✅ Codex via the latest rollout JSONL under `~/.codex/sessions/` — no network call, fallback to older rollouts when the most recent has no `rate_limits` event yet
- ✅ Honest User-Agent (no Claude Code impersonation), graceful failure on 401/429, disclaimer in `--help` and output

## Future ideas

- ⬜ Outbound webhooks on status changes
- ⬜ Multi-machine support via shared config / remote API
- ⬜ TUI actions: kill session, attach terminal
- ⬜ Session history browser (browse past conversations)
- ⬜ `prune`: per-project y/N/number prompts to cherry-pick which projects get deleted
- ⬜ `prune`: soft delete to a trash area, with restore
- ⬜ `prune` / `compact`: content-based search before selection
