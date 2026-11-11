<script lang="ts">
  import { onMount } from "svelte";
  import {
    selectedId,
    selectedDetail,
    windowMinutes,
    cardDensity,
    type CardDensity,
    detailWidth,
    searching,
    showLimits,
    limitsRefreshToken,
    updateVersion,
    isDetached,
  } from "./lib/stores";
  import {
    loadSessions,
    loadDetail,
    cycleFilter,
    adjustWindow,
    toggleDetach,
    setSearch,
    syncDetachState,
    toggleIdleCloud,
  } from "./lib/actions";
  import PanelView from "./lib/PanelView.svelte";
  import DesktopView from "./lib/DesktopView.svelte";
  import * as SessionService from "./bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";
  import { Events } from "@wailsio/runtime";

  $effect(() => {
    const id = $selectedId;
    if (id) {
      loadDetail(id);
    } else {
      $selectedDetail = null;
    }
  });

  function handleLimitsRefresh(e: KeyboardEvent) {
    if ($showLimits && (e.key === "r" || e.key === "R")) {
      e.preventDefault();
      e.stopImmediatePropagation();
      $limitsRefreshToken += 1;
    }
  }

  function handleKeydown(e: KeyboardEvent) {
    if ($searching) {
      if (e.key === "Escape") {
        e.preventDefault();
        setSearch("");
        $searching = false;
      }
      return;
    }

    const target = e.target as HTMLElement;
    if (["INPUT", "TEXTAREA", "SELECT"].includes(target?.tagName) || target?.isContentEditable) return;

    if ($showLimits) {
      if (!$isDetached && (e.key === "Escape" || e.key === "l" || e.key === "L")) {
        e.preventDefault();
        $showLimits = false;
        return;
      }
    }

    if (e.key === "Escape") {
      if ($selectedId !== null) {
        e.preventDefault();
        $selectedId = null;
      }
    } else if (e.key === "/") {
      e.preventDefault();
      $searching = true;
    } else if (e.key === "d") {
      e.preventDefault();
      toggleDetach();
    } else if (e.key === "a") {
      e.preventDefault();
      toggleIdleCloud().catch(() => {});
    } else if (e.key === "f") {
      e.preventDefault();
      cycleFilter();
    } else if (e.key === "+" || e.key === "=") {
      e.preventDefault();
      adjustWindow(10);
    } else if (e.key === "-") {
      e.preventDefault();
      adjustWindow(-10);
    } else if (e.key === "l" || e.key === "L") {
      e.preventDefault();
      $showLimits = true;
    }
  }

  onMount(() => {
    loadSessions();

    SessionService.GetWindowMinutes().then((m) => {
      $windowMinutes = m;
    }).catch(() => {});

    SessionService.GetCardDensity()
      .then((d) => $cardDensity = d as CardDensity)
      .catch(() => {});

    SessionService.GetDetailWidth()
      .then((w) => $detailWidth = w)
      .catch(() => {});

    Events.On("sessions:updated", () => {
      loadSessions();
      if ($selectedId) loadDetail($selectedId);
    });

    syncDetachState();

    Events.On("detach:changed", () => {
      syncDetachState();
    });

    // Native View-menu events (desktop mode).
    Events.On("density:changed", (ev: any) => {
      const d = Array.isArray(ev?.data) ? ev.data[0] : ev?.data;
      if (d === "compact" || d === "rich" || d === "live") $cardDensity = d;
    });
    Events.On("menu:showLimits", () => {
      $showLimits = true;
    });

    // Check for updates after a short delay (gives the backend time to fetch)
    setTimeout(async () => {
      try {
        const v = await SessionService.GetUpdateVersion();
        if (v) $updateVersion = v;
      } catch {}
    }, 3000);
  });
</script>

<svelte:window onkeydowncapture={handleLimitsRefresh} onkeydown={handleKeydown} />

{#if $isDetached}
  <DesktopView />
{:else}
  <PanelView />
{/if}
