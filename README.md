# lazyagent

![GitHub Downloads](https://img.shields.io/github/downloads/illegalstudio/lazyagent/total?v=1)
![License: MIT](https://img.shields.io/badge/license-MIT-blue)
[![Product Hunt](https://img.shields.io/badge/Product%20Hunt-Launch-ff6154?logo=producthunt&logoColor=white)](https://www.producthunt.com/products/lazy-agent)

**A terminal UI, macOS menu bar app, and HTTP API for monitoring all your coding agents from a single place.**

Watch sessions from [Claude Code](https://claude.ai/code), [Cursor](https://cursor.com/), [Codex](https://developers.openai.com/codex/), [Amp](https://ampcode.com/), [pi](https://github.com/badlogic/pi-mono), and [OpenCode](https://opencode.ai/) — no lock-in, no server, purely observational.

Inspired by [lazygit](https://github.com/jesseduffield/lazygit), [lazyworktree](https://github.com/chmouel/lazyworktree), and [pixel-agents](https://github.com/pablodelucca/pixel-agents).

> ⭐ If lazyagent is useful to you, consider [starring the repo](https://github.com/illegalstudio/lazyagent) — it helps others discover it!
>
> 💛 Loving it? Consider [becoming a sponsor](https://github.com/sponsors/nahime0) to keep the project alive and growing.

## Why lazyagent?

Unlike other tools, lazyagent doesn't replace your workflow — it watches it. Launch agents wherever you want (terminal, IDE, desktop app), lazyagent just observes. No lock-in, no server, no account required.

### Terminal UI
![lazyagent TUI](assets/tui.png)

### macOS Menu Bar App
![lazyagent macOS tray](assets/tray.png)

### HTTP API
![lazyagent API playground](assets/api.png)

## Install

### Homebrew

```bash
brew tap illegalstudio/tap
brew install lazyagent
```

### Go (TUI only)

```bash
go install github.com/illegalstudio/lazyagent@latest
```

### Build from source

```bash
git clone https://github.com/illegalstudio/lazyagent
cd lazyagent

# TUI only (no Wails/Node.js needed)
make tui

# Full build with menu bar app (requires Node.js for frontend)
make install   # npm install (first time only)
make build
```

## Launch

```
lazyagent                        Launch the terminal UI (monitors all agents)
lazyagent --agent claude         Monitor only Claude Code sessions
lazyagent --api                  Start the HTTP API on 127.0.0.1:7421
lazyagent --gui                  Launch the macOS menu bar app
lazyagent --tui --gui --api      Run everything together
lazyagent prune --days N         Delete chat sessions older than N days
lazyagent compact                Shrink chat files by truncating bulky payloads
lazyagent --help                 Show full help
```

## Documentation

Full documentation — supported agents, activity states, keybindings, configuration, the HTTP API, the `prune` and `compact` maintenance commands, and architecture — lives at:

- **[lazyagent.dev/docs](https://lazyagent.dev/docs)** — rendered website
- [`docs/`](docs/) — Markdown sources in this repository, organized by topic:
  - [Getting started](docs/getting-started/) — install, quickstart
  - [Concepts](docs/concepts/) — how it works, supported agents, activity states, session info
  - [Interfaces](docs/interfaces/) — terminal UI, macOS GUI, HTTP API
  - [Maintenance](docs/maintenance/) — `prune` and `compact` commands
  - [Reference](docs/reference/) — configuration, architecture, development, roadmap

## License

MIT
