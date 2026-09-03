<script lang="ts">
  import type { SessionItem, CardDensity } from "./stores";
  import { activityColor, formatCost, timeAgo } from "./stores";
  import Sparkline from "./Sparkline.svelte";
  import ActivityBadge from "./ActivityBadge.svelte";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

  interface Props {
    session: SessionItem;
    density: CardDensity;
    selected: boolean;
    onselect: (id: string) => void;
    oncontext: (session: SessionItem, x: number, y: number) => void;
    renameRequest?: string | null;
    onrenamehandled?: () => void;
  }
  let {
    session, density, selected, onselect, oncontext,
    renameRequest = null, onrenamehandled = () => {},
  }: Props = $props();

  $effect(() => {
    if (renameRequest === session.sessionId) {
      startRename();
      onrenamehandled();
    }
  });

  let color = $derived(activityColor(session.activity));
  let name = $derived(session.customName || session.agentName || session.shortName);

  const glyphs: Record<string, string> = {
    pi: "π", opencode: "O", kilo: "L", cursor: "C", codex: "X", amp: "A", kimi: "K",
  };
  let glyph = $derived(
    glyphs[session.agent] ?? (session.source === "desktop" ? "D" : "")
  );

  let renaming = $state(false);
  let renameValue = $state("");
  let renameInput = $state<HTMLInputElement | null>(null);

  function startRename() {
    renaming = true;
    renameValue = session.customName || "";
    requestAnimationFrame(() => renameInput?.focus());
  }
  function confirmRename() {
    if (renaming) {
      SessionService.SetSessionName(session.sessionId, renameValue.trim()).catch(() => {});
    }
    renaming = false;
  }
  function handleRenameKey(e: KeyboardEvent) {
    e.stopPropagation();
    if (e.key === "Enter") { e.preventDefault(); confirmRename(); }
    else if (e.key === "Escape") { e.preventDefault(); renaming = false; }
  }

  function resume(yolo = false) {
    SessionService.ResumeInTerminal(session.sessionId, yolo).catch(() => {});
  }
  function openEditor() {
    SessionService.OpenInEditor(session.cwd, session.agent).catch(() => {});
  }
  function moreMenu(e: MouseEvent) {
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
    oncontext(session, r.left, r.bottom + 4);
  }
</script>

<div
  class="group flex flex-col rounded-lg border bg-surface transition-colors duration-75 no-drag overflow-hidden
    {selected ? 'border-accent' : 'border-border hover:border-subtext/50'}"
  role="group"
  oncontextmenu={(e) => { e.preventDefault(); oncontext(session, e.clientX, e.clientY); }}
>
  <button class="flex flex-col text-left px-3 pt-2.5 pb-2 w-full" onclick={() => onselect(session.sessionId)} ondblclick={startRename}>
    <div class="flex items-center justify-between gap-2 w-full">
      <div class="flex items-center gap-1.5 min-w-0 flex-1">
        <span
          class="shrink-0 h-2 w-2 rounded-full"
          class:animate-pulse-dot={session.isActive}
          style="background: {color};"
        ></span>
        {#if renaming}
          <input
            bind:this={renameInput}
            bind:value={renameValue}
            onkeydown={handleRenameKey}
            onblur={confirmRename}
            onclick={(e) => e.stopPropagation()}
            class="flex-1 min-w-0 bg-surface-hover text-text text-[13px] font-semibold px-1 py-0 rounded border border-accent outline-none"
            placeholder={session.shortName}
          />
        {:else}
          <span class="truncate text-[13px] font-semibold text-text">
            {#if glyph}<span class="{session.agent === 'pi' ? 'text-activity-spawning' : session.source === 'desktop' ? 'text-accent' : 'text-subtext'} font-normal">{glyph}</span>{/if}
            {name}
          </span>
        {/if}
      </div>
      <div class="shrink-0">
        <ActivityBadge activity={session.activity} isActive={session.isActive} />
      </div>
    </div>

    <div class="mt-1.5">
      <Sparkline data={session.sparklineData} {color} width={140} height={16} />
    </div>

    <div class="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-[10.5px] text-subtext w-full">
      {#if density !== "compact"}
        {#if session.model}<span class="bg-surface-hover rounded px-1.5 py-px truncate max-w-[9rem]">{session.model}</span>{/if}
        {#if session.gitBranch}<span class="truncate max-w-[9rem]">⎇ {session.gitBranch}</span>{/if}
      {/if}
      <span class="text-activity-reading font-medium">{formatCost(session.costUsd)}</span>
      {#if density === "compact"}
        <span>{timeAgo(session.lastActivity)}</span>
      {:else}
        <span>{session.totalMessages} msg</span>
      {/if}
    </div>

    {#if density !== "compact" && session.currentTool}
      <div class="mt-2 pt-1.5 border-t border-surface-hover w-full flex items-center gap-1.5 text-[10.5px] text-subtext">
        <span>▸</span>
        <code class="bg-surface-hover rounded px-1 py-px text-[10px] text-text">{session.currentTool}</code>
      </div>
    {/if}

    {#if density === "live" && session.lastMessage}
      <div class="mt-1.5 w-full rounded border-l-2 border-border bg-surface/60 px-2 py-1 text-[10.5px] italic leading-snug text-subtext line-clamp-2">
        {session.lastMessage}
      </div>
    {/if}
  </button>

  <div class="flex items-center gap-1.5 px-2.5 py-1.5 border-t border-border bg-[#181825]
    {density === 'compact' ? 'hidden group-hover:flex' : ''}">
    {#if session.resumeAvailable}
      <div class="flex overflow-hidden rounded-md">
        <button
          class="bg-accent/90 hover:bg-accent text-surface text-[10.5px] font-semibold px-2 py-0.5"
          onclick={() => resume(false)}
          title="Resume this session in a new terminal window"
        >▶ Resume</button>
        {#if session.yoloResumeAvailable}
          <button
            class="border-l border-surface/30 bg-activity-writing/90 hover:bg-activity-writing text-surface text-[9.5px] font-bold px-1.5 py-0.5"
            onclick={() => resume(true)}
            title="Resume using this agent's YOLO flag"
          >YOLO</button>
        {/if}
      </div>
    {/if}
    <button
      class="rounded-md bg-surface-hover hover:bg-surface-active text-text text-[10.5px] px-2 py-0.5"
      onclick={openEditor}
      title="Open the project in your editor"
    >Editor</button>
    <button
      class="rounded-md bg-surface-hover hover:bg-surface-active text-text text-[10.5px] px-2 py-0.5"
      onclick={startRename}
      title="Rename session"
    >✎</button>
    <button
      class="rounded-md bg-surface-hover hover:bg-surface-active text-text text-[10.5px] px-2 py-0.5 ml-auto"
      onclick={moreMenu}
      title="More actions"
    >⋯</button>
  </div>
</div>
