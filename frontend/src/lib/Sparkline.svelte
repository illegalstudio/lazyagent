<script lang="ts">
  interface Props {
    data: number[];
    color: string;
    width?: number;
    height?: number;
  }

  let { data, color, width = 120, height = 24 }: Props = $props();

  let coords = $derived.by(() => {
    if (!data || data.length === 0) return [];
    const max = Math.max(...data, 1);
    const stepX = width / (data.length - 1 || 1);
    return data.map((v, i) => ({
      x: i * stepX,
      y: height - (v / max) * height,
    }));
  });

  let points = $derived(coords.map((c) => `${c.x},${c.y}`).join(" "));

  let areaPath = $derived.by(() => {
    if (coords.length === 0) return "";
    let d = `M 0,${height}`;
    for (const c of coords) {
      d += ` L ${c.x},${c.y}`;
    }
    d += ` L ${width},${height} Z`;
    return d;
  });
</script>

<svg {width} {height} viewBox="0 0 {width} {height}" class="block">
  {#if coords.length > 0}
    <path d={areaPath} fill={color} opacity="0.15" />
    <polyline
      {points}
      fill="none"
      stroke={color}
      stroke-width="1.5"
      stroke-linecap="round"
      stroke-linejoin="round"
    />
  {/if}
</svg>
