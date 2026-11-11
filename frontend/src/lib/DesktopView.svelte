<script lang="ts">
  import {
    sessions, selectedId, activeCount, windowMinutes, activityFilter,
    searchQuery, showLimits, updateVersion,
    isPinned, cardDensity, detailWidth,
  } from "./stores";
  import type { CardDensity, SessionItem } from "./stores";
  import { formatCost } from "./stores";
  import {
    cycleFilter, adjustWindow, toggleDetach, togglePin, setSearch,
  } from "./actions";
  import SessionCard from "./SessionCard.svelte";
  import CloudStatus from "./CloudStatus.svelte";
  import DetailPanel from "./DetailPanel.svelte";
  import LimitsModal from "./LimitsModal.svelte";
  import ContextMenu from "./ContextMenu.svelte";
  import ShortcutsOverlay from "./ShortcutsOverlay.svelte";
  import SettingsModal from "./SettingsModal.svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  const densities: CardDensity[] = ["compact", "rich", "live"];
  const minCardWidth: Record<CardDensity, number> = { compact: 220, rich: 260, live: 300 };

  let searchEl = $state<HTMLInputElement | null>(null);
  let contextMenu = $state<{ session: SessionItem; x: number; y: number } | null>(null);
  let shortcutsOpen = $state(false);
  let settingsOpen = $state(false);
  let renameRequest = $state<string | null>(null);

  let windowCost = $derived($sessions.reduce((sum, s) => sum + s.costUsd, 0));

  function pickDensity(d: CardDensity) {
    $cardDensity = d;
    SessionService.SetCardDensity(d).catch(() => {});
  }

  function select(id: string) {
    $selectedId = $selectedId === id ? null : id;
  }

  function refresh() {
    SessionService.Refresh().catch(() => {});
  }

  function openContext(session: SessionItem, x: number, y: number) {
    contextMenu = { session, x, y };
  }

  // Detail panel resize: drag the handle on the panel's left edge.
  // Pointer capture keeps move/up events on the handle even when the
  // pointer leaves it; the width persists once, on release.
  let resizing = $state(false);
  let resizeStartX = 0;
  let resizeStartWidth = 0;

  function startResize(e: PointerEvent) {
    resizing = true;
    resizeStartX = e.clientX;
    // Baseline from the rendered width, not the store: a persisted value
    // wider than the current window's 60vw cap renders CSS-clamped, and
    // seeding from the store would make the first move jump.
    const panel = (e.target as HTMLElement).parentElement;
    resizeStartWidth = panel ? panel.getBoundingClientRect().width : $detailWidth;
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }

  function moveResize(e: PointerEvent) {
    if (!resizing) return;
    // 2000 mirrors the Go-side plausibility ceiling (NormalizeDetailWidth):
    // without it a wide-display drag past 2000 would persist as 400.
    const max = Math.min(2000, Math.floor(window.innerWidth * 0.6));
    $detailWidth = Math.min(max, Math.max(300, resizeStartWidth + (resizeStartX - e.clientX)));
  }

  function endResize() {
    if (!resizing) return;
    resizing = false;
    SessionService.SetDetailWidth($detailWidth).catch(() => {});
  }

  function resetWidth() {
    $detailWidth = 400;
    SessionService.SetDetailWidth(400).catch(() => {});
  }

  // Grid j/k navigation while the detail panel is open or closed.
  function handleKeydown(e: KeyboardEvent) {
    const tag = (e.target as HTMLElement)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA") return;
    if (e.key === "/") {
      e.preventDefault();
      searchEl?.focus();
      return;
    }
    if (e.key === "?") {
      e.preventDefault();
      shortcutsOpen = true;
      return;
    }
    const list = $sessions;
    if (!list.length) return;
    const idx = list.findIndex((s) => s.sessionId === $selectedId);
    if (e.key === "j" || e.key === "ArrowDown") {
      e.preventDefault();
      $selectedId = list[Math.min(idx + 1, list.length - 1)].sessionId;
    } else if (e.key === "k" || e.key === "ArrowUp") {
      e.preventDefault();
      $selectedId = list[Math.max(idx - 1, 0)].sessionId;
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

<div class="flex flex-col h-screen bg-[#11111b]">
  <!-- Toolbar -->
  <header class="flex items-center gap-2.5 px-4 py-2 bg-surface border-b border-border drag-region shrink-0">
    <div class="flex items-center gap-2 no-drag shrink-0">
      <h1 class="text-[14px] font-bold text-accent">lazyagent</h1>
      <span class="rounded-full bg-activity-waiting/15 text-activity-waiting text-[10px] font-semibold px-2 py-px">{$activeCount} active</span>
    </div>

    <div class="no-drag flex items-center gap-1.5 flex-initial w-56 bg-[#11111b] border border-border rounded-lg px-2.5 py-1 focus-within:border-accent">
      <svg viewBox="0 0 24 24" class="w-3 h-3 stroke-subtext shrink-0" fill="none" stroke-width="2.5" stroke-linecap="round"><circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/></svg>
      <input
        bind:this={searchEl}
        type="text"
        class="w-full bg-transparent text-text text-[12px] outline-none placeholder-subtext"
        placeholder="Search sessions…"
        value={$searchQuery}
        oninput={(e) => setSearch((e.target as HTMLInputElement).value)}
        onkeydown={(e) => { if (e.key === "Escape") { setSearch(""); (e.target as HTMLInputElement).blur(); e.stopPropagation(); } }}
      />
    </div>

    {#if $activityFilter}
      <button
        class="no-drag rounded-md px-2 py-1 text-[11px] font-medium text-accent bg-accent/10 hover:bg-accent/20"
        onclick={cycleFilter}
        title="Cycle activity filter (f)"
      >{$activityFilter}</button>
    {/if}

    <div class="flex-1 drag-region"></div>

    <div class="no-drag flex rounded-lg border border-border overflow-hidden">
      {#each densities as d}
        <button
          class="px-2.5 py-1 text-[11px] {$cardDensity === d ? 'bg-accent text-surface font-semibold' : 'text-subtext hover:text-text bg-[#11111b]'}"
          onclick={() => pickDensity(d)}
          title="Card density (⌘{densities.indexOf(d) + 1})"
        >{d}</button>
      {/each}
    </div>

    <div class="no-drag flex items-center rounded-lg border border-border bg-[#11111b] overflow-hidden text-[11px]">
      <button class="px-2 py-1 text-subtext hover:text-text" onclick={() => adjustWindow(-10)} title="Narrow the time window">−</button>
      <span class="px-1 text-subtext tabular-nums">{$windowMinutes}m</span>
      <button class="px-2 py-1 text-subtext hover:text-text" onclick={() => adjustWindow(10)} title="Widen the time window">+</button>
    </div>

    <button
      class="no-drag rounded-lg border px-2.5 py-1 text-[11px] {$showLimits ? 'border-accent bg-accent/10 text-accent font-semibold' : 'border-border bg-[#11111b] text-subtext hover:text-text'}"
      onclick={() => ($showLimits = true)}
      title="Show limits (⌘L)"
    >Limits</button>

    <button
      class="no-drag rounded-lg border border-border bg-[#11111b] px-2 py-1 text-subtext hover:text-text"
      onclick={refresh}
      title="Refresh sessions (⌘R)"
    >
      <svg viewBox="0 0 24 24" class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 12a9 9 0 0 1 9-9 9.75 9.75 0 0 1 6.74 2.74L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-9 9 9.75 9.75 0 0 1-6.74-2.74L3 16"/><path d="M8 16H3v5"/></svg>
    </button>

    <button
      class="no-drag rounded-lg border px-2 py-1 {$isPinned ? 'border-accent bg-accent text-surface' : 'border-border bg-[#11111b] text-subtext hover:text-text'}"
      onclick={togglePin}
      title={$isPinned ? "Unpin from top" : "Keep window on top"}
    >
      <svg viewBox="0 0 24 24" class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 17v5"/><path d="M9 10.76a2 2 0 0 1-1.11 1.79l-1.78.9A2 2 0 0 0 5 15.24V16a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1v-.76a2 2 0 0 0-1.11-1.79l-1.78-.9A2 2 0 0 1 15 10.76V6h1a2 2 0 0 0 0-4H8a2 2 0 0 0 0 4h1z"/></svg>
    </button>

    <button
      class="no-drag rounded-lg border border-border bg-[#11111b] px-2 py-1 text-subtext hover:text-text"
      onclick={() => (settingsOpen = true)}
      title="Settings"
    >
      <svg viewBox="0 0 24 24" class="w-3.5 h-3.5" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z"/><circle cx="12" cy="12" r="3"/></svg>
    </button>

    <button
      class="no-drag rounded-lg border border-border bg-[#11111b] px-2 py-1 text-subtext hover:text-text text-[13px] leading-none"
      onclick={toggleDetach}
      title="Attach to system tray (d)"
    >⤡</button>
  </header>

  <CloudStatus />

  <!-- Content -->
  <div class="flex-1 flex min-h-0">
    <div class="flex-1 min-w-0 overflow-y-auto p-3">
      {#if $sessions.length}
        <div
          class="grid gap-3"
          style="grid-template-columns: repeat(auto-fill, minmax({minCardWidth[$cardDensity]}px, 1fr));"
        >
          {#each $sessions as session (session.sessionId)}
            <SessionCard
              {session}
              density={$cardDensity}
              selected={session.sessionId === $selectedId}
              onselect={select}
              oncontext={openContext}
              {renameRequest}
              onrenamehandled={() => (renameRequest = null)}
            />
          {/each}
        </div>
      {:else}
        <div class="flex flex-col items-center justify-center h-full gap-3 text-subtext">
          <svg viewBox="0 0 24 24" class="w-12 h-12 opacity-40" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
            <path d="M3 7V5a2 2 0 0 1 2-2h2"/><path d="M17 3h2a2 2 0 0 1 2 2v2"/><path d="M21 17v2a2 2 0 0 1-2 2h-2"/><path d="M7 21H5a2 2 0 0 1-2-2v-2"/><circle cx="12" cy="12" r="1"/><path d="M18.944 12.33a1 1 0 0 0 0-.66 7.5 7.5 0 0 0-13.888 0 1 1 0 0 0 0 .66 7.5 7.5 0 0 0 13.888 0"/>
          </svg>
          <div class="text-[13px] font-medium text-text">No sessions in the last {$windowMinutes} minutes</div>
          <div class="text-[11.5px]">Agents appear here as soon as they run — press <kbd class="bg-surface-hover border border-border rounded px-1">+</kbd> to widen the window.</div>
        </div>
      {/if}
    </div>
    {#if $selectedId}
      <div
        class="relative shrink-0 min-h-0 max-w-[60vw]"
        style="width: {$detailWidth}px; min-width: 300px;"
      >
        <div
          class="absolute left-0 top-0 bottom-0 w-[5px] z-10 cursor-col-resize hover:bg-accent/30 {resizing ? 'bg-accent/30' : ''}"
          role="separator"
          aria-orientation="vertical"
          title="Drag to resize, double-click to reset"
          onpointerdown={startResize}
          onpointermove={moveResize}
          onpointerup={endResize}
          onpointercancel={endResize}
          ondblclick={resetWidth}
        ></div>
        <DetailPanel />
      </div>
    {/if}
  </div>

  <!-- Status bar -->
  <footer class="flex items-center gap-2 px-4 py-1 bg-surface border-t border-border shrink-0 text-[10.5px] text-subtext">
    <span>{$sessions.length} {$sessions.length === 1 ? "session" : "sessions"}</span>
    <span class="text-border">·</span>
    <span>{$activeCount} active</span>
    <span class="text-border">·</span>
    <span><span class="text-activity-reading font-medium">{formatCost(windowCost)}</span> in window</span>
    {#if $updateVersion}
      <span class="text-border">·</span>
      <button class="text-accent underline hover:text-text" onclick={() => SessionService.OpenReleases()}>
        {$updateVersion} available
      </button>
    {/if}
    <button
      class="ml-auto rounded px-1.5 py-px hover:text-text hover:bg-surface-hover"
      onclick={() => (shortcutsOpen = true)}
      title="Keyboard shortcuts (?)"
    >? shortcuts</button>
  </footer>
</div>

{#if contextMenu}
  <ContextMenu
    session={contextMenu.session}
    x={contextMenu.x}
    y={contextMenu.y}
    onclose={() => (contextMenu = null)}
    onrename={(id) => (renameRequest = id)}
  />
{/if}

{#if shortcutsOpen}
  <ShortcutsOverlay onclose={() => (shortcutsOpen = false)} />
{/if}

{#if settingsOpen}
  <SettingsModal onclose={() => (settingsOpen = false)} />
{/if}

{#if $showLimits}
  <LimitsModal onclose={() => ($showLimits = false)} />
{/if}
