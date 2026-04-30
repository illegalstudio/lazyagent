---
title: "CLI reference"
description: "Every `lazyagent` invocation flag, with syntax, defaults, and examples."
sidebar:
  order: 1
---

This page documents the root `lazyagent` command — the one you run to monitor agents. Maintenance and search subcommands live alongside it:

- [`lazyagent prune`](../maintenance/prune.md) — delete old or orphaned chat files
- [`lazyagent compact`](../maintenance/compact.md) — truncate bulky payloads in place
- `lazyagent search` — search transcript-file agents with highlighted snippets
- [`lazyagent limits`](../maintenance/limits.md) — show 5-hour and weekly rate-limit usage with a pace indicator

## Synopsis

```
lazyagent [--tui] [--gui] [--api] [--host ADDR]
          [--agent NAME] [--demo]
          [--version] [--help]
```

No flag at all is the most common invocation — it opens the TUI with every supported agent enabled.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--tui` | bool | auto | Launch the terminal UI. Implicit when no other mode is set |
| `--gui` | bool | `false` | Launch the macOS menu bar app (detaches) |
| `--api` | bool | `false` | Start the HTTP API server |
| `--host ADDR` | string | `127.0.0.1:7421` | API listen address (only relevant with `--api`) |
| `--agent NAME` | string | `all` | Restrict monitoring to one agent |
| `--demo` | bool | `false` | Use generated fake data instead of real sessions |
| `--version` | bool | `false` | Print the version and exit |
| `--help` | bool | `false` | Print usage and exit |
| `--tray` | bool | `false` | **Deprecated** alias for `--gui`, kept for backwards compatibility |

### `--tui`

Explicitly open the terminal UI. Omitting it is the same as passing it *unless* another mode is specified:

```bash
lazyagent                    # implicit --tui
lazyagent --tui              # explicit, same result
lazyagent --api              # API only, no TUI
lazyagent --tui --api        # TUI in foreground, API in background
```

See [Terminal UI](../interfaces/terminal-ui.md) for keybindings.

### `--gui`

Launch the macOS menu bar app. The process detaches from your terminal immediately — the shell prompt returns, and the app appears in the menu bar.

```bash
lazyagent --gui              # menu bar only (detached)
lazyagent --gui --api        # menu bar + API in foreground
lazyagent --tui --gui --api  # everything (TUI foreground, tray and API in background)
```

On non-macOS systems `--gui` prints an error. See [macOS GUI](../interfaces/macos-gui.md).

### `--api`

Start the read-only HTTP API server.

```bash
lazyagent --api              # default bind: 127.0.0.1:7421
lazyagent --api --host :8080 # custom port, localhost only
lazyagent --api --host 0.0.0.0:7421   # expose on the network
```

If the chosen port is busy, the default bind falls back across `7421`–`7431`; when `--host` is set, there is no fallback. Full reference: [HTTP API](../interfaces/http-api.md).

### `--host ADDR`

Override the API bind address. Accepts any Go `net.Listen` address:

| Value | Meaning |
|-------|---------|
| `:7421` | All interfaces, port 7421 (shorthand for `0.0.0.0:7421`) |
| `127.0.0.1:9000` | Localhost, custom port |
| `0.0.0.0:7421` | All interfaces, default port (e.g. LAN exposure) |

Ignored without `--api`.

### `--agent NAME`

Restrict monitoring to one agent. Valid values:

| Value | Sessions included |
|-------|-------------------|
| `claude` | Claude Code CLI **and** Desktop |
| `pi` | pi coding agent |
| `codex` | Codex CLI |
| `amp` | Amp CLI |
| `cursor` | Cursor IDE |
| `opencode` | OpenCode |
| `all` | Every enabled agent (default) |

```bash
lazyagent --agent claude     # only Claude
lazyagent --agent codex      # only Codex
lazyagent --agent all        # default — every agent
```

To disable agents *permanently* (rather than per-invocation), flip them in the [`agents` map of your config](../reference/configuration.md#agents).

### `--demo`

Replace real session discovery with a curated fake dataset — useful for screenshots, demos, or debugging the UI without cluttering your actual agent history.

```bash
lazyagent --demo             # fake TUI
lazyagent --demo --gui       # fake tray app
```

Combinable with any interface flag.

### `--version`

```bash
lazyagent --version
```

Prints the running version and, if a newer release is available on GitHub, a hint to update.

### `--help`

```bash
lazyagent --help
```

Prints the full usage text, including short keybinding reference.

## Subcommand dispatch

When the first positional argument is `prune`, `compact`, `search`, or `limits`, lazyagent switches into subcommand mode — root-level flags are ignored and the subcommand parses its own set.

```bash
lazyagent prune --days 30          # prune subcommand
lazyagent compact --agent claude   # compact subcommand
lazyagent search --agent codex api # search subcommand
lazyagent limits --agent claude    # limits subcommand
lazyagent --agent claude prune     # ❌ wrong: prune is not a flag value
```

See [`prune`](../maintenance/prune.md), [`compact`](../maintenance/compact.md), and [`limits`](../maintenance/limits.md) for their flag tables.

### `search`

Search indexes local transcript files incrementally into a SQLite FTS database under the user cache directory, then prints matching sessions with highlighted snippets. It intentionally excludes agents backed by third-party SQLite databases such as Cursor and OpenCode.

After printing results in an interactive terminal, `search` prompts for a result number. Entering one opens that chat with the originating agent's resume command; pressing Enter exits without opening anything. Piped output stays non-interactive.

```bash
lazyagent search "race condition"
lazyagent search --agent codex "parser"
lazyagent search --reindex "config"
```

Useful flags:

| Flag | Default | Summary |
|------|---------|---------|
| `--agent NAME` | `all` | Search one transcript-file agent or a comma-separated subset (`claude,codex,pi,amp`) |
| `--limit N` | `20` | Maximum chat sessions to show |
| `--snippets N` | `2` | Maximum snippets per chat session |
| `--reindex` | `false` | Rebuild the local search index before searching |

### `limits`

`limits` prints a one-shot snapshot of the 5-hour and weekly rate-limit windows for Claude Code and Codex, with a pace indicator that compares actual consumption to a perfectly linear pace (`underutilizing` / `on track` / `overutilizing`).

```bash
lazyagent limits                 # both agents
lazyagent limits --agent claude  # only Claude Code
lazyagent limits --agent codex   # only Codex
```

Claude data comes from `/api/oauth/usage` on `api.anthropic.com` — the same undocumented endpoint Claude Code's `/status` calls. Codex data is read from the latest rollout JSONL under `~/.codex/sessions/` (no network call).

Full reference, including disclaimers and token-resolution order: [`limits`](../maintenance/limits.md).

## Common invocations

```bash
# Terminal UI, all agents (the default)
lazyagent

# Terminal UI but only Claude
lazyagent --agent claude

# Menu bar app only
lazyagent --gui

# Menu bar app + HTTP API (ideal daily-driver combo on macOS)
lazyagent --gui --api

# HTTP API exposed on the LAN for a mobile client
lazyagent --api --host 0.0.0.0:7421

# Everything at once
lazyagent --tui --gui --api

# Demo mode for screenshots
lazyagent --demo --gui

# Search chat history
lazyagent search "api server"
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Normal exit |
| `1` | Runtime error (bind failure, invalid argument, IO error, …) — details printed to stderr |

The maintenance subcommands define their own exit codes; see their respective pages.

## Environment variables

| Variable | Effect |
|----------|--------|
| `CLAUDE_CONFIG_DIR` | Alternate Claude home when `claude_dirs` is not set in the config. Must contain a `projects/` subfolder |
| `CLAUDE_CODE_OAUTH_TOKEN` | Override the OAuth token used by `lazyagent limits` for the Claude call. Bypasses the macOS keychain and the credentials file |
| `XDG_CONFIG_HOME` | Overrides the default `~/.config` base for `~/.config/lazyagent/` |
| `VISUAL` | Preferred GUI editor for <kbd>o</kbd> (TUI) / Open (GUI). See [Editor support](../reference/editor-support.md) |
| `EDITOR` | Fallback terminal editor when `$VISUAL` is unset |
| `LAZYAGENT_DETACHED` | Internal marker set when the tray forks itself; do not set manually |
