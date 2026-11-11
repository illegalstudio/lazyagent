<script lang="ts">
  import { cloudStatus } from "./stores";
  import { toggleIdleCloud } from "./actions";
</script>

{#if $cloudStatus.configured || $cloudStatus.error}
  <div class="flex items-center gap-2 px-3 py-1.5 border-b border-border text-[11px] no-drag shrink-0" role="status" aria-live="polite">
    <div class="flex-1 min-w-0">
      <span class="text-accent font-medium">Cursor cloud</span>
      <span class="text-subtext"> · {$cloudStatus.count} loaded</span>
      {#if $cloudStatus.loading}<span class="text-subtext"> · refreshing…</span>{/if}
      {#if $cloudStatus.error}<div class="text-activity-waiting break-words mt-0.5">{$cloudStatus.error}</div>{/if}
    </div>
    <button
      class="shrink-0 rounded px-2 py-1 text-[10px] {$cloudStatus.showIdle ? 'text-accent bg-accent/10' : 'text-subtext bg-surface-hover hover:text-text'}"
      aria-pressed={$cloudStatus.showIdle}
      onclick={() => toggleIdleCloud().catch(() => {})}
      title="Include idle cloud agents, including older runs (a)"
    >Idle cloud</button>
  </div>
{/if}
