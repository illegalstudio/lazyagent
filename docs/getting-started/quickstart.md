---
title: "Quickstart"
description: "Launch lazyagent for the first time and pick an interface."
sidebar:
  order: 2
---

Once lazyagent is [installed](installation.md), a five-minute tour gets you oriented. For the exhaustive flag reference, jump straight to the [CLI reference](../usage/cli.md).

## First launch

With no arguments, lazyagent opens the terminal UI and scans every supported agent:

```bash
lazyagent
```

You'll see a list of sessions on the left and a detail panel on the right. Use <kbd>↑</kbd>/<kbd>↓</kbd> to navigate, <kbd>tab</kbd> to switch focus between panels, and <kbd>q</kbd> to quit. The full keybinding list lives in [Terminal UI](../interfaces/terminal-ui.md).

## The three interfaces

lazyagent is a single binary with three interfaces, selectable by flag:

```bash
lazyagent              # Terminal UI (default)
lazyagent --gui        # macOS menu bar app (detaches from the terminal)
lazyagent --api        # HTTP API on http://127.0.0.1:7421 (Bearer-token protected)
```

They're combinable — on a typical macOS setup:

```bash
lazyagent --gui --api
```

runs the menu bar app and the API side by side from a single process. See [Recipes](../usage/recipes.md) for more combinations.

The first time you launch `--api`, lazyagent prompts for a passphrase, derives a Bearer token from it (PBKDF2-SHA256), and prints the token to stderr once. Clients can fetch the public salt from `/api/auth` and derive the same token locally — see [HTTP API → Authentication](../interfaces/http-api.md#authentication).

## Scope to one agent

```bash
lazyagent --agent claude    # Claude Code CLI + Desktop
lazyagent --agent codex     # Codex CLI only
lazyagent --agent grok      # Grok CLI only
lazyagent --agent kimi      # Kimi Code CLI only
lazyagent --agent all       # every agent (default)
```

Every value for `--agent` is documented in [CLI reference](../usage/cli.md#-agent-name).

## Maintenance at a glance

The maintenance subcommands keep chat transcripts and usage windows under control:

```bash
lazyagent prune --days 30        # delete sessions idle for >30 days
lazyagent compact                # shrink session files in place
lazyagent search "query"         # search local transcripts
lazyagent limits                 # show rate-limit / billing usage
```

`prune` and `compact` support `--dry-run` and an interactive agent picker. Full reference:

- [Prune old sessions](../maintenance/prune.md)
- [Compact session files](../maintenance/compact.md)
- [Search chat transcripts](../maintenance/search.md)
- [Show rate-limit usage](../maintenance/limits.md)

## Next steps

- Skim the [Supported agents](../concepts/supported-agents.md) table to know what each prefix means
- Learn the mental model in [How it works](../concepts/how-it-works.md)
- Tweak the time window, theme, and per-agent toggles in [Configuration](../reference/configuration.md)
- Browse the [Recipes](../usage/recipes.md) for end-to-end setups
