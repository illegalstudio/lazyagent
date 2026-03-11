<script lang="ts">
  import {
    selectedDetail,
    activityColor,
    formatTokens,
    formatCost,
    timeAgo,
  } from "./stores";
  import ActivityBadge from "./ActivityBadge.svelte";
  import Sparkline from "./Sparkline.svelte";
  import * as SessionService from "../bindings/github.com/nahime0/lazyagent/internal/tray/sessionservice";

  let detail = $derived($selectedDetail);
  let color = $derived(detail ? activityColor(detail.activity) : "var(--color-activity-idle)");
  let displayName = $derived(detail ? (detail.customName || detail.agentName || detail.shortName) : "");

  let renaming = $state(false);
  let renameValue = $state("");
  let renameInput = $state<HTMLInputElement | null>(null);

  function openEditor() {
    if (detail) {
      SessionService.OpenInEditor(detail.cwd, detail.agent).catch(() => {});
    }
  }

  function startRename() {
    if (!detail) return;
    renaming = true;
    renameValue = detail.customName || "";
    requestAnimationFrame(() => renameInput?.focus());
  }

  function confirmRename() {
    if (detail && renaming) {
      SessionService.SetSessionName(detail.sessionId, renameValue.trim()).catch(() => {});
    }
    renaming = false;
    renameValue = "";
  }

  function cancelRename() {
    renaming = false;
    renameValue = "";
  }

  function handleRenameKey(e: KeyboardEvent) {
    e.stopPropagation(); // Prevent App.svelte from intercepting rename input keys
    if (e.key === "Enter") { e.preventDefault(); confirmRename(); }
    else if (e.key === "Escape") { e.preventDefault(); cancelRename(); }
  }

  function handleKeydown(e: KeyboardEvent) {
    if (renaming) return; // Already renaming, let handleRenameKey deal with it
    const tag = (e.target as HTMLElement)?.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA") return;
    if (e.key === "r" && detail) {
      e.preventDefault();
      startRename();
    }
  }
</script>

<svelte:window onkeydown={handleKeydown} />

{#if detail}
  <div class="flex flex-col h-full overflow-y-auto px-4 py-3 gap-3">
    <!-- Header -->
    <div>
      <div class="flex items-center justify-between gap-2">
        {#if renaming}
          <input
            bind:this={renameInput}
            bind:value={renameValue}
            onkeydown={handleRenameKey}
            onblur={confirmRename}
            class="flex-1 min-w-0 bg-surface text-text text-[15px] font-semibold px-1 py-0 rounded border border-accent outline-none"
            placeholder={detail.shortName}
          />
        {:else}
          <h2
            class="text-[15px] font-semibold text-text truncate cursor-pointer hover:text-accent transition-colors"
            ondblclick={startRename}
            title="Double-click to rename"
          >{displayName}</h2>
        {/if}
        <div class="flex gap-1 shrink-0">
          <button
            class="rounded px-2 py-1 text-[11px] font-medium text-subtext bg-surface-hover hover:text-text transition-colors no-drag"
            onclick={startRename}
            title="Rename session"
          >
            Rename
          </button>
          <button
            class="rounded px-2 py-1 text-[11px] font-medium text-accent bg-accent/10 hover:bg-accent/20 transition-colors no-drag"
            onclick={openEditor}
            title="Open in editor"
          >
            Open
          </button>
        </div>
      </div>
      <div class="flex items-center gap-2 mt-1">
        <ActivityBadge activity={detail.activity} isActive={detail.isActive} />
        {#if detail.currentTool}
          <span class="text-[11px] text-subtext">({detail.currentTool})</span>
        {/if}
      </div>
    </div>

    <!-- Sparkline -->
    <div class="bg-surface rounded-lg p-2">
      <Sparkline data={detail.sparklineData} {color} width={320} height={32} />
    </div>

    <!-- Info grid -->
    <div class="border-t border-border pt-3">
      <dl class="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1.5 text-[12px]">
        {#if detail.agent}
          <dt class="text-subtext">Agent</dt>
          <dd class="text-text">{detail.agent === "pi" ? "π pi" : detail.agent}</dd>
        {/if}
        {#if detail.source === "desktop"}
          <dt class="text-subtext">Source</dt>
          <dd class="text-accent">Claude Desktop</dd>
          {#if detail.desktopTitle}
            <dt class="text-subtext">Title</dt>
            <dd class="text-text">{detail.desktopTitle}</dd>
          {/if}
          {#if detail.permissionMode}
            <dt class="text-subtext">Permissions</dt>
            <dd class="text-text">{detail.permissionMode}</dd>
          {/if}
        {:else if detail.agent === "claude"}
          <dt class="text-subtext">Source</dt>
          <dd class="text-text">CLI</dd>
        {/if}
        {#if detail.model}
          <dt class="text-subtext">Model</dt>
          <dd class="text-text truncate">{detail.model}</dd>
        {/if}
        {#if detail.gitBranch && detail.gitBranch !== "HEAD"}
          <dt class="text-subtext">Branch</dt>
          <dd class="text-text truncate">{detail.gitBranch}</dd>
        {/if}
        {#if detail.version}
          <dt class="text-subtext">Version</dt>
          <dd class="text-text">{detail.version}</dd>
        {/if}
        <dt class="text-subtext">Messages</dt>
        <dd class="text-text">
          {detail.totalMessages}
          <span class="text-subtext">({detail.userMessages} user, {detail.assistantMessages} AI)</span>
        </dd>
        {#if detail.inputTokens > 0 || detail.outputTokens > 0}
          <dt class="text-subtext">Tokens</dt>
          <dd class="text-text">
            {formatTokens(detail.inputTokens + detail.cacheCreationTokens + detail.cacheReadTokens)} in /
            {formatTokens(detail.outputTokens)} out
            {#if detail.costUsd > 0.001}
              <span class="text-accent font-medium ml-1">{formatCost(detail.costUsd)}</span>
            {/if}
          </dd>
        {/if}
        {#if detail.isWorktree}
          <dt class="text-subtext">Worktree</dt>
          <dd class="text-accent">yes {#if detail.mainRepo}<span class="text-subtext">({detail.mainRepo})</span>{/if}</dd>
        {/if}
        {#if detail.lastFileWrite}
          <dt class="text-subtext">Last file</dt>
          <dd class="text-text truncate">
            {detail.lastFileWrite}
            <span class="text-subtext ml-1">{timeAgo(detail.lastFileWriteAt)}</span>
          </dd>
        {/if}
        <dt class="text-subtext">Last active</dt>
        <dd class="text-text">{timeAgo(detail.lastActivity)}</dd>
        <dt class="text-subtext">CWD</dt>
        <dd class="text-text truncate text-[11px]">{detail.cwd}</dd>
      </dl>
    </div>

    <!-- Conversation -->
    {#if detail.recentMessages && detail.recentMessages.length > 0}
      <div class="border-t border-border pt-3">
        <h3 class="text-[12px] font-semibold text-subtext mb-2">Conversation</h3>
        <div class="flex flex-col gap-1.5">
          {#each detail.recentMessages.slice(-5).reverse() as msg}
            <div class="flex gap-2 text-[12px]">
              <span class="shrink-0 w-8 text-right font-medium {msg.role === 'user' ? 'text-accent' : 'text-activity-thinking'}">
                {msg.role === "user" ? "You" : "AI"}
              </span>
              <span class="text-text/80 truncate">{msg.text}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Recent Tools -->
    {#if detail.recentTools && detail.recentTools.length > 0}
      <div class="border-t border-border pt-3">
        <h3 class="text-[12px] font-semibold text-subtext mb-2">Recent Tools</h3>
        <div class="flex flex-col gap-1">
          {#each detail.recentTools.slice(-10).reverse() as tool}
            <div class="flex items-center justify-between text-[12px]">
              <span class="text-accent">{tool.name}</span>
              <span class="text-subtext">{tool.ago}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  </div>
{:else}
  <div class="flex items-center justify-center h-full text-subtext text-sm">
    Select a session
  </div>
{/if}
