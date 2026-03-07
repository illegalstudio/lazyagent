<script lang="ts">
  import { sessions, selectedId, activityColor } from "./stores";
  import Sparkline from "./Sparkline.svelte";
  import ActivityBadge from "./ActivityBadge.svelte";

  function select(id: string) {
    $selectedId = id;
  }

  function handleKeydown(e: KeyboardEvent) {
    const list = $sessions;
    if (!list.length) return;

    const idx = list.findIndex((s) => s.sessionId === $selectedId);

    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      const next = Math.min(idx + 1, list.length - 1);
      $selectedId = list[next].sessionId;
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      const prev = Math.max(idx - 1, 0);
      $selectedId = list[prev].sessionId;
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex flex-col overflow-y-auto h-full">
  {#each $sessions as session (session.sessionId)}
    {@const color = activityColor(session.activity)}
    <button
      class="flex items-center gap-2 px-3 py-2 text-left transition-colors duration-75 border-l-2 no-drag
        {session.sessionId === $selectedId
          ? 'bg-surface-hover border-accent'
          : 'border-transparent hover:bg-surface-hover/50'}"
      onclick={() => select(session.sessionId)}
    >
      <!-- Activity dot -->
      <span
        class="shrink-0 h-2 w-2 rounded-full"
        class:animate-pulse-dot={session.isActive}
        style="background: {color};"
      ></span>

      <!-- Name + sparkline -->
      <div class="flex-1 min-w-0">
        <div class="truncate text-[13px] font-medium text-text">
          {session.shortName}
        </div>
        <div class="mt-0.5">
          <Sparkline data={session.sparklineData} {color} width={100} height={16} />
        </div>
      </div>

      <!-- Activity badge -->
      <div class="shrink-0">
        <ActivityBadge activity={session.activity} isActive={session.isActive} />
      </div>
    </button>
  {:else}
    <div class="flex items-center justify-center h-full text-subtext text-sm">
      No sessions found
    </div>
  {/each}
</div>
