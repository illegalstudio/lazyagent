<script lang="ts">
  import {
    selectedId,
    activeCount,
    windowMinutes,
    activityFilter,
    searchQuery,
    searching,
    showLimits,
    limitsRefreshToken,
    updateVersion,
    isDetached,
    isPinned,
  } from "./stores";
  import {
    cycleFilter,
    adjustWindow,
    toggleDetach,
    togglePin,
    setSearch,
  } from "./actions";
  import SessionList from "./SessionList.svelte";
  import CloudStatus from "./CloudStatus.svelte";
  import SettingsModal from "./SettingsModal.svelte";
  import SessionDetail from "./SessionDetail.svelte";
  import LimitsPage from "./LimitsPage.svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  let showDetail = $derived($selectedId !== null);
  let settingsOpen = $state(false);
</script>

<div class="flex flex-col h-screen bg-surface">
  <!-- Header -->
  <header class="flex items-center justify-between px-3 py-2 bg-surface border-b border-border drag-region">
    <div class="flex items-center gap-2 no-drag">
      <button class="text-subtext hover:text-text" onclick={() => settingsOpen = true} title="Settings" aria-label="Settings">⚙</button>
      <h1 class="text-[14px] font-bold text-accent">lazyagent</h1>
      <span class="text-[11px] text-subtext">
        {$activeCount} active
      </span>
    </div>
    <div class="flex items-center gap-2 no-drag">
      <button
        class="rounded px-1.5 py-0.5 text-[11px] font-medium {$showLimits ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
        onclick={() => ($showLimits = !$showLimits)}
        title="Show limits (l)"
      >limits</button>
      {#if $activityFilter}
        <button
          class="rounded px-1.5 py-0.5 text-[11px] font-medium text-accent bg-accent/10 hover:bg-accent/20"
          onclick={cycleFilter}
        >
          {$activityFilter}
        </button>
      {/if}
      <span class="text-[11px] text-subtext">{$windowMinutes}m</span>
      <button
        class="text-subtext hover:text-text text-[14px] leading-none"
        onclick={() => adjustWindow(-10)}
        title="Decrease time window"
      >−</button>
      <button
        class="text-subtext hover:text-text text-[14px] leading-none"
        onclick={() => adjustWindow(10)}
        title="Increase time window"
      >+</button>
      {#if $isDetached}
        <button
          class="leading-none ml-1 text-[11px] font-medium rounded px-1 py-0.5 {$isPinned ? 'text-accent bg-accent/10' : 'text-subtext hover:text-text'}"
          onclick={togglePin}
          title={$isPinned ? "Unpin from top" : "Pin on top"}
        >pin</button>
      {/if}
      <button
        class="text-subtext hover:text-text text-[14px] leading-none ml-1"
        onclick={toggleDetach}
        title={$isDetached ? "Attach to tray" : "Detach to window"}
      >{$isDetached ? "\u2921" : "\u2922"}</button>
    </div>
  </header>

  <CloudStatus />

  <!-- Search bar -->
  {#if $searching}
    <div class="px-3 py-1.5 bg-surface border-b border-border">
      <input
        type="text"
        class="w-full bg-transparent text-text text-[13px] outline-none placeholder-subtext"
        placeholder="Search sessions..."
        value={$searchQuery}
        oninput={(e) => setSearch((e.target as HTMLInputElement).value)}
      />
    </div>
  {/if}

  <!-- Content -->
  <div class="flex-1 flex min-h-0">
    {#if $showLimits}
      <div class="flex-1 overflow-hidden">
        <LimitsPage refreshToken={$limitsRefreshToken} />
      </div>
    {:else if showDetail}
      <div class="w-[45%] border-r border-border overflow-hidden">
        <SessionList />
      </div>
      <div class="flex-1 overflow-hidden">
        <SessionDetail />
      </div>
    {:else}
      <div class="flex-1 overflow-hidden">
        <SessionList />
      </div>
    {/if}
  </div>

  <!-- Footer -->
  <footer class="px-3 py-1 bg-surface border-t border-border">
    {#if $updateVersion}
      <div class="flex items-center justify-center gap-1 text-[10px] text-accent pb-0.5">
        <span>↑ lazyagent {$updateVersion} available —</span>
        <button
          class="underline hover:text-text cursor-pointer"
          onclick={() => SessionService.OpenReleases()}
        >releases</button>
      </div>
    {/if}
    <div class="flex items-center justify-center gap-3 text-[10px] text-subtext">
      <span><kbd class="text-text/60">j/k</kbd> navigate</span>
      <span><kbd class="text-text/60">/</kbd> search</span>
      <span><kbd class="text-text/60">f</kbd> filter</span>
      <span><kbd class="text-text/60">l</kbd> limits</span>
      <span><kbd class="text-text/60">+/−</kbd> window</span>
      <span><kbd class="text-text/60">r</kbd> {$showLimits ? "refresh" : "rename"}</span>
      <span><kbd class="text-text/60">d</kbd> {$isDetached ? "attach" : "detach"}</span>
      <span><kbd class="text-text/60">esc</kbd> back</span>
    </div>
  </footer>
</div>

{#if settingsOpen}<SettingsModal onclose={() => settingsOpen = false} />{/if}
