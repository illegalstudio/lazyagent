<script lang="ts">
  import { sessions, selectedId, activityColor } from "./stores";
  import Sparkline from "./Sparkline.svelte";
  import ActivityBadge from "./ActivityBadge.svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  let renamingId = $state<string | null>(null);
  let renameValue = $state("");
  let renameInput = $state<HTMLInputElement | null>(null);

  function select(id: string) {
    $selectedId = id;
  }

  function startRename(session: { sessionId: string; customName: string; shortName: string }) {
    renamingId = session.sessionId;
    renameValue = session.customName || "";
    // Focus the input after it renders
    requestAnimationFrame(() => renameInput?.focus());
  }

  function confirmRename() {
    if (renamingId) {
      SessionService.SetSessionName(renamingId, renameValue.trim()).catch(() => {});
      renamingId = null;
      renameValue = "";
    }
  }

  function cancelRename() {
    renamingId = null;
    renameValue = "";
  }

  function handleRenameKey(e: KeyboardEvent) {
    e.stopPropagation(); // Prevent App.svelte from intercepting rename input keys
    if (e.key === "Enter") {
      e.preventDefault();
      confirmRename();
    } else if (e.key === "Escape") {
      e.preventDefault();
      cancelRename();
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (renamingId) return; // Don't navigate while renaming
    // Don't intercept keys when an input/textarea is focused (e.g. search box)
    const tag = (e.target as HTMLElement)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA") return;
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
      ondblclick={() => startRename(session)}
    >
      <!-- Activity dot -->
      <span
        class="shrink-0 h-2 w-2 rounded-full"
        class:animate-pulse-dot={session.isActive}
        style="background: {color};"
      ></span>

      <!-- Name + sparkline -->
      <div class="flex-1 min-w-0">
        {#if renamingId === session.sessionId}
          <input
            bind:this={renameInput}
            bind:value={renameValue}
            onkeydown={handleRenameKey}
            onblur={confirmRename}
            class="w-full bg-surface text-text text-[13px] font-medium px-1 py-0 rounded border border-accent outline-none"
            placeholder={session.shortName}
          />
        {:else}
          <div class="truncate text-[13px] font-medium text-text">
            {#if session.agent === "pi"}<span class="text-activity-spawning">π</span>
            {:else if session.agent === "opencode"}<span class="text-subtext">O</span>
            {:else if session.agent === "cursor"}<span class="text-subtext">C</span>
            {:else if session.source === "desktop"}<span class="text-accent">D</span>
            {/if}
            {session.customName || session.agentName || session.shortName}
          </div>
        {/if}
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
