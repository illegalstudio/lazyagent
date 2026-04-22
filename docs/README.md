---
title: "lazyagent Documentation"
description: "A terminal UI, macOS menu bar app, and HTTP API for monitoring every coding agent on your machine — plus maintenance commands to keep their transcripts under control."
sidebar:
  order: 0
---

lazyagent watches session data from coding agents — [Claude Code](https://claude.ai/code) (CLI and Desktop), [Cursor](https://cursor.com/), [Codex](https://developers.openai.com/codex/), [Amp](https://ampcode.com/), [pi](https://github.com/badlogic/pi-mono), and [OpenCode](https://opencode.ai/) — and shows what each one is doing in real time. No modifications to any agent are needed; it's purely observational.

Three interfaces ship in a single binary: a terminal UI, a macOS menu bar app, and an HTTP API. They share the same engine and can all run at once. Two maintenance subcommands, `prune` and `compact`, clean up old or oversized transcripts.

## Getting Started

- [Installation](getting-started/installation.md) — Homebrew, Go, or build from source
- [Quickstart](getting-started/quickstart.md) — first launch, flags, and combining interfaces

## Concepts

- [How it works](concepts/how-it-works.md) — the observational model and the shared core
- [Supported agents](concepts/supported-agents.md) — paths, prefixes, and per-agent quirks
- [Activity states](concepts/activity-states.md) — the state machine behind the color-coded labels
- [Session info](concepts/session-info.md) — every field lazyagent surfaces, and where it comes from

## Interfaces

- [Terminal UI](interfaces/terminal-ui.md) — the default, bubbletea-powered TUI
- [macOS GUI](interfaces/macos-gui.md) — the detachable menu bar panel
- [HTTP API](interfaces/http-api.md) — REST + Server-Sent Events, with the interactive playground

## Maintenance

- [Prune old sessions](maintenance/prune.md) — delete chat files by age or orphaned-project filter
- [Compact session files](maintenance/compact.md) — truncate bulky tool outputs and thinking blocks in place

## Reference

- [Editor support](reference/editor-support.md) — how `$VISUAL` / `$EDITOR` are resolved
- [Configuration](reference/configuration.md) — `~/.config/lazyagent/config.json` field by field
- [Architecture](reference/architecture.md) — module map of the codebase
- [Development](reference/development.md) — build targets and dependencies
- [Roadmap](reference/roadmap.md) — shipped features and what's next
