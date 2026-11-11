<script lang="ts">
  interface Props {
    onclose: () => void;
  }
  let { onclose }: Props = $props();

  const groups: { title: string; keys: [string, string][] }[] = [
    {
      title: "Navigate",
      keys: [["j / k", "Move between sessions"], ["esc", "Close panel / dialog"], ["/", "Search"]],
    },
    {
      title: "View",
      keys: [["⌘1 ⌘2 ⌘3", "Card density"], ["f", "Cycle activity filter"], ["a", "Include idle cloud agents"], ["l", "Open limits"], ["+ / −", "Time window"]],
    },
    {
      title: "Session",
      keys: [["r", "Rename selected / refresh limits"], ["double-click", "Rename card"], ["right-click", "Card actions"]],
    },
    {
      title: "App",
      keys: [["⌘R", "Refresh sessions"], ["d", "Attach to system tray"], ["⌘Q", "Quit"]],
    },
  ];

  function handleWindowKey(e: KeyboardEvent) {
    if (e.key === "Escape" || e.key === "?") {
      e.stopPropagation();
      onclose();
    }
  }
</script>

<svelte:window onkeydowncapture={handleWindowKey} />

<div class="fixed inset-0 z-40 flex items-center justify-center">
  <button
    class="absolute inset-0 bg-[#11111b]/70 cursor-default"
    aria-label="Close keyboard shortcuts"
    onclick={onclose}
  ></button>
  <div
    class="relative w-[440px] rounded-xl border border-surface-active bg-surface shadow-[0_20px_60px_rgba(0,0,0,0.7)] p-5"
    role="dialog"
    aria-label="Keyboard shortcuts"
    tabindex="-1"
  >
    <div class="flex items-center justify-between mb-4">
      <h2 class="text-[14px] font-bold text-text">Keyboard shortcuts</h2>
      <button class="text-subtext hover:text-text text-[14px] leading-none px-1" onclick={onclose} title="Close">✕</button>
    </div>
    <div class="grid grid-cols-2 gap-x-6 gap-y-4">
      {#each groups as g}
        <div>
          <div class="text-[10px] uppercase tracking-wider text-subtext mb-1.5">{g.title}</div>
          {#each g.keys as [key, desc]}
            <div class="flex items-center justify-between py-0.5 text-[12px]">
              <span class="text-subtext">{desc}</span>
              <kbd class="bg-surface-hover border border-border rounded px-1.5 py-px text-[10.5px] text-text ml-2 whitespace-nowrap">{key}</kbd>
            </div>
          {/each}
        </div>
      {/each}
    </div>
  </div>
</div>
