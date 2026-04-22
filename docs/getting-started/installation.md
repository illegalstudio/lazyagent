---
title: "Installation"
description: "How to install lazyagent via Homebrew, go install, or a source build."
sidebar:
  order: 1
---

lazyagent ships as a single binary. The terminal UI works on any platform; the macOS menu bar app is built in when the frontend is compiled alongside the binary.

## Homebrew

The recommended way on macOS and Linux:

```bash
brew tap illegalstudio/tap
brew install lazyagent
```

This installs the full binary — TUI, menu bar app (macOS), and HTTP API all included.

## Go (TUI only)

If you only need the terminal interface and already have a Go toolchain:

```bash
go install github.com/illegalstudio/lazyagent@latest
```

The resulting binary doesn't include the Wails-powered macOS menu bar app — the GUI requires a Node.js build step, which `go install` doesn't perform. Use Homebrew or a source build if you want the tray.

## Build from source

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent

# TUI only — no Wails, no Node.js required
make tui

# Full build with menu bar app (requires Node.js 18+)
make install   # npm install, only the first time
make build
```

The resulting binary is written to the repository root.

## Verify

```bash
lazyagent --version
```

lazyagent reads its configuration from `~/.config/lazyagent/config.json`, creating it with defaults on first run. See [Configuration](../reference/configuration.md) for the field-by-field reference.
