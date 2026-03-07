<script lang="ts">
  import { activityColor } from "./stores";

  interface Props {
    activity: string;
    isActive: boolean;
  }

  let { activity, isActive }: Props = $props();

  let color = $derived(activityColor(activity));
</script>

<span
  class="inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium leading-none"
  style="background: color-mix(in srgb, {color} 15%, transparent); color: {color};"
>
  <span
    class="inline-block h-1.5 w-1.5 rounded-full"
    class:animate-pulse-dot={isActive}
    style="background: {color};"
  ></span>
  {activity}
</span>
