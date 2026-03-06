# lazyagent

A terminal UI for monitoring all running [Claude Code](https://claude.ai/code) instances on your machine — inspired by [lazygit](https://github.com/jesseduffield/lazygit), [lazyworktree](https://github.com/chmouel/lazyworktree) and [pixel-agents](https://github.com/pablodelucca/pixel-agents).

```
╭─────────────────────────────╮╭──────────────────────────────────────────╮
│ PROJECT          STATUS     ││ /Volumes/Crucio/Developer/myapp          │
│ ────────────────────────     ││ ● running                                │
│ myapp            running    ││                                           │
│ myapp/worktree   waiting    ││ Session ID          0279269c…b3cb         │
│ other-project    idle       ││ Version             2.1.70                │
│                             ││ Model               claude-sonnet-4-6     │
│                             ││ Git Branch          feat/new-api          │
│                             ││ Worktree            no                    │
│                             ││ Messages            42  (18 user, 24 ai)  │
│                             ││ Last operation      Bash  (3s ago)        │
│                             ││ Last file           src/main.go (12s ago) │
│                             ││ ─────────────────────────────────         │
│                             ││ Conversation                              │
│                             ││   AI    Fixed the parsing bug…            │
│                             ││   User  Can you also fix the tests?       │
│                             ││ ─────────────────────────────────         │
│                             ││ Recent Tools                              │
│                             ││   Bash         5s ago                     │
│                             ││   Read         12s ago                    │
│                             ││   Write        30s ago                    │
╰─────────────────────────────╯╰──────────────────────────────────────────╯
 k/↑ prev  j/↓ next  tab detail  +/- mins  f filter  / search  r refresh  q quit
```

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
| Last file written | JSONL |
| Recent conversation (last 5 messages) | JSONL |
| Last 20 tools used | JSONL |
| Last activity timestamp | JSONL |

## Install

```bash
go install github.com/nahime0/lazyagent@latest
```

Or build from source:

```bash
git clone https://github.com/nahime0/lazyagent
cd lazyagent
go build -o lazyagent .
```

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
| `r` | Force refresh |
| `q` / `ctrl+c` | Quit |

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
- [ ] TUI actions: open project in editor, kill session, attach terminal
- [ ] Session history browser (browse past conversations)
