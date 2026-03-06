# lazyclaude

A terminal UI for monitoring all running [Claude Code](https://claude.ai/code) instances on your machine — inspired by [lazygit](https://github.com/jesseduffield/lazygit) and [lazyworktree](https://github.com/chmouel/lazyworktree).

```
╭─────────────────────────────╮╭──────────────────────────────────────────╮
│ PROJECT          STATUS     ││ /Volumes/Crucio/Developer/myapp          │
│ ────────────────────────     ││ ⚙  tool  → Bash                          │
│ myapp            ⚙  tool   ││                                           │
│ myapp/worktree   ⏳ waiting ││ PID                 84231                 │
│ other-project    💤 idle    ││ Session ID          0279269c...           │
│                             ││ Version             2.1.70                │
│                             ││ Model               claude-sonnet-4-6     │
│                             ││ Git Branch          feat/new-api          │
│                             ││ Worktree            no                    │
│                             ││ Permissions         ⚠ dangerously-skip... │
│                             ││ Messages            42 total (18u, 24a)   │
│                             ││ Last activity       3s ago                │
│                             ││                                           │
│                             ││ Recent Tools                              │
│                             ││   ⚙  Bash         5s ago                 │
│                             ││   ⚙  Read         12s ago                │
│                             ││   ⚙  Write        30s ago                │
╰─────────────────────────────╯╰──────────────────────────────────────────╯
 ↑/k up  ↓/j down  tab switch panel  r refresh  q quit
```

## How it works

lazyclaude watches Claude Code's JSONL transcript files (`~/.claude/projects/*/`) to determine what each session is doing. No modifications to Claude Code are needed — it's purely observational.

From the JSONL stream it can detect:

- **waiting** — Claude responded, waiting for your input
- **thinking** — You sent a message, Claude is generating
- **tool** — Claude invoked a tool (Bash, Read, Write, etc.)
- **processing** — Tool result received, Claude is analyzing
- **idle** — Session file exists but process is no longer running

Combined with `ps` and `lsof`, it also surfaces:

| Info | Source |
|------|--------|
| Working directory | JSONL + lsof |
| Git branch | JSONL |
| Claude version | JSONL |
| Model used | JSONL |
| PID | ps |
| `--dangerously-skip-permissions` | ps args |
| Is git worktree | git rev-parse |
| Main repo path (if worktree) | git worktree |
| Message count (user/assistant) | JSONL |
| Last N tools used | JSONL |
| Last activity timestamp | JSONL |

## Install

```bash
go install github.com/nahime0/lazyclaude@latest
```

Or build from source:

```bash
git clone https://github.com/nahime0/lazyclaude
cd lazyclaude
go build -o lazyclaude .
```

## Usage

```
lazyclaude
```

### Keybindings

| Key | Action |
|-----|--------|
| `↑` / `k` | Move up |
| `↓` / `j` | Move down |
| `tab` | Switch focus between panels |
| `r` | Force refresh |
| `q` / `ctrl+c` | Quit |

## Roadmap

### v0.1 — Core TUI (current)
- [x] Discover all Claude Code sessions from `~/.claude/projects/`
- [x] Parse JSONL to determine session status
- [x] Match sessions to running processes via `ps` + `lsof`
- [x] Detect worktrees
- [x] Detect `--dangerously-skip-permissions`
- [x] Show tool history
- [x] Auto-refresh every 2 seconds

### v0.2 — Richer session info
- [ ] Show full conversation preview in detail panel
- [ ] Display file diff / last file written
- [ ] Filter sessions (active only, by project, by status)
- [ ] Sort by status / name / last activity
- [ ] Search sessions

### v0.3 — HTTP API
- [ ] Embedded HTTP server (`--api` flag)
- [ ] `GET /sessions` — JSON list of all sessions
- [ ] `GET /sessions/:id` — Full session detail
- [ ] `GET /sessions/:id/events` — SSE stream of status changes
- [ ] Authentication token support

### v0.4 — Hooks & Webhooks
- [ ] Claude Code hook integration (read hook output files)
- [ ] Outbound webhooks on status changes (e.g. waiting → send Slack notification)
- [ ] Configurable via `~/.config/lazyclaude/config.yaml`
- [ ] Webhook payload format (session ID, status, project, timestamp)

### v0.5 — Notifications & integrations
- [ ] macOS native notifications when session needs input
- [ ] Linear issue linking (detect issue refs in conversation)
- [ ] Desktop menu bar icon (systray) showing active session count
- [ ] `lazyclaude notify` CLI mode (run headless, only notify)

### Future ideas
- [ ] Multi-machine support via shared config / remote API
- [ ] TUI actions: open project in editor, kill session, attach terminal
- [ ] Session history browser (browse past conversations)
