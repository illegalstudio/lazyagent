<script lang="ts">
  interface Props {
    data: number[];
    color: string;
    width?: number;
    height?: number;
  }

  let { data, color, width = 120, height = 24 }: Props = $props();

  let points = $derived.by(() => {
    if (!data || data.length === 0) return "";
    const max = Math.max(...data, 1);
    const stepX = width / (data.length - 1 || 1);
    const pts = data.map((v, i) => {
      const x = i * stepX;
      const y = height - (v / max) * height;
      return `${x},${y}`;
    });
    return pts.join(" ");
  });

  let areaPath = $derived.by(() => {
    if (!data || data.length === 0) return "";
    const max = Math.max(...data, 1);
    const stepX = width / (data.length - 1 || 1);
    let d = `M 0,${height}`;
    for (let i = 0; i < data.length; i++) {
      const x = i * stepX;
      const y = height - (data[i] / max) * height;
      d += ` L ${x},${y}`;
    }
    d += ` L ${width},${height} Z`;
    return d;
  });
</script>

<svg {width} {height} viewBox="0 0 {width} {height}" class="block">
  {#if data && data.length > 0}
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
