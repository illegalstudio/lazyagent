---
title: "Compact session files"
description: "Rewrite session JSONLs in place, truncating bulky tool outputs and thinking blocks while preserving the conversation."
sidebar:
  order: 2
---

`lazyagent compact` **shrinks** session files in place without deleting them. Tool outputs, embedded images, and long thinking blocks are truncated above a threshold; the message graph, prompts, and tool call metadata are preserved so every compacted session stays resumable with the originating agent.

For *deleting* entire sessions, see [Prune](prune.md) instead.

## Synopsis

```
lazyagent compact  [--days N] [--agent LIST]
                   [--threshold-kb N] [--min-size-kb N]
                   [--dry-run | --dry-run-verbose]
                   [--no-backup] [--yes]
```

All flags are optional. With no flags, compact opens the interactive agent picker, scans every supported agent, and prompts for confirmation before rewriting anything.

## Flags

| Flag | Type | Default | Summary |
|------|------|---------|---------|
| `--days N` | int | `0` (unset) | Only compact sessions idle more than N days |
| `--agent LIST` | string | *unset* | Comma-separated subset: `claude,pi,codex`. Empty opens the picker |
| `--threshold-kb N` | int | `10` | Truncate JSON string values larger than N KiB |
| `--min-size-kb N` | int | `512` | Skip files smaller than N KiB |
| `--dry-run` | bool | `false` | Print a grouped summary, rewrite nothing |
| `--dry-run-verbose` | bool | `false` | Print one row per file, rewrite nothing |
| `--no-backup` | bool | `false` | Do not write `.bak` sidecars before rewriting |
| `--yes` | bool | `false` | Skip the destructive-op disclaimer |

## Quick reference

```bash
lazyagent compact                                     # interactive picker + dry-run summary
lazyagent compact --agent claude --dry-run            # preview group totals before/after
lazyagent compact --agent claude --dry-run-verbose    # one row per file
lazyagent compact --agent claude --days 14            # only sessions idle ≥14 days
lazyagent compact --threshold-kb 20                   # looser cut (default 10 KiB)
lazyagent compact --min-size-kb 2048                  # only files ≥2 MiB (default 512 KiB)
lazyagent compact --no-backup                         # skip the .bak sidecar
lazyagent compact --yes                               # skip the destructive-op disclaimer
```

## How it works

Each JSONL line is parsed, walked, and re-serialized. Where a string value exceeds the threshold, lazyagent keeps a prefix (minimum 256 bytes, typically `threshold / 10`) and appends a marker:

```
[truncated by lazyagent compact — was 47123 bytes, kept first 4096]
```

The full rewrite is validated: **the line count before and after must match**, otherwise the rewrite is aborted and the original file is left untouched. For extra safety a `.bak` sidecar is written by default; pass `--no-backup` to skip it.

Files where the rewrite wouldn't actually shrink the file (usually because JSON map-key re-ordering added a handful of bytes but nothing was truncated) are silently left alone — no needless `.bak`, no wasted I/O.

## Thresholds

`--threshold-kb N` (default **10**) sets the maximum size for a single JSON string value. Beyond that, the value is truncated. Thinking blocks get a **2× budget** because they carry genuine model reasoning that's more expensive to lose than a tool output snapshot.

`--min-size-kb N` (default **512**) skips files smaller than that many KiB — compacting a 40 KB transcript saves nothing meaningful.

Tuning guide:

| Goal | Recommended |
|------|-------------|
| Free up the most space, lose verbose tool output | `--threshold-kb 5` |
| Conservative cut, keep short tool results readable | `--threshold-kb 20` |
| Only hit whales (>2 MiB transcripts) | `--min-size-kb 2048` |
| Aggressive sweep of everything >100 KiB | `--min-size-kb 100 --threshold-kb 10` |

## Agent selection

Same interactive picker as `prune` when `--agent` is omitted — see [Prune: agent selection](prune.md#agent-selection) for the keybindings. Pass `--agent claude,codex,pi` to skip the picker.

## What gets truncated

Each agent has its own set of field paths. Only oversized values are touched; short strings (UUIDs, timestamps, tool names, signatures under threshold) are left alone.

### Claude Code

- `toolUseResult.stdout`, `toolUseResult.stderr` — bash output
- `toolUseResult.originalFile` — pre-edit file snapshots
- `toolUseResult.file.content` — Read tool results
- `toolUseResult.content[].text` — structured tool payloads
- `message.content[].thinking` — extended-thinking blocks (2× threshold)
- `message.content[].text`, `message.content[].content` — rare large assistant/user content
- `message.content[].source.data` — base64-encoded images (replaced with a short marker rather than truncated)
- Same fields under `data.message.message.content[]` for progress-type entries

### pi

- `message.content[].text` — text blocks
- `message.content[].thinking` (2× threshold) — thinking blocks
- `message.content[].thinkingSignature` — cryptographic attestation
- `message.content[].arguments` — tool call arguments JSON
- `message.details.truncation.content` — post-compaction snapshots
- `summary` — compaction summaries, only when they grow beyond 4× threshold (normally small and semantically useful)

### Codex

- `payload.output` — function_call_output bodies
- `payload.result` — alternative result payload variant
- `payload.message` — long agent_message payloads
- `payload.arguments` — function call arguments
- `payload.content[].text` / `input_text` / `output_text` — message content blocks

## Dry runs

Same two flags as `prune` — `--dry-run` for grouped totals, `--dry-run-verbose` for per-file rows. Both show the **before/after size and the exact reclaimable bytes** based on a simulated rewrite:

```
AGENT    PROJECT                                 FILES  BEFORE     AFTER     SAVED
claude   /Users/me/projects/big-app              6      142.1 MiB  86.7 MiB  55.3 MiB
claude   /Users/me/projects/mid-app              3      10.4 MiB   5.9 MiB   4.5 MiB
pi       /Users/me/projects/pi-exp               2      2.1 MiB    1.0 MiB   1.1 MiB
…
Total: 41 file(s) — 307.5 MiB → 220.9 MiB (86.6 MiB reclaimed).
```

## Confirmation

Real runs print the summary (with a numbered `#` column next to every project group), then the disclaimer box, then a prompt that accepts three forms:

```
Compact N file(s)? Enter y for all, a row # to target a single project, or N to abort [y/N/#]:
```

| Input | Effect |
|-------|--------|
| `y` / `yes` | Compact every candidate in the table |
| Row number (1-based, from the `#` column) | Compact only the files in that group |
| `n` / empty / anything else / EOF | Abort without rewriting anything |

Out-of-range numbers abort. When you pick a row, lazyagent prints a one-line confirmation (`Selected row 3: claude  /path/to/project  (4 file(s))`) before the rewrite starts.

The disclaimer and prompt are both skipped with `--yes`, which always acts on every candidate — the per-row picker is interactive only.

## Safety

- **Line-count validation** — the rewrite is aborted if the line count changes, leaving the original file untouched.
- **`.bak` sidecar** — written by default before each rewrite. Pass `--no-backup` to skip.
- **File mode preserved** — a 0600 transcript stays 0600 after compaction; no quiet permission widening.
- **Active sessions** (touched in the last 5 minutes) are skipped.
- **Sub-agent transcripts** are skipped to avoid breaking the parent's file.
- **Path guard** refuses to rewrite anything outside the known agent roots.
- **No-op guard** — if a simulated rewrite wouldn't actually shrink the file, it's left alone.

## Restoring from a backup

Every rewrite (unless `--no-backup`) produces a `<filename>.jsonl.bak` sibling. To undo a compaction:

```bash
mv session.jsonl.bak session.jsonl
```

## Supported agents

- **claude** (Claude Code CLI and Desktop share the same JSONL format)
- **pi**
- **codex**

Not supported:

- **Amp** — local files are re-synced from the remote; rewriting them gets overwritten on the next sync.
- **Cursor** and **OpenCode** — sessions live inside third-party SQLite databases. Rewriting their internals is deferred to a future version.

## Examples

```bash
# Default sweep with an interactive picker
lazyagent compact

# Preview what would happen for Claude only — no writes
lazyagent compact --agent claude --dry-run-verbose

# Aggressive cut: everything ≥100 KiB, with a tight 5 KiB threshold
lazyagent compact --min-size-kb 100 --threshold-kb 5 --agent claude

# Conservative cut: only old, only big, keep everything resumable
lazyagent compact --days 14 --min-size-kb 2048 --threshold-kb 20

# Script-friendly run: no prompt, no .bak, log the summary
lazyagent compact --agent claude --days 7 --yes --no-backup > ~/.local/state/la-compact.log 2>&1

# Undo a specific compaction
mv path/to/session.jsonl.bak path/to/session.jsonl
```

See [Recipes](../usage/recipes.md) for the full "weekly tidy-up" and "reclaim disk space aggressively" walkthroughs.

## Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success (including "nothing to compact") |
| `1`  | At least one rewrite failed |
| `2`  | Invalid flags or combinations |
