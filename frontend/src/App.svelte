<script lang="ts">
  import { onMount } from "svelte";
  import {
    sessions,
    selectedId,
    selectedDetail,
    windowMinutes,
    activeCount,
    activityFilter,
    searchQuery,
  } from "./lib/stores";
  import SessionList from "./lib/SessionList.svelte";
  import SessionDetail from "./lib/SessionDetail.svelte";
  import * as SessionService from "./bindings/github.com/nahime0/lazyagent/internal/tray/sessionservice";
  import { Events } from "@wailsio/runtime";

  let showDetail = $derived($selectedId !== null);
  let searching = $state(false);

  async function loadSessions() {
    try {
      const items = await SessionService.GetSessions();
      $sessions = items || [];
    } catch {
      // Fallback for dev mode without Go backend
    }
  }

  async function loadDetail(id: string) {
    try {
      const detail = await SessionService.GetSessionDetail(id);
      $selectedDetail = detail as any;
    } catch {
      $selectedDetail = null;
    }
  }

  $effect(() => {
    const id = $selectedId;
    if (id) {
      loadDetail(id);
    } else {
      $selectedDetail = null;
    }
  });

  function handleKeydown(e: KeyboardEvent) {
    if (searching) {
      if (e.key === "Escape") {
        e.preventDefault();
        searching = false;
        $searchQuery = "";
        SessionService.SetSearchQuery("").catch(() => {});
        loadSessions();
      }
      return;
    }

    if (e.key === "Escape") {
      if (showDetail) {
        e.preventDefault();
        $selectedId = null;
      }
    } else if (e.key === "/") {
      e.preventDefault();
      searching = true;
    } else if (e.key === "f") {
      e.preventDefault();
      cycleFilter();
    } else if (e.key === "+" || e.key === "=") {
      e.preventDefault();
      adjustWindow(10);
    } else if (e.key === "-") {
      e.preventDefault();
      adjustWindow(-10);
    }
  }

  function handleSearchInput(e: Event) {
    const target = e.target as HTMLInputElement;
    $searchQuery = target.value;
    SessionService.SetSearchQuery(target.value).catch(() => {});
    loadSessions();
  }

  const allFilters = ["", "idle", "waiting", "thinking", "compacting", "reading", "writing", "running", "searching", "browsing", "spawning"];

  function cycleFilter() {
    const idx = allFilters.indexOf($activityFilter);
    $activityFilter = allFilters[(idx + 1) % allFilters.length];
    SessionService.SetActivityFilter($activityFilter).catch(() => {});
    loadSessions();
  }

  function adjustWindow(delta: number) {
    const next = Math.max(10, Math.min(480, $windowMinutes + delta));
    $windowMinutes = next;
    SessionService.SetWindowMinutes(next).catch(() => {});
    loadSessions();
  }

  onMount(() => {
    loadSessions();

    SessionService.GetWindowMinutes().then((m) => {
      $windowMinutes = m;
    }).catch(() => {});

    Events.On("sessions:updated", () => {
      loadSessions();
      if ($selectedId) loadDetail($selectedId);
    });
  });
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex flex-col h-screen bg-surface">
  <!-- Header -->
  <header class="flex items-center justify-between px-3 py-2 bg-surface border-b border-border drag-region">
    <div class="flex items-center gap-2 no-drag">
      <h1 class="text-[14px] font-bold text-accent">lazyagent</h1>
      <span class="text-[11px] text-subtext">
        {$activeCount} active
      </span>
    </div>
    <div class="flex items-center gap-2 no-drag">
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
    </div>
  </header>

  <!-- Search bar -->
  {#if searching}
    <div class="px-3 py-1.5 bg-surface border-b border-border">
      <input
        type="text"
        class="w-full bg-transparent text-text text-[13px] outline-none placeholder-subtext"
        placeholder="Search sessions..."
        value={$searchQuery}
        oninput={handleSearchInput}
      />
    </div>
  {/if}

  <!-- Content -->
  <div class="flex-1 flex min-h-0">
    {#if showDetail}
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
  <footer class="flex items-center justify-center gap-3 px-3 py-1 bg-surface border-t border-border text-[10px] text-subtext">
    <span><kbd class="text-text/60">j/k</kbd> navigate</span>
    <span><kbd class="text-text/60">/</kbd> search</span>
    <span><kbd class="text-text/60">f</kbd> filter</span>
    <span><kbd class="text-text/60">+/−</kbd> window</span>
    <span><kbd class="text-text/60">r</kbd> rename</span>
    <span><kbd class="text-text/60">esc</kbd> back</span>
  </footer>
</div>
