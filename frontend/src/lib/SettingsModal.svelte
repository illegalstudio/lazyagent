<script lang="ts">
  import { onMount } from "svelte";
  import { Clipboard } from "@wailsio/runtime";
  import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";
  import { loadSessions } from "./actions";

  interface Props {
    onclose: () => void;
  }
  let { onclose }: Props = $props();

  const terminals: [string, string][] = [
    ["terminal", "System default"],
    ["kitty", "Kitty"],
    // Parked until their launch incantations are verified on real setups
    // (also re-enable in core.validTerminals and tray/terminal.go):
    // ["iterm2", "iTerm2"],
    // ["ghostty", "Ghostty"],
    // ["wezterm", "WezTerm"],
    // ["alacritty", "Alacritty"],
  ];

  let terminal = $state("terminal");
  let editor = $state("");
  let agents = $state<Record<string, boolean>>({});
  let excludes = $state("");
  let loaded = $state(false);
  let saving = $state(false);

  let apiConfigured = $state(false);
  let newPassphrase = $state("");
  let apiFeedback = $state("");
	let cursorConfigured = $state(false);
	let cursorSource = $state("");
	let cursorKey = $state("");
	let cursorFeedback = $state("");
	let cursorSaving = $state(false);

	async function refreshCursorStatus() {
		const status = await SessionService.GetCursorCredentialStatus();
		cursorConfigured = status.configured;
		cursorSource = status.source;
		if (status.error) cursorFeedback = status.error;
	}

	async function saveCursorKey() {
		if (!cursorKey.trim() || cursorSaving) return;
		cursorSaving = true;
		try {
			await SessionService.SetCursorAPIKey(cursorKey.trim());
			cursorKey = "";
			await refreshCursorStatus();
			cursorFeedback = cursorSource === "LAZYAGENT_CURSOR_API_KEY" ? "Saved; the environment override is still active." : "Key saved.";
			await loadSessions();
		} catch (error) { cursorFeedback = String(error); }
		finally { cursorSaving = false; }
	}

	async function removeCursorKey() {
		if (cursorSaving) return;
		cursorSaving = true;
		try {
			await SessionService.RemoveCursorAPIKey();
			cursorKey = "";
			await refreshCursorStatus();
			cursorFeedback = cursorConfigured ? "Saved key removed; the environment override is still active." : "Saved key removed.";
			await loadSessions();
		} catch (error) { cursorFeedback = String(error); }
		finally { cursorSaving = false; }
	}

  onMount(() => {
    SessionService.GetSettings().then((s: any) => {
      terminal = s.terminal || "terminal";
      editor = s.editor || "";
      agents = { ...(s.agents || {}) };
      excludes = (s.excludeCwdSubstrings || []).join("\n");
      loaded = true;
    }).catch(() => { loaded = true; });
    SessionService.IsAPIConfigured().then((v) => (apiConfigured = v)).catch(() => {});
    refreshCursorStatus().catch(() => cursorFeedback = "Could not read Cursor credential status.");
  });

  let agentNames = $derived(Object.keys(agents).sort());

  function save() {
    saving = true;
    const excludeList = excludes.split("\n").map((s) => s.trim()).filter(Boolean);
    SessionService.SaveSettings({
      terminal,
      editor: editor.trim(),
      agents,
      excludeCwdSubstrings: excludeList,
    } as any)
      .then(() => onclose())
      .catch(() => { saving = false; });
  }

  function updatePassphrase() {
    const p = newPassphrase.trim();
    if (!p) return;
    SessionService.SetAPIPassphrase(p).then(() => {
      apiConfigured = true;
      newPassphrase = "";
      apiFeedback = "Passphrase saved; restart --api to apply";
    }).catch(() => (apiFeedback = "Could not save the passphrase"));
  }

  function clearPassphrase() {
    SessionService.SetAPIPassphrase("").then(() => {
      apiConfigured = false;
      apiFeedback = "API authentication disabled";
    }).catch(() => (apiFeedback = "Could not clear the passphrase"));
  }

  function copyToken() {
    SessionService.GetAPIToken().then((t) => {
      if (t) {
        Clipboard.SetText(t);
        apiFeedback = "Bearer token copied";
      } else {
        apiFeedback = "No passphrase configured yet";
      }
    }).catch(() => {});
  }

  function handleWindowKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
      e.stopPropagation();
      onclose();
    }
  }
</script>

<svelte:window onkeydowncapture={handleWindowKey} />

<div class="fixed inset-0 z-40 flex items-center justify-center">
  <button
    class="absolute inset-0 bg-[#11111b]/70 cursor-default"
    aria-label="Close settings"
    onclick={onclose}
  ></button>
  <div
    class="relative w-[480px] max-w-[calc(100vw-24px)] max-h-[85vh] overflow-y-auto rounded-xl border border-surface-active bg-surface shadow-[0_20px_60px_rgba(0,0,0,0.7)] p-5"
    role="dialog"
    aria-label="Settings"
    tabindex="-1"
  >
    <div class="flex items-center justify-between mb-4">
      <h2 class="text-[14px] font-bold text-text">Settings</h2>
      <button class="text-subtext hover:text-text text-[14px] leading-none px-1" onclick={onclose} title="Close">✕</button>
    </div>

    {#if loaded}
      <div class="flex flex-col gap-5 text-[12px]">
        <section>
          <label class="block text-[10px] uppercase tracking-wider text-subtext mb-1.5" for="set-terminal">Terminal</label>
          <select
            id="set-terminal"
            bind:value={terminal}
            class="w-full bg-[#11111b] border border-border rounded-lg px-2.5 py-1.5 text-text outline-none focus:border-accent"
          >
            {#each terminals as [value, label]}
              <option {value}>{label}</option>
            {/each}
          </select>
          <p class="text-[10.5px] text-subtext mt-1">Used by Resume and any action that opens a terminal window.</p>
        </section>

        <section>
          <label class="block text-[10px] uppercase tracking-wider text-subtext mb-1.5" for="set-editor">Editor</label>
          <input
            id="set-editor"
            type="text"
            bind:value={editor}
            placeholder="code, zed, subl…"
            class="w-full bg-[#11111b] border border-border rounded-lg px-2.5 py-1.5 text-text outline-none focus:border-accent placeholder-subtext"
          />
          <p class="text-[10.5px] text-subtext mt-1">GUI editor command for "Open in editor". Empty falls back to $VISUAL, then $EDITOR in a terminal.</p>
        </section>

        <section>
          <div class="text-[10px] uppercase tracking-wider text-subtext mb-1.5">Agents</div>
          <div class="grid grid-cols-3 gap-x-3 gap-y-1.5">
            {#each agentNames as name}
              <label class="flex items-center gap-1.5 text-text">
                <input type="checkbox" bind:checked={agents[name]} class="accent-[#cba6f7]" />
                {name}
              </label>
            {/each}
          </div>
          <p class="text-[10.5px] text-subtext mt-1.5">Which agents to monitor. Takes effect at the next launch.</p>
        </section>

        <section>
          <label class="block text-[10px] uppercase tracking-wider text-subtext mb-1.5" for="set-excludes">Hide projects containing</label>
          <textarea
            id="set-excludes"
            bind:value={excludes}
            rows="3"
            placeholder={"node_modules\n/tmp/"}
            class="w-full bg-[#11111b] border border-border rounded-lg px-2.5 py-1.5 text-text outline-none focus:border-accent placeholder-subtext resize-y font-mono text-[11px]"
          ></textarea>
          <p class="text-[10.5px] text-subtext mt-1">One path fragment per line. Applies immediately.</p>
        </section>

        <section>
          <div class="flex items-center gap-2 mb-1.5">
            <span class="text-[10px] uppercase tracking-wider text-subtext">Cursor cloud agents</span>
            <span class="text-[10px] text-accent">{cursorConfigured ? "configured" : "not configured"}</span>
          </div>
          {#if cursorSource}<p class="text-[10.5px] text-subtext mb-1.5">{cursorSource === "LAZYAGENT_CURSOR_API_KEY" ? "Environment override active" : "Saved on this device"}</p>{/if}
          <div class="flex flex-wrap gap-1.5">
            <input id="set-cursor-key" aria-label="Cursor API key" type="password" autocomplete="new-password" bind:value={cursorKey} placeholder={cursorConfigured ? "New Cursor API key" : "Cursor API key"} class="flex-1 min-w-0 bg-[#11111b] border border-border rounded-lg px-2.5 py-1.5 text-text outline-none focus:border-accent placeholder-subtext" onkeydown={(e) => { if (e.key === "Enter") { e.preventDefault(); saveCursorKey(); } e.stopPropagation(); }} />
            <button class="rounded-lg bg-surface-hover hover:bg-surface-active text-text px-2.5 py-1.5 disabled:opacity-40" onclick={saveCursorKey} disabled={cursorSaving || !cursorKey.trim()}>Save key</button>
          </div>
          <div class="flex flex-wrap gap-2 mt-1.5">
            <button class="text-[11px] text-accent hover:underline" onclick={() => SessionService.OpenCursorAPIKeys().catch(() => {})}>Get a Cursor API key</button>
            <button class="text-[11px] text-subtext hover:text-text disabled:opacity-40" onclick={removeCursorKey} disabled={cursorSaving}>Remove saved key</button>
          </div>
          {#if cursorFeedback}<p class="text-[10.5px] text-accent mt-1.5" role="status">{cursorFeedback}</p>{/if}
          <p class="text-[10.5px] text-subtext mt-1.5">Saved beside the lazyagent configuration and shared with TUI and API. Changes apply to this window immediately; restart other running instances.</p>
        </section>

        <section>
          <div class="flex items-center gap-2 mb-1.5">
            <span class="text-[10px] uppercase tracking-wider text-subtext">API authentication</span>
            {#if apiConfigured}
              <span class="rounded-full bg-activity-waiting/15 text-activity-waiting text-[9.5px] font-semibold px-1.5 py-px">configured</span>
            {:else}
              <span class="rounded-full bg-surface-hover text-subtext text-[9.5px] font-semibold px-1.5 py-px">not configured</span>
            {/if}
          </div>
          <div class="flex gap-1.5">
            <input
              type="password"
              bind:value={newPassphrase}
              placeholder={apiConfigured ? "New passphrase (rotate)" : "Set a passphrase"}
              class="flex-1 min-w-0 bg-[#11111b] border border-border rounded-lg px-2.5 py-1.5 text-text outline-none focus:border-accent placeholder-subtext"
              onkeydown={(e) => { if (e.key === "Enter") updatePassphrase(); e.stopPropagation(); }}
            />
            <button
              class="rounded-lg bg-surface-hover hover:bg-surface-active text-text px-2.5 py-1.5 disabled:opacity-40"
              onclick={updatePassphrase}
              disabled={!newPassphrase.trim()}
            >Save passphrase</button>
          </div>
          <div class="flex flex-wrap items-center gap-1.5 mt-1.5">
            <button class="rounded-lg bg-surface-hover hover:bg-surface-active text-text px-2.5 py-1 text-[11px]" onclick={copyToken} disabled={!apiConfigured}>Copy bearer token</button>
            <button class="rounded-lg bg-surface-hover hover:bg-activity-spawning/20 hover:text-activity-spawning text-subtext px-2.5 py-1 text-[11px] disabled:opacity-40" onclick={clearPassphrase} disabled={!apiConfigured}>Clear</button>
            {#if apiFeedback}<span class="text-[10.5px] text-accent">{apiFeedback}</span>{/if}
          </div>
          <p class="text-[10.5px] text-subtext mt-1.5">Protects the HTTP API. The current passphrase is never shown; a running --api server applies changes at its next restart.</p>
        </section>
      </div>

      <div class="flex justify-end gap-2 mt-5 pt-4 border-t border-border">
        <button class="rounded-lg bg-surface-hover hover:bg-surface-active text-text px-3.5 py-1.5 text-[12px]" onclick={onclose}>Cancel</button>
        <button
          class="rounded-lg bg-accent hover:bg-accent/90 text-surface font-semibold px-3.5 py-1.5 text-[12px] disabled:opacity-50"
          onclick={save}
          disabled={saving}
        >{saving ? "Saving…" : "Save"}</button>
      </div>
    {:else}
      <div class="py-8 text-center text-subtext text-[12px]">Loading…</div>
    {/if}
  </div>
</div>
