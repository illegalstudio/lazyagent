---
title: "Show rate-limit usage"
description: "On-demand summary of Claude Code, Codex, Grok, Kimi, and Cursor rate-limit or billing windows, with a detailed pace view available on demand."
sidebar:
  order: 3
---

`lazyagent limits` prints a one-shot summary table of the rate-limit / billing windows exposed by Claude Code, Codex, Grok, Kimi, and Cursor. The default output is optimized for a quick scan: one row per agent — two for Cursor, whose Auto/Composer and API pools are metered independently — with a **5-hour** column and a **weekly/global** column. Each populated cell labels both **used** and **expected**, where expected is the linear usage pace for elapsed window time. `--detailed` prints the full per-window report with reset times, source notes, and the *pace indicator* that compares actual consumption to a perfectly linear pace through the window. It's read-only, on demand, and does not poll.

Claude and Codex each expose a **5-hour** and a **7-day** window; Grok exposes a single credit window — **weekly** on xAI's unified billing, **monthly** on legacy plans; Kimi exposes the windows returned by Kimi Code CLI's `/status` endpoint; Cursor exposes two **monthly** rows sharing one billing-cycle window — its Auto/Composer pool and its usage-based API pool, each against its own allowance.

Use it to answer questions like *"am I burning the weekly limit faster than I should?"* before you commit to a long agent run, *"how much of my 5-hour budget is left until the next reset?"* when you suspect you're close to the wall, or *"how much of my Grok credit period have I burned?"* before kicking off a long Grok run.

The same limits are viewable interactively without leaving lazyagent: in the **TUI** press `l` to open a centered modal with **Summary** and **Detailed** tabs (the Detailed tab scrolls inside the modal); in the **GUI** click **limits** in the header or press `l`. The attached menu-bar view uses the limits page and closes with `l` or `Esc`. Detached desktop mode instead opens a non-blocking floating dialog over the usable dashboard: it has explicit refresh and close controls, can be dragged and resized, and its local pin freezes the dialog position without pinning the app window. Its size and position are kept inside the app window when it opens or the containing window changes size. It does not dim the dashboard and closes only from its close button. Both interfaces read limits on entry and refresh on demand when you press `r`; neither view polls in the background.

## Synopsis

```
lazyagent limits [--agent claude|codex|grok|kimi|cursor|all] [--detailed] [--json]
```

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--agent NAME` | string | `all` | Which agent to query: `claude`, `codex`, `grok`, `kimi`, `cursor`, or `all` |
| `--detailed` | bool | `false` | Print the detailed per-window report with bars, reset times, sources, notes, and pace |
| `--json` | bool | `false` | Print every window, pace, severity, reset time, and per-agent error as JSON (see [JSON output](#json-output)) |
| `--help` | bool | `false` | Print usage and exit |

Only `claude`, `codex`, `grok`, `kimi`, and `cursor` are supported — they're the agents in lazyagent's set that expose rate-limit or billing windows in a stable-enough, observable form.

## Quick reference

```bash
lazyagent limits                   # summary table for all supported limits providers (default)
lazyagent limits --detailed        # detailed report with bars, reset times, and pace
lazyagent limits --agent claude    # only Claude Code
lazyagent limits --agent codex     # only Codex
lazyagent limits --agent grok      # only Grok
lazyagent limits --agent kimi      # only Kimi Code
lazyagent limits --agent cursor    # only Cursor (Models + API pools)
lazyagent limits --json            # machine-readable report for scripts and widgets
lazyagent limits --help            # full usage + disclaimers
```

## Output

By default the command prints a compact terminal table:

```
╭───────────────┬────────────────────────┬────────────────────────╮
│ Agent         │ 5h                     │ Week (or Global)       │
├───────────────┼────────────────────────┼────────────────────────┤
│ Claude        │ 21.0% used / 40.0% exp │ 23.0% used / 57.1% exp │
│ Codex         │ 4.0% used / 90.0% exp  │ 11.0% used / 42.9% exp │
│ Grok          │ --                     │ 13.9% used / 51.6% exp │
│ Kimi          │ 10.0% used / 33.3% exp │ 20.0% used / 14.3% exp │
│ Cursor Models │ --                     │  5.2% used / 54.8% exp │
│ Cursor API    │ --                     │ 41.5% used / 54.8% exp │
╰───────────────┴────────────────────────┴────────────────────────╯
```

Cells label `used` and `exp` explicitly. `exp` is how much of the quota you would have consumed at a perfectly linear pace through the current window. In an interactive terminal cells are colored by severity, blending absolute usage with pace: red when used ≥ 90% or consumption is over pace, orange when used ≥ 75%, green when comfortably under pace, and the default text color when on track. `--` means that agent does not expose that window. For the `Week (or Global)` column, Claude, Codex, and Kimi use their weekly window when present; Grok uses its credit-period window (weekly on unified billing, monthly on legacy plans) and both Cursor rows use their monthly billing window. Neither Cursor row populates the `5h` column — they only have a monthly window.

With `--detailed`, each window prints four lines:

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

  Source: Codex (ChatGPT pro) /backend-api/wham/usage
  Note: reads /backend-api/wham/usage, the endpoint Codex CLI polls for its rate-limit display. May break or be revoked by OpenAI without notice.
```

In an interactive terminal the bars and pace label are colored. When piped or redirected, lazyagent strips ANSI escapes automatically.

## JSON output

`--json` prints a single object to stdout and nothing else, so the output can be piped straight into `jq`, a status-bar widget, or a dashboard. It carries everything the `--detailed` view shows plus the summary-table cells, so `--detailed` has no effect when combined with it. Field names are part of the CLI contract and every key is always present; empty lists encode as `[]`.

```bash
lazyagent limits --json | jq '.reports[] | {agent, used: .summary.week_global.used_percent}'
```

```json
{
  "generated_at": "2026-09-07T13:44:48+02:00",
  "reports": [
    {
      "agent": "claude",
      "provider": "Claude Code",
      "short_name": "Claude",
      "source": "",
      "note": "Note: reads /api/oauth/usage, an undocumented Claude endpoint. May break or be revoked by Anthropic without notice.",
      "windows": [
        {
          "label": "5-hour",
          "window_minutes": 300,
          "used_percent": 21,
          "expected_percent": 39.9,
          "pace_known": true,
          "pace_ratio": 0.53,
          "pace_label": "underutilizing",
          "used_severity": "ok",
          "resets_at": "2026-09-07T14:30:00Z",
          "reset_in_seconds": 9912,
          "reset_relative": "in 2h 45m"
        }
      ],
      "summary": {
        "five_hour": { "used_percent": 21, "expected_percent": 39.9, "severity": "ok", "text": "21.0% used / 39.9% exp" },
        "week_global": null
      }
    }
  ],
  "errors": [
    {
      "agent": "kimi",
      "kind": "not_installed",
      "message": "Kimi Code CLI is not installed or not logged in (no ~/.kimi-code/credentials/kimi-code.json). Run `kimi login`, or set KIMI_CODE_OAUTH_TOKEN."
    }
  ]
}
```

Top level:

| Field | Meaning |
|-------|---------|
| `generated_at` | When the reports were fetched, RFC 3339 in the local zone |
| `reports` | One entry per metered pool, in the canonical order `claude`, `codex`, `grok`, `kimi`, `cursor`. Cursor contributes two entries (`Cursor Models`, `Cursor API`) that share `"agent": "cursor"` |
| `errors` | Agents that produced no report, in the same order |

Each report:

| Field | Meaning |
|-------|---------|
| `agent` | Stable id to key on: `claude`, `codex`, `grok`, `kimi`, `cursor` |
| `provider` | Display name used by the detailed view (`Claude Code`, `Cursor Models`, …) |
| `short_name` | Label used by the summary table (`Claude`, `Kimi`, …) |
| `source`, `note` | The provenance line and the disclaimer from the detailed view, or `""` |
| `windows` | Every window the provider exposes, see below |
| `summary.five_hour`, `summary.week_global` | The two summary-table cells, each `{used_percent, expected_percent, severity, text}` or `null` when the provider has no such window (what the table prints as `--`) |

Each window:

| Field | Meaning |
|-------|---------|
| `label` | Provider wording: `5-hour`, `7-day`, `weekly`, `monthly`, … |
| `window_minutes` | Window length, `0` when the provider does not state it |
| `used_percent` | Quota consumed, 0-100 |
| `expected_percent` | Where a perfectly linear pace would be for the elapsed window time |
| `pace_known` | `false` when the window has just reset (less than 1% elapsed) or has no reset time; the pace fields are then `0` and `""` |
| `pace_ratio`, `pace_label` | `used / expected` and its bucket: `underutilizing`, `on track`, `overutilizing` |
| `used_severity` | Absolute-usage bucket matching the detailed bars: `ok` (≤ 50%), `info` (≤ 75%), `warn` (≤ 90%), `danger` |
| `resets_at` | RFC 3339 reset time, or `null` when unknown |
| `reset_in_seconds` | Seconds until reset, `0` when unknown or already passed |
| `reset_relative` | The `in 3h 17m` string from the detailed view, or `""` |

Summary-cell `severity` blends pace and absolute usage the same way the table colors do: `danger` when used ≥ 90% or over pace, `warn` when used ≥ 75%, `ok` when comfortably under pace, `default` when on track.

Each error:

| Field | Meaning |
|-------|---------|
| `agent` | Which agent failed |
| `kind` | `not_installed` (no credentials or footprint on this machine), `unavailable` (installed, but nothing to pace, such as an unlimited Cursor plan), or `error` (network, expired token, unexpected response) |
| `message` | The same actionable text the human output prints on stderr |

Unlike the human output, `--json` lists `not_installed` and `unavailable` agents even under `--agent all`, so a consumer can hide a provider that is simply absent instead of treating it as broken. The [exit codes](#exit-codes) are unchanged: a `not_installed` entry only fails the command when that agent was requested explicitly or when nothing at all was found.

The polling caveat below applies doubly to anything built on `--json`: refresh on demand or on a generous timer, never in a tight loop.

## How it gets the data

The providers work differently — there's a single command, but several paths under the hood.

### Claude Code

A single HTTPS GET to `https://api.anthropic.com/api/oauth/usage` with the user's OAuth bearer token and `anthropic-beta: oauth-2025-04-20`. This is the **same** endpoint Claude Code's interactive `/status` slash command queries.

The OAuth token is read in this priority order:

1. **`CLAUDE_CODE_OAUTH_TOKEN`** environment variable — useful for CI or for overriding the local credential store
2. **macOS Keychain** — service `Claude Code-credentials`, account `$USER`
3. **`~/.claude/.credentials.json`** — the on-disk fallback (Linux default; macOS fallback)

If none of the three is present, the command tells you to run `claude` to log in.

The User-Agent identifies lazyagent honestly (`lazyagent/<version> (+https://github.com/illegalstudio/lazyagent)`); it does **not** impersonate Claude Code. If Anthropic ever audits this traffic, the request can be attributed to lazyagent and the project page tells them where to file a complaint.

### Codex

A single HTTPS GET to `https://chatgpt.com/backend-api/wham/usage` with the ChatGPT OAuth bearer token and a `chatgpt-account-id` header. This is the **same** endpoint the Codex CLI's TUI polls (roughly every 60 seconds) to render its own rate-limit display, so the figures match what the CLI shows — live, not lagging behind a session rollout.

The OAuth token (and account id) are read in this priority order:

1. **`CODEX_OAUTH_TOKEN`** environment variable (with optional `CODEX_ACCOUNT_ID`) — useful for CI or overrides
2. **`~/.codex/auth.json`** — where the Codex CLI persists its ChatGPT login under `tokens.access_token` / `tokens.account_id`

If neither is present, the command tells you to run `codex` to log in. If the token has expired the endpoint returns 401 and lazyagent surfaces a message telling you to open the Codex CLI again to refresh it.

> Earlier versions read the limits locally from the latest session rollout under `~/.codex/sessions/`. That never made a network call, but it only reflected the server's response as of your last completed turn, so it could lag noticeably behind the live CLI figures. The live endpoint replaces it.

### Grok

A single HTTPS GET to `https://cli-chat-proxy.grok.com/v1/billing?format=credits` with the user's OAuth bearer token. This is the **same** endpoint (and the same `format=credits` parameter) Grok CLI ≥ 1.0.5's interactive `/usage show` slash command queries.

The OAuth token is read in this priority order:

1. **`GROK_OAUTH_TOKEN`** environment variable — useful for CI or for overriding the on-disk credential file
2. **`~/.grok/auth.json`** — the file `grok login` writes to. lazyagent picks the first entry whose `key` field is non-empty (in practice there is exactly one)

If neither is present, the command tells you to run `grok login`.

Accounts on xAI's **unified billing** (`isUnifiedBillingUser: true`) report usage as a percentage of the current credit period — typically **weekly** — via `creditUsagePercent` and a `currentPeriod` with start / end timestamps. lazyagent maps this onto a single `Window` labeled after the period type (`weekly`, `monthly`, or `daily`), uses `creditUsagePercent` directly as `Used %`, and uses the period end as the reset time (e.g. `Source: 8.0% of weekly credits used`).

**Legacy** accounts instead carry one monthly window's worth of state — the included credit limit, the amount used in the current billing period (both in cents), the on-demand spending cap, and the period start / end timestamps. lazyagent maps this onto a single `monthly` `Window`, computes `Used %` as `used / monthlyLimit × 100`, and uses the period end as the reset time. Absolute dollar amounts appear on the `Source:` line (e.g. `Source: $83.25 of $600.00 used`) so you can see both the dollar figures and the percentage in the same report.

In either shape, when the response advertises an `onDemandCap` greater than zero, the cap appears in parentheses on the same `Source:` line (e.g. `(on-demand cap: $200.00)`). The `Used %` is intentionally not re-scaled against the cap — what matters for the pace indicator is how fast you're consuming the *included* budget.

### Kimi Code

A single HTTPS GET to `https://api.kimi.com/coding/v1/usages` with the user's OAuth bearer token. This is the same endpoint Kimi Code CLI's interactive `/status` slash command queries. If `KIMI_CODE_BASE_URL` is set, lazyagent appends `/usages` to that base URL instead.

The OAuth token is read in this priority order:

1. **`KIMI_CODE_OAUTH_TOKEN`** environment variable — useful for CI or for overriding the on-disk credential file
2. **`~/.kimi-code/credentials/kimi-code.json`** — the file Kimi Code CLI writes after login

lazyagent does **not** refresh Kimi OAuth tokens. If the access token has expired or been rejected, the command surfaces the server's `401` and tells you to run `kimi login` or open Kimi Code CLI again.

The response carries a top-level `usage` quota plus zero or more rolling `limits[]` windows. lazyagent maps `usage` to a weekly window and maps each `limits[]` entry by its advertised duration, for example `300` minutes becomes the `5-hour` window. Absolute quota counts and the parallelism cap, when present, appear in the `Source:` line.

### Cursor

Cursor is the odd one out: it's an IDE, not a CLI, so its usage lives in the web dashboard rather than a `/status` endpoint. lazyagent reads it the way Cursor's own dashboard does — with the session token Cursor stores locally.

Unlike the others, there is no OAuth file and no bearer env var. lazyagent reads one value straight from Cursor's local `state.vscdb` (the same SQLite database it already uses for Cursor session monitoring): **`cursorAuth/accessToken`**, the JWT session token. lazyagent decodes its `sub` claim to recover the user id and rebuilds the `WorkosCursorSessionToken=<userId>%3A%3A<token>` cookie the dashboard sends. The plan name (`pro`, `pro_plus`, `ultra`, …) comes from the API response's `membershipType` field instead of a separate database read.

With that cookie it makes one HTTPS call to `cursor.com`: `GET /api/usage-summary`, the same endpoint the dashboard uses for its usage headline.

**Two pools, two rows.** Cursor meters spend into an **Auto / Composer** pool and a **usage-based API** pool, each against its own allowance, and reports each as its own percentage. lazyagent shows both: `Cursor Models` uses `autoPercentUsed`, `Cursor API` uses `apiPercentUsed`. They share one monthly window bounded by `billingCycleStart` and `billingCycleEnd`, and both appear or disappear together since one call produces both.

**Why `Cursor Models` reads lower than Cursor's UI.** When Auto is the selected model, Cursor's own dashboard shows the *combined* figure (`totalPercentUsed`), not the Auto pool's own utilization. lazyagent deliberately shows `autoPercentUsed`: with two separate rows, using the combined total would double-count the API spend the second row already reports.

**No dollar amounts.** The endpoint returns percentages, not the per-pool split in currency. It does carry `plan.used` / `plan.limit`, but those are not the denominators of any of the three percentages, so rendering them would mislead. The percentage comes straight from Cursor — there is no plan table to keep in sync and nothing to override.

**`CURSOR_INCLUDED_USD` is gone.** Earlier versions read this environment variable to override a hardcoded per-plan dollar table used to compute a single Cursor row. That table and the override are both removed now that Cursor supplies the percentage directly — lazyagent no longer reads `CURSOR_INCLUDED_USD`, so any value you have exported is silently ignored.

## When an agent isn't installed

All providers are optional. The command's behavior depends on which agents have a detectable footprint on this machine — for Claude that's an OAuth token in any of the supported sources, for Codex it's a ChatGPT OAuth token in `~/.codex/auth.json` (or `CODEX_OAUTH_TOKEN`), for Grok it's an OAuth token in `~/.grok/auth.json` (or `GROK_OAUTH_TOKEN`), for Kimi it's an OAuth token in `~/.kimi-code/credentials/kimi-code.json` (or `KIMI_CODE_OAUTH_TOKEN`), and for Cursor it's a session token in its local `state.vscdb` (present once you've signed in to Cursor).

| State | Default (`--agent all`) | `--agent claude` | `--agent codex` | `--agent grok` | `--agent kimi` | `--agent cursor` |
|-------|-------------------------|------------------|-----------------|----------------|----------------|------------------|
| All installed | All reports printed | Claude printed | Codex printed | Grok printed | Kimi printed | Cursor printed |
| Subset installed | Installed providers printed, others silently skipped | Claude printed or error | Codex printed or error | Grok printed or error | Kimi printed or error | Cursor printed or error |
| None installed | Single guidance message on stderr, exit 1 | Friendly error, exit 1 | Friendly error, exit 1 | Friendly error, exit 1 | Friendly error, exit 1 | Friendly error, exit 1 |

The default `--agent all` mode is forgiving: a missing agent is not an error, it just doesn't show up. Explicit `--agent X` is strict: if you asked for it, missing it is an error worth surfacing.

**Cursor signed in, but with no budget to pace.** There's a second skip case unique to Cursor: you're signed in, but Cursor reports the account as unlimited (`isUnlimited`) or returns no enabled usage plan. Both rows are skipped together — silently under `--agent all`, with an actionable message under `--agent cursor`. Plans outside the consumer tiers (Business, Enterprise, Teams) no longer need any configuration: Cursor computes the percentage for them too.

## Disclaimers (Claude Code, Grok, Kimi, Cursor)

`/api/oauth/usage` is **not** part of Anthropic's documented public API. As of this writing it is used internally by Claude Code's `/status` UI and is subject to:

- **Aggressive rate limiting** — the endpoint will reject sustained polling with HTTP 429. lazyagent is intentionally on-demand only and does not poll.
- **No stability guarantee** — the endpoint, response shape, or beta header may change without notice. lazyagent fails gracefully when this happens (clear error, exit code 1).
- **Subscription scope** — the endpoint reflects the limits of the Claude.ai Pro/Max plan associated with the OAuth token. API-key users (Console, Bedrock, Vertex) won't see meaningful data here.

If you're distributing lazyagent or building automation on top of it, prefer running `lazyagent limits` only when a human has asked for it. Don't put it in a `watch` loop.

For **Grok**, `/v1/billing` is similarly **not** part of xAI's documented public API. As of this writing it is used internally by the Grok CLI's `/usage show` slash command and is subject to:

- **No stability guarantee** — endpoint path, response shape, and field names may change without notice. lazyagent fails gracefully (clear error, exit code 1) when this happens.
- **Subscription scope** — the response reflects the billing plan associated with the OAuth token (SuperGrok and similar). Users without a billing plan, or pure API-key users on `api.x.ai`, won't see meaningful data here.
- **Bearer reuse** — lazyagent sends the same JWT the Grok CLI uses. Treat it as a credential; the same caveats about token-rotation and revocation apply.

For **Kimi**, `/coding/v1/usages` is similarly not documented as a public API. As of this writing it is used internally by Kimi Code CLI's `/status` slash command and is subject to:

- **No stability guarantee** — endpoint path, response shape, and field names may change without notice. lazyagent fails gracefully when this happens.
- **Subscription scope** — the response reflects the plan associated with the Kimi Code OAuth token.
- **Bearer reuse** — lazyagent sends the same access token Kimi Code CLI uses, but does not refresh it. Treat it as a credential.

For **Cursor**, `/api/usage-summary` on `cursor.com` is not documented as a public API. As of this writing it is used internally by the Cursor dashboard's usage headline and is subject to:

- **No stability guarantee** — the endpoint path, the response shape, and the `autoPercentUsed` / `apiPercentUsed` fields may change without notice. lazyagent fails gracefully when this happens.
- **Vendor-computed percentages** — the figures come from Cursor's own `autoPercentUsed` / `apiPercentUsed`, so they track whatever allowances Cursor applies to your plan. If Cursor renames or restructures those fields, lazyagent fails gracefully rather than reporting a stale number.
- **Session-token reuse** — lazyagent reads the session token from Cursor's local `state.vscdb` and sends it as the dashboard's cookie. Treat it as a credential; it is not refreshed.

The "don't poll" guidance applies equally to Grok, Kimi, and Cursor: run `lazyagent limits` interactively, not in a `watch` loop.

## Exit codes

| Code | Meaning |
|------|---------|
| `0` | All requested agents succeeded |
| `1` | At least one agent failed (token missing, endpoint error, Codex login missing, …) — details on stderr |
| `2` | Invalid flags (e.g. unknown `--agent` value) |

Even on partial failure (`1`), the successful agents' output is printed to stdout. Errors go to stderr with a `Error (claude): …` / `Error (codex): …` / `Error (grok): …` / `Error (kimi): …` / `Error (cursor): …` prefix, so you can pipe stdout to a parser without losing the error context. With `--json`, errors are part of the stdout object under `errors` and nothing is written to stderr; the exit code follows the same rules.

## Environment

| Variable | Effect |
|----------|--------|
| `CLAUDE_CODE_OAUTH_TOKEN` | Override the OAuth token for the Claude call. Used in priority before the macOS keychain or the credentials file |
| `GROK_OAUTH_TOKEN` | Override the OAuth token for the Grok call. Used in priority before `~/.grok/auth.json` |
| `KIMI_CODE_OAUTH_TOKEN` | Override the OAuth token for the Kimi call. Used in priority before `~/.kimi-code/credentials/kimi-code.json` |
| `KIMI_CODE_BASE_URL` | Override the Kimi Code API base URL. lazyagent appends `/usages` |

## See also

- [Roadmap](../reference/roadmap.md) — shipped features per version
- [`lazyagent prune`](prune.md) — delete chat files (destructive, complementary)
- [`lazyagent compact`](compact.md) — shrink chat files in place (destructive, complementary)
