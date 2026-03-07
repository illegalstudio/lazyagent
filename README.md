# lazyagent

![GitHub Downloads](https://img.shields.io/github/downloads/illegalstudio/lazyagent/total)
![License: MIT](https://img.shields.io/badge/license-MIT-blue)

A terminal UI for monitoring all running [Claude Code](https://claude.ai/code) instances on your machine — inspired by [lazygit](https://github.com/jesseduffield/lazygit), [lazyworktree](https://github.com/chmouel/lazyworktree) and [pixel-agents](https://github.com/pablodelucca/pixel-agents).

![lazyagent](assets/screenshot.jpg)

## How it works

lazyagent watches Claude Code's JSONL transcript files (`~/.claude/projects/*/`) to determine what each session is doing. No modifications to Claude Code are needed — it's purely observational.

From the JSONL stream it detects activity states with color-coded labels:

- **idle** — Session file exists but no recent activity
- **waiting** — Claude responded, waiting for your input (with 10s grace period to avoid false positives)
- **thinking** — Claude is generating a response
- **compacting** — Context compaction in progress
- **reading** / **writing** / **running** / **searching** / **browsing** / **spawning** — Tool-specific activities

It also surfaces:

| Info | Source |
|------|--------|
| Working directory | JSONL |
| Git branch | JSONL |
| Claude version | JSONL |
| Model used | JSONL |
| Is git worktree | git rev-parse |
| Main repo path (if worktree) | git worktree |
| Message count (user/assistant) | JSONL |
| Token usage & estimated cost | JSONL |
| Activity sparkline (last N minutes) | JSONL |
| Last file written | JSONL |
| Recent conversation (last 5 messages) | JSONL |
| Last 20 tools used | JSONL |
| Last activity timestamp | JSONL |

## Install

### Homebrew

```bash
brew tap illegalstudio/tap
brew install lazyagent
```

### Go

```bash
go install github.com/nahime0/lazyagent@latest
```

### Build from source

```bash
git clone https://github.com/nahime0/lazyagent
cd lazyagent
go build -o lazyagent .
```

### macOS note

On first launch, macOS may block the binary. Go to **System Settings → Privacy & Security**, scroll down and click **Allow Anyway**, then run it again.

## Usage

```
lazyagent
```

### Keybindings

| Key | Action |
|-----|--------|
| `↑` / `k` | Move up / scroll up (detail) |
| `↓` / `j` | Move down / scroll down (detail) |
| `tab` | Switch focus between panels |
| `+` / `-` | Adjust time window (±10 minutes) |
| `f` | Cycle activity filter |
| `/` | Search sessions by project path |
| `o` | Open session CWD in editor (see below) |
| `r` | Force refresh |
| `q` / `ctrl+c` | Quit |

### Editor support

Pressing `o` opens the selected session's working directory in your editor.

| Configuration | Behavior |
|---------------|----------|
| Both `$VISUAL` and `$EDITOR` set | A picker popup asks which one to use |
| Only `$VISUAL` set | Opens directly as GUI editor |
| Only `$EDITOR` set | Opens directly as TUI editor (suspends the TUI) |
| Neither set | Shows a hint to configure them |

```bash
# Example: add to ~/.zshrc or ~/.bashrc
export VISUAL="code"   # GUI editor (VS Code, Cursor, Zed, …)
export EDITOR="nvim"   # TUI editor (vim, nvim, nano, …)
```

## Roadmap

### v0.1 — Core TUI
- [x] Discover all Claude Code sessions from `~/.claude/projects/`
- [x] Parse JSONL to determine session status
- [x] Detect worktrees
- [x] Show tool history
- [x] FSEvents-based file watcher with debouncing
- [x] Fallback 30s polling

### v0.2 — Richer session info (current)
- [x] Conversation preview in detail panel (last 5 messages, User/AI labels)
- [x] Last file written with age
- [x] Filter sessions by activity type
- [x] Search sessions by project path
- [x] Time window control (show last N minutes)
- [x] Color-coded activity states with grace periods
- [x] Memory-efficient single-pass JSONL parsing
- [x] Activity sparkline graph in session list
- [x] Token usage and cost estimation in detail panel
- [x] Animated braille spinner for active sessions
- [x] `o` key to open session CWD in editor (GUI via `$VISUAL`, TUI via `$EDITOR`, picker when both set)
- [ ] Display file diff for last written file

### v0.3 — HTTP API
- [ ] Embedded HTTP server (`--api` flag)
- [ ] `GET /sessions` — JSON list of all sessions
- [ ] `GET /sessions/:id` — Full session detail
- [ ] `GET /sessions/:id/events` — SSE stream of status changes
- [ ] Authentication token support

### v0.4 — Hooks & Webhooks
- [ ] Claude Code hook integration (read hook output files)
- [ ] Outbound webhooks on status changes (e.g. waiting → send Slack notification)
- [ ] Configurable via `~/.config/lazyagent/config.yaml`
- [ ] Webhook payload format (session ID, status, project, timestamp)

### v0.5 — Notifications & integrations
- [ ] macOS native notifications when session needs input
- [ ] Linear issue linking (detect issue refs in conversation)
- [ ] Desktop menu bar icon (systray) showing active session count
- [ ] `lazyagent notify` CLI mode (run headless, only notify)

### Future ideas
- [ ] Multi-machine support via shared config / remote API
- [ ] TUI actions: kill session, attach terminal
- [ ] Session history browser (browse past conversations)
