---
title: "Development"
description: "Build targets, dev loop, and requirements for contributing to lazyagent."
sidebar:
  order: 4
---

## Requirements

- **Go 1.25+** — the module targets a recent Go toolchain.
- **Node.js 18+** — required only for the macOS menu bar app frontend (Svelte 5 + Tailwind 4). The TUI and API build without it.
- **macOS** — required for the menu bar app. The TUI and HTTP API are cross-platform.

## Build targets

```bash
# Install frontend deps (first time only)
make install

# Full build: TUI + macOS menu bar + API
make build

# TUI only — no Wails, no Node.js required
make tui

# Quick dev cycle (rebuild + relaunch tray)
make dev

# Clean all artifacts
make clean
```

All binaries land in the repository root.

## Dev loop

For TUI and API work:

```bash
make tui && ./lazyagent --tui --api
```

For tray/frontend work:

```bash
make dev
```

`make dev` rebuilds the embedded frontend, re-runs the Go build, and relaunches the tray process.

## Tests

```bash
go test ./...
```

Every package with behavior under test (`internal/amp`, `claude`, `codex`, `core`, `pi`, `api`) has real fixture files that exercise the JSONL parsers end to end. Keep the fixtures realistic — synthetic JSONL has a way of missing the edge cases that break in production.

## Repository layout

See [Architecture](architecture.md) for the full module map.

## Contributing

Pull requests and issues are welcome at [github.com/illegalstudio/lazyagent](https://github.com/illegalstudio/lazyagent). A few conventions:

- Commit messages follow the conventional-commits style (`feat:`, `fix:`, `refactor:`, `docs:`, `chore:`). Scope to the relevant package when possible, e.g. `feat(compact):`, `fix(amp):`.
- Adding a new agent means adding a provider under `internal/<agent>/` that implements the `SessionProvider` interface from `internal/core/provider.go`. For the maintenance commands to cover it, also add a mutator under `internal/compact/<agent>.go` and deletion hooks under `internal/prune/delete.go`.
- Keep the documentation in `docs/` in the same PR as the behavior change — the site at [lazyagent.dev/docs](https://lazyagent.dev/docs) tracks this repository as upstream.
