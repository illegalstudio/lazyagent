---
title: "CLI reference"
description: "Every `lazyagent` invocation flag, with syntax, defaults, and examples."
sidebar:
  order: 1
---

This page documents the root `lazyagent` command — the one you run to monitor agents. Maintenance and search subcommands live alongside it:

- [`lazyagent prune`](../maintenance/prune.md) — delete old or orphaned chat files
- [`lazyagent compact`](../maintenance/compact.md) — truncate bulky payloads in place
- [`lazyagent search`](../maintenance/search.md) — search transcript-file agents with highlighted snippets
- [`lazyagent limits`](../maintenance/limits.md) — show 5-hour, weekly, and monthly usage summary; add `--detailed` for pace
- [`lazyagent sessions`](sessions.md) — list sessions for the current directory and reopen one

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
| `--gui` | bool | `false` | Launch the macOS/Linux desktop app (detaches) |
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

Launch the desktop app. Native installs detach from the terminal immediately, so the shell prompt returns while the app appears in the macOS menu bar or Linux system tray. An AppImage stays attached when launched from a shell; append `&` if you want to background it.

```bash
lazyagent --gui              # desktop app only (detached)
lazyagent --gui --api        # desktop app + API in foreground
lazyagent --tui --gui --api  # everything (TUI foreground, tray and API in background)
```

The CLI-only build prints an error for `--gui`; install a desktop package to enable it. See [macOS GUI](../interfaces/macos-gui.md) or [Linux GUI](../interfaces/linux-gui.md).

### `--api`

Start the HTTP API server.

```bash
lazyagent --api              # default bind: 127.0.0.1:7421
lazyagent --api --host :8080 # custom port, localhost only
lazyagent --api --host 0.0.0.0:7421   # expose on the network
```

On the very first interactive run, `--api` prompts for a passphrase, saves it to `~/.config/lazyagent/config.json`, and prints the derived bearer token to stderr once. On subsequent runs it does not print the token; use `lazyagent passphrase --show` when you explicitly need it. Set `LAZYAGENT_API_PASSPHRASE` in the environment to override the configured value (useful for CI / launchd).

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
| `grok` | Grok CLI |
| `kilo` | Kilo |
| `kimi` | Kimi Code CLI |
| `cursor` | Cursor IDE |
| `opencode` | OpenCode |
| `all` | Every enabled agent (default) |

```bash
lazyagent --agent claude     # only Claude
lazyagent --agent codex      # only Codex
lazyagent --agent grok       # only Grok
lazyagent --agent kilo       # only Kilo
lazyagent --agent kimi       # only Kimi Code
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

When the first positional argument is `prune`, `compact`, `search`, `sessions`, `latest`, `limits`, or `passphrase`, lazyagent switches into subcommand mode. Root-level flags are ignored and the subcommand parses its own set. `history` is retained as a legacy alias for `sessions`; it does not preserve the removed history-specific flags or output format.

```bash
lazyagent prune --days 30          # prune subcommand
lazyagent compact --agent claude   # compact subcommand
lazyagent search --agent codex api # search subcommand
lazyagent sessions --agent codex   # sessions subcommand
lazyagent sessions --yolo          # picker with YOLO as the default resume mode
lazyagent history --agent codex    # alias for sessions
lazyagent latest                   # resume the newest session here
lazyagent latest --yolo            # resume the newest session in YOLO mode
lazyagent limits --agent claude    # limits subcommand
lazyagent passphrase               # rotate the API passphrase
lazyagent --agent claude prune     # ❌ wrong: prune is not a flag value
```

See [`prune`](../maintenance/prune.md), [`compact`](../maintenance/compact.md), [`search`](../maintenance/search.md), [`sessions`](sessions.md), [`latest`](latest.md), and [`limits`](../maintenance/limits.md) for their flag tables.

### `search`

`search` runs full-text search over local agent transcripts (Claude, Codex, pi, Amp, Grok, Kimi) using an incremental SQLite FTS5 index under the user cache directory. Cursor, OpenCode, and Kilo are excluded because their history lives in third-party SQLite databases.

```bash
lazyagent search "race condition"
lazyagent search --agent codex "parser"
lazyagent search --reindex "config"
lazyagent search --yolo "config"
```

After printing results in an interactive terminal, `search` prompts for a result number; entering one opens that chat via the originating agent's resume command when lazyagent knows one. Add `--yolo` to use the agent-specific permissive variant. Grok sessions resume with `grok --resume '<session-id>'`. Piped output stays non-interactive.

Full reference, including the index location, ranking, and resume commands: [`search`](../maintenance/search.md).

### `limits`

`limits` prints a one-shot summary table of the rate-limit / billing windows exposed by Claude Code, Codex, Grok, Kimi, and Cursor. The default table labels each 5-hour and weekly/global cell as `used` and `exp`, where `exp` is the linear pace for elapsed window time; `--detailed` prints the full per-window report with reset times and the pace indicator (`underutilizing` / `on track` / `overutilizing`). Claude and Codex each expose a 5-hour and a 7-day window; Grok exposes a single monthly credit window; Kimi exposes the windows returned by Kimi Code CLI's `/status` endpoint; Cursor exposes two monthly rows sharing one billing-cycle window — its Auto/Composer pool and its usage-based API pool, each against its own allowance.

```bash
lazyagent limits                 # summary table for all supported limits providers
lazyagent limits --detailed      # detailed report with bars, reset times, and pace
lazyagent limits --agent claude  # only Claude Code
lazyagent limits --agent codex   # only Codex
lazyagent limits --agent grok    # only Grok
lazyagent limits --agent kimi    # only Kimi Code
lazyagent limits --agent cursor  # only Cursor (Models + API pools)
```

Claude data comes from `/api/oauth/usage` on `api.anthropic.com` — the same undocumented endpoint Claude Code's `/status` calls. Codex data comes from `/backend-api/wham/usage` on `chatgpt.com` — the same endpoint the Codex CLI's TUI polls for its rate-limit display. Grok data comes from `/v1/billing` on `cli-chat-proxy.grok.com` — the same undocumented endpoint Grok CLI's `/usage show` slash command calls. Kimi data comes from `/coding/v1/usages` on `api.kimi.com`, the endpoint Kimi Code CLI's `/status` slash command calls. Cursor data comes from `/api/usage-summary` on `cursor.com` — the same endpoint the Cursor dashboard uses for its usage headline — read with the session token from Cursor's local `state.vscdb`; it reports the Auto/Composer and usage-based API pools as separate percentages, shown as two rows.

Full reference, including disclaimers and token-resolution order: [`limits`](../maintenance/limits.md).

### `passphrase`

`passphrase` sets or rotates the passphrase that protects the [HTTP API](../interfaces/http-api.md). It runs without starting the server, so you can change credentials at any time and let any future `lazyagent --api` pick up the new value.

```bash
lazyagent passphrase             # interactive prompt and save
lazyagent passphrase --show      # print the bearer token for the current passphrase
```

`passphrase` (no flags) always prompts (double-entry confirmation), even if a passphrase is already configured — it's a rotation, not a setup. `--show` derives the token from the env var if `LAZYAGENT_API_PASSPHRASE` is set, otherwise from the configured value, without prompting or changing the passphrase.

`--show` writes the raw token to **stdout** (single line, no prefix) so it can be captured in a pipe: `TOKEN=$(lazyagent passphrase --show)`. Diagnostics (passphrase source, missing-config hints) go to stderr. Other invocations do not print the token.

Restart any running `lazyagent --api` after rotating: the server reads the passphrase at startup, so it keeps using the old token until restarted.

## Common invocations

```bash
# Terminal UI, all agents (the default)
lazyagent

# Terminal UI but only Claude
lazyagent --agent claude

# Desktop app only
lazyagent --gui

# Desktop app + HTTP API
lazyagent --gui --api

# HTTP API exposed on the LAN for a mobile client
lazyagent --api --host 0.0.0.0:7421

# Everything at once
lazyagent --tui --gui --api

# Demo mode for screenshots
lazyagent --demo --gui

# Search chat history
lazyagent search "api server"

# List and reopen sessions for the current directory
lazyagent sessions

# The old name is an alias for sessions
lazyagent history

# Jump straight back into the newest session here
lazyagent latest
```

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Normal exit |
| `1` | Runtime error (bind failure, invalid argument, IO error, …) — details printed to stderr |

Subcommands define their own exit codes; see their respective reference pages.

## Environment variables

| Variable | Effect |
|----------|--------|
| `CLAUDE_CONFIG_DIR` | Alternate Claude home when `claude_dirs` is not set in the config. Must contain a `projects/` subfolder |
| `CLAUDE_CODE_OAUTH_TOKEN` | Override the OAuth token used by `lazyagent limits` for the Claude call. Bypasses the macOS keychain and the credentials file |
| `GROK_OAUTH_TOKEN` | Override the OAuth token used by `lazyagent limits` for the Grok billing call. Bypasses `~/.grok/auth.json` |
| `KIMI_SHARE_DIR` | Alternate Kimi Code data root. Defaults to `~/.kimi-code` |
| `KIMI_CODE_OAUTH_TOKEN` | Override the OAuth token used by `lazyagent limits` for the Kimi call. Bypasses `~/.kimi-code/credentials/kimi-code.json` |
| `KIMI_CODE_BASE_URL` | Override the Kimi Code API base URL for `lazyagent limits`; `/usages` is appended |
| `XDG_CONFIG_HOME` | Overrides the default `~/.config` base for `~/.config/lazyagent/` |
| `VISUAL` | Preferred GUI editor for <kbd>o</kbd> (TUI) / Open (GUI). See [Editor support](../reference/editor-support.md) |
| `EDITOR` | Fallback terminal editor when `$VISUAL` is unset |
| `LAZYAGENT_DETACHED` | Internal marker set when the tray forks itself; do not set manually |
