# Grok Compact — Phase 0 Resume-Verification Findings

> **Date:** 2026-05-20
> **Status:** COMPLETE — all four candidate files verified SAFE. Task 8 may proceed at full scope.
> Companion to `2026-05-20-grok-agent-integration.md` Task 7.

## Question

`lazyagent compact` truncates bulky payloads in a session while keeping it
resumable by the originating agent. Before writing the Grok rewriter, we must
know **which files inside a Grok session directory can be truncated without
breaking `grok` resume.**

## Method

The verification ran against a real local Grok install (`~/.grok`, Grok Build
TUI). Originals were never touched — all work was on a disposable session.

1. **Created a disposable session.** Ran `grok -p "<task>" --cwd /tmp/grok-phase0`
   with a task that exercised every relevant file type: a shell command (→
   `terminal/*.log`, a `tool_result`), creating a file (→ `rewind_points.jsonl`
   file snapshot), editing it (→ another snapshot, `hunk_records.jsonl`), and a
   codeword (`MANGO-7731`) planted in the conversation as a coherence probe.
   Resulting session: `chat_history.jsonl` 39 KiB (5 `tool_result` entries),
   `updates.jsonl` 142 KiB (33 lines), one `terminal/*.log`, a
   `rewind_points.jsonl` with embedded file snapshots.
2. **Snapshotted** the pristine session directory for restore between runs.
3. **Baseline resume** of the unmodified session.
4. **Run B** — restored the snapshot, then truncated all four candidate files,
   and resumed again.

Resume command (headless, single-turn):

```
grok --resume <SESSION_ID> --cwd <cwd> \
     -p "What codeword did I ask you to remember earlier? Reply with just the codeword." \
     --output-format plain --no-alt-screen --disable-web-search
```

**SAFE criterion:** resume exits 0 and the model coherently recalls the
codeword (proves the session loaded and the conversation continued).

## Results

| Run | Mutation | Outcome |
|-----|----------|---------|
| Baseline | none (unmodified session) | exit 0, recalled `MANGO-7731` |
| Run B | **all four files truncated at once** | exit 0, recalled `MANGO-7731` |

Run B mutations:
- `updates.jsonl` — deep string truncation (every string value >200 chars cut to a marker), 142 KiB → 120 KiB, line count preserved (33).
- `chat_history.jsonl` — **every** `tool_result.content` replaced with `"[phase0-truncated tool output]"`, line count preserved (17). `tool_call_id` linkage untouched.
- `terminal/*.log` — truncated to 12 bytes.
- `rewind_points.jsonl` — embedded `file_snapshots`/`after_snapshots` `content` values replaced with a marker.

All rewritten files remained valid JSONL (every line independently parseable).

## Verdict

Because the **combined** truncation of all four files was tolerated, every
subset is necessarily tolerated too — no bisection was needed.

| File | Verdict | Notes |
|------|---------|-------|
| `updates.jsonl` | **SAFE** | ACP render/telemetry stream; deep string truncation tolerated, resume coherent. Highest-value target (~70% of session size in large sessions). |
| `chat_history.jsonl` → `tool_result.content` | **SAFE** | Resume coherent with all tool outputs gutted. This changes what the model sees on resume — the intended, accepted `compact` behavior, identical to Claude compact. Only `tool_result.content` is truncated; `user`/`assistant` text and `tool_call_id` linkage are left intact. |
| `terminal/*.log` | **SAFE** | Raw command-output capture; not consulted on resume. |
| `rewind_points.jsonl` (embedded snapshots) | **SAFE — with caveat** | Resume coherent. Caveat: gutting the embedded file snapshots disables Grok's *rewind* feature for that session. Acceptable and must be documented. |

## Caveats

- One session of moderate size was tested (`updates.jsonl` 142 KiB). Truncation
  tolerance is a structural property — whether Grok reads the file on resume —
  not size-dependent, so the result generalizes to the ~18 MB sessions seen in
  the wild.
- `stderr` showed unrelated `MCP`/`SSE` worker errors (`127.0.0.1:.../sse`
  connection refused) in **both** the baseline and Run B — a side MCP server,
  not caused by truncation.
- The test session was fully removed afterwards (directory, encoded-cwd parent,
  `session_search.sqlite` row, `worktrees.db` row).

## Decision for Task 8

Proceed at **full scope**, no narrowing:

- `grokBulkJSONL = ["updates.jsonl", "chat_history.jsonl", "rewind_points.jsonl"]`
- plus `terminal/*.log` raw-log truncation.
- `updates.jsonl` and `rewind_points.jsonl`: deep string truncation.
- `chat_history.jsonl`: truncate `tool_result.content` only; preserve line count and `tool_call_id` linkage.
- Document the `rewind_points.jsonl` caveat (rewind disabled for compacted sessions) in `docs/maintenance/compact.md`.
