<script lang="ts">
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";
  import {
    View,
    Severity,
    type WindowView,
  } from "../bindings/github.com/illegalstudio/lazyagent/internal/limits/models";

  interface Props {
    refreshToken?: number;
    closeHint?: string;
  }

  let { refreshToken = 0, closeHint = "esc / l to close" }: Props = $props();
  let loading = $state(true);
  let view = $state<View | null>(null);
  let fetchedAt = $state(0);
  let now = $state(Date.now());

  $effect(() => {
    refreshToken;
    let cancelled = false;
    loading = true;

    SessionService.GetLimits()
      .then((v) => {
        if (!cancelled) {
          view = v;
          fetchedAt = Date.now();
          now = fetchedAt;
          loading = false;
        }
      })
      .catch(() => {
        if (!cancelled) {
          view = new View({ Reports: [], Summary: [], Available: false });
          fetchedAt = Date.now();
          loading = false;
        }
      });

    return () => {
      cancelled = true;
    };
  });

  // The reset countdowns and the "updated ago" line tick on their own so the
  // panel stays truthful between refreshes.
  $effect(() => {
    const id = setInterval(() => (now = Date.now()), 1000);
    return () => clearInterval(id);
  });

  const clamp = (v: number, lo: number, hi: number) => Math.max(lo, Math.min(hi, v));

  function percentText(value: number): string {
    const rounded = Math.round(value * 10) / 10;
    return `${rounded.toFixed(1).replace(/\.0$/, "")}%`;
  }

  function ratioText(value: number): string {
    if (value >= 10) return `${Math.round(value)}×`;
    return `${(Math.round(value * 100) / 100).toFixed(2).replace(/\.?0+$/, "")}×`;
  }

  // Seconds → "6d 17h", "1h 19m", "12m", "45s".
  function formatDuration(seconds: number): string {
    const total = Math.max(0, Math.floor(seconds));
    if (total < 60) return `${total}s`;
    const days = Math.floor(total / 86400);
    const hours = Math.floor((total % 86400) / 3600);
    const minutes = Math.floor((total % 3600) / 60);
    if (days > 0) return `${days}d ${hours}h`;
    if (hours > 0) return `${hours}h ${minutes}m`;
    return `${minutes}m`;
  }

  // Live countdown when the window carries a reset instant, otherwise the
  // relative string the backend already computed.
  function resetText(w: WindowView): string {
    if (w.ResetUnix > 0) {
      const remaining = w.ResetUnix * 1000 - now;
      return remaining > 0 ? `resets in ${formatDuration(remaining / 1000)}` : "reset due";
    }
    return w.ResetRelative ? `resets ${w.ResetRelative}` : "";
  }

  function agoText(): string {
    if (fetchedAt <= 0) return "";
    const seconds = Math.max(0, (now - fetchedAt) / 1000);
    return seconds < 10 ? "updated just now" : `updated ${formatDuration(seconds)} ago`;
  }

  function sevText(sev: Severity): string {
    switch (sev) {
      case Severity.SevOK: return "text-activity-writing";
      case Severity.SevInfo: return "text-activity-thinking";
      case Severity.SevWarn: return "text-activity-running";
      case Severity.SevDanger: return "text-activity-spawning";
      default: return "text-text";
    }
  }

  function sevBar(sev: Severity): string {
    switch (sev) {
      case Severity.SevOK: return "bg-activity-writing";
      case Severity.SevInfo: return "bg-activity-thinking";
      case Severity.SevWarn: return "bg-activity-running";
      case Severity.SevDanger: return "bg-activity-spawning";
      default: return "bg-subtext";
    }
  }

  // Pace as lazyagent defines it: below 0.85× of the elapsed share is
  // underutilizing, above 1.15× is overutilizing.
  function paceClass(label: string): string {
    if (label === "overutilizing") return "text-activity-spawning border-activity-spawning/60";
    if (label === "on track") return "text-activity-writing border-activity-writing/60";
    return "text-subtext border-border";
  }

  function paceDetail(w: WindowView): string {
    if (!w.PaceKnown) return "";
    return `${ratioText(w.PaceRatio)} of expected ${percentText(w.ExpectedPercent)}`;
  }
</script>

<div class="flex h-full flex-col bg-surface">
  <div class="min-h-0 flex-1 overflow-auto p-3">
    {#if loading}
      <div class="py-8 text-center text-[13px] text-subtext">Reading rate limits…</div>
    {:else if !view || !view.Available}
      <div class="py-8 text-center text-[13px] text-subtext">
        No supported agents detected.<br />
        Log in to Claude Code, Codex, Grok, Kimi, or Cursor and refresh.
      </div>
    {:else}
      <div class="grid grid-cols-[repeat(auto-fill,minmax(260px,1fr))] gap-2.5">
        {#each view.Reports as report}
          <div class="rounded-lg border border-border bg-surface-hover/30 p-2.5">
            <div class="mb-2 text-[11px] font-bold uppercase tracking-wide text-accent">
              {report.Provider}
            </div>

            {#if report.Windows.length === 0}
              <div class="text-[11px] text-subtext">No windows reported.</div>
            {/if}

            <div class="flex flex-col gap-3">
              {#each report.Windows as w}
                <div class="flex flex-col gap-1.5">
                  <div class="flex items-baseline gap-2">
                    <span class="truncate text-[12px] text-text">{w.Label} window</span>
                    <span class="ml-auto shrink-0 text-[12px] font-bold tabular-nums {sevText(w.UsedSeverity)}">
                      {percentText(w.UsedPercent)} used
                    </span>
                  </div>

                  <div class="relative h-1.5 w-full rounded-full bg-border/50">
                    <div
                      class="absolute inset-y-0 left-0 rounded-full transition-[width] duration-200 ease-out {sevBar(w.UsedSeverity)}"
                      style="width: {clamp(w.UsedPercent, 0, 100)}%"
                    ></div>
                    {#if w.PaceKnown}
                      <!-- Where a perfectly linear consumer would be right now. -->
                      <div
                        class="absolute -top-1 h-3.5 w-0.5 rounded-full bg-text/80 ring-1 ring-surface"
                        style="left: calc({clamp(w.ExpectedPercent, 0, 100)}% - 1px)"
                        title="Expected pace {percentText(w.ExpectedPercent)}"
                      ></div>
                    {/if}
                  </div>

                  <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
                    <span class="rounded border px-1.5 py-px text-[10px] font-bold {paceClass(w.PaceKnown ? w.PaceLabel : '')}">
                      {w.PaceKnown ? w.PaceLabel : "pace unknown"}
                    </span>
                    {#if w.PaceKnown}
                      <span class="truncate text-[10px] text-subtext tabular-nums">{paceDetail(w)}</span>
                    {/if}
                    {#if resetText(w)}
                      <span
                        class="ml-auto shrink-0 text-[10px] text-subtext tabular-nums"
                        title={w.ResetAbsolute}
                      >{resetText(w)}</span>
                    {/if}
                  </div>
                </div>
              {/each}
            </div>
          </div>
        {/each}
      </div>
    {/if}
  </div>

  <div class="flex shrink-0 items-center gap-2 border-t border-border px-3 py-1.5 text-[10px] text-subtext">
    <span class="truncate">{loading ? "refreshing…" : agoText()}</span>
    <span class="ml-auto shrink-0">
      r to refresh{#if closeHint} · {closeHint}{/if}
    </span>
  </div>
</div>
