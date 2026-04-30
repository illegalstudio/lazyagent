---
title: "Show rate-limit usage"
description: "On-demand snapshot of Claude Code and Codex 5-hour and weekly rate-limit windows, with a pace indicator vs. linear consumption."
sidebar:
  order: 3
---

`lazyagent limits` prints a one-shot snapshot of the **5-hour** and **weekly** rate-limit windows for Claude Code and Codex, with a *pace indicator* that compares actual consumption to a perfectly linear pace through the window. It's read-only, on demand, and does not poll.

Use it to answer questions like *"am I burning the weekly limit faster than I should?"* before you commit to a long agent run, or *"how much of my 5-hour budget is left until the next reset?"* when you suspect you're close to the wall.

## Synopsis

```
lazyagent limits [--agent claude|codex|all]
```

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent NAME` | string | `all` | Which agent to query: `claude`, `codex`, or `all` |
| `--help` | bool | `false` | Print usage and exit |

Only `claude` and `codex` are supported — they're the only agents in lazyagent's set that expose rate-limit windows in a stable, observable form.

## Quick reference

```bash
lazyagent limits                   # both agents (default)
lazyagent limits --agent claude    # only Claude Code
lazyagent limits --agent codex     # only Codex
lazyagent limits --help            # full usage + disclaimers
```

## Output

For each window the command prints four lines:

| Field | Meaning |
|-------|---------|
| **Used** | Percentage of the window's quota consumed (0-100), with a colored progress bar (green ≤ 50%, blue ≤ 75%, orange ≤ 90%, red above) |
| **Elapsed** | How far we are into the window's time, with a muted progress bar |
| **Resets** | Relative time until the window rolls over plus the absolute reset time in your local zone |
| **Pace** | Consumption vs. linear: `0.00× of expected NN.N%` where `NN.N%` is where you'd be on a perfectly linear pace. Label is colored (green = on track, red = overutilizing, muted = underutilizing) |

The pace label uses three buckets:

| Label | Ratio (used / elapsed) | Interpretation |
|-------|------------------------|----------------|
| `underutilizing` | `< 0.85` | You have headroom — usage is below linear |
| `on track` | `0.85 – 1.15` | Roughly aligned with linear consumption |
| `overutilizing` | `> 1.15` | Burning faster than linear — likely to hit the limit before the window resets |

When the window has just reset (less than 1% elapsed), pace is shown as `—` since the ratio is not meaningful yet.

### Example

```
Claude Code

  5-hour window
    Used:      21.0%  ████░░░░░░░░░░░░░░░░
    Elapsed:   39.9%  ████████░░░░░░░░░░░░
    Resets:   in 3h 0m (Thu 30 Apr 20:10 CEST)
    Pace:     underutilizing (0.53× of expected 39.9%)

  7-day window
    Used:      23.0%  █████░░░░░░░░░░░░░░░
    Elapsed:   35.8%  ███████░░░░░░░░░░░░░
    Resets:   in 4d 11h (Tue 5 May 05:00 CEST)
    Pace:     underutilizing (0.64× of expected 35.8%)

  Note: reads /api/oauth/usage, an undocumented Claude endpoint. May break or be revoked by Anthropic without notice.

Codex

  5-hour window
    Used:       4.0%  █░░░░░░░░░░░░░░░░░░░
    Elapsed:   34.1%  ███████░░░░░░░░░░░░░
    Resets:   in 3h 17m (Thu 30 Apr 20:27 CEST)
    Pace:     underutilizing (0.12× of expected 34.1%)

  7-day window
    Used:      11.0%  ██░░░░░░░░░░░░░░░░░░
    Elapsed:   34.1%  ███████░░░░░░░░░░░░░
    Resets:   in 4d 14h (Tue 5 May 07:56 CEST)
    Pace:     underutilizing (0.32× of expected 34.1%)

  Source: /Users/me/.codex/sessions/2026/04/24/rollout-…jsonl
  Note: limits are read from the latest Codex session rollout, not fetched live. They reflect the server's last response.
```

In an interactive terminal the bars and pace label are colored. When piped or redirected, lazyagent strips ANSI escapes automatically.

## How it gets the data

The two providers work very differently — there's a single command, but two paths under the hood.

### Claude Code

A single HTTPS GET to `https://api.anthropic.com/api/oauth/usage` with the user's OAuth bearer token and `anthropic-beta: oauth-2025-04-20`. This is the **same** endpoint Claude Code's interactive `/status` slash command queries.

The OAuth token is read in this priority order:

1. **`CLAUDE_CODE_OAUTH_TOKEN`** environment variable — useful for CI or for overriding the local credential store
2. **macOS Keychain** — service `Claude Code-credentials`, account `$USER`
3. **`~/.claude/.credentials.json`** — the on-disk fallback (Linux default; macOS fallback)

If none of the three is present, the command tells you to run `claude` to log in.

The User-Agent identifies lazyagent honestly (`lazyagent/<version> (+https://github.com/illegalstudio/lazyagent)`); it does **not** impersonate Claude Code. If Anthropic ever audits this traffic, the request can be attributed to lazyagent and the project page tells them where to file a complaint.

### Codex

No network call. lazyagent walks `~/.codex/sessions/`, picks the most recent rollout JSONL by mtime, and extracts the last `rate_limits` block from it. Codex itself persists the server's rate-limit response after every turn inside `event_msg` payloads of type `token_count`, so the last entry is always representative of where the user stood at the end of their last Codex interaction.

If the most recent rollout has no `rate_limits` event yet (a brand-new session that hasn't completed its first turn), lazyagent falls back to the next-most-recent rollout that does.

The `Source:` line in the output points to the rollout actually read — useful when you notice the data is stale and want to confirm whether you've simply not used Codex recently.

## Disclaimer (Claude Code)

`/api/oauth/usage` is **not** part of Anthropic's documented public API. As of this writing it is used internally by Claude Code's `/status` UI and is subject to:

- **Aggressive rate limiting** — the endpoint will reject sustained polling with HTTP 429. lazyagent is intentionally on-demand only and does not poll.
- **No stability guarantee** — the endpoint, response shape, or beta header may change without notice. lazyagent fails gracefully when this happens (clear error, exit code 1).
- **Subscription scope** — the endpoint reflects the limits of the Claude.ai Pro/Max plan associated with the OAuth token. API-key users (Console, Bedrock, Vertex) won't see meaningful data here.

If you're distributing lazyagent or building automation on top of it, prefer running `lazyagent limits` only when a human has asked for it. Don't put it in a `watch` loop.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | Both requested agents succeeded |
| `1` | At least one agent failed (token missing, endpoint error, no Codex sessions, …) — details on stderr |
| `2` | Invalid flags (e.g. unknown `--agent` value) |

Even on partial failure (`1`), the successful agents' output is printed to stdout. Errors go to stderr with a `Error (claude): …` / `Error (codex): …` prefix, so you can pipe stdout to a parser without losing the error context.

## Environment

| Variable | Effect |
|----------|--------|
| `CLAUDE_CODE_OAUTH_TOKEN` | Override the OAuth token for the Claude call. Used in priority before the macOS keychain or the credentials file |

## See also

- [Roadmap](../reference/roadmap.md) — shipped features per version
- [`lazyagent prune`](prune.md) — delete chat files (destructive, complementary)
- [`lazyagent compact`](compact.md) — shrink chat files in place (destructive, complementary)
