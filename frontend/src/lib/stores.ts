import { writable, derived } from "svelte/store";

export interface SessionItem {
  sessionId: string;
  cwd: string;
  shortName: string;
  activity: string;
  isActive: boolean;
  model: string;
  gitBranch: string;
  costUsd: number;
  lastActivity: string;
  totalMessages: number;
  sparklineData: number[];
}

export interface SessionFull extends SessionItem {
  version: string;
  isWorktree: boolean;
  mainRepo: string;
  inputTokens: number;
  outputTokens: number;
  cacheCreationTokens: number;
  cacheReadTokens: number;
  userMessages: number;
  assistantMessages: number;
  currentTool: string;
  lastFileWrite: string;
  lastFileWriteAt: string;
  recentTools: { name: string; timestamp: string; ago: string }[];
  recentMessages: { role: string; text: string; timestamp: string }[];
}

export const sessions = writable<SessionItem[]>([]);
export const selectedId = writable<string | null>(null);
export const selectedDetail = writable<SessionFull | null>(null);
export const windowMinutes = writable(30);
export const activityFilter = writable("");
export const searchQuery = writable("");

export const activeCount = derived(sessions, ($sessions) =>
  $sessions.filter((s) => s.isActive).length
);

// Activity color mapping
const activityColorMap: Record<string, string> = {
  thinking: "var(--color-activity-thinking)",
  writing: "var(--color-activity-writing)",
  reading: "var(--color-activity-reading)",
  running: "var(--color-activity-running)",
  waiting: "var(--color-activity-waiting)",
  idle: "var(--color-activity-idle)",
  searching: "var(--color-activity-searching)",
  browsing: "var(--color-activity-browsing)",
  spawning: "var(--color-activity-spawning)",
  compacting: "var(--color-activity-compacting)",
};

export function activityColor(activity: string): string {
  return activityColorMap[activity] || "var(--color-activity-idle)";
}

export function formatTokens(n: number): string {
  if (n < 1000) return `${n}`;
  if (n < 1_000_000) return `${(n / 1000).toFixed(1)}k`;
  return `${(n / 1_000_000).toFixed(2)}M`;
}

export function formatCost(usd: number): string {
  if (usd < 0.01) return "<$0.01";
  return `$${usd.toFixed(2)}`;
}

export function timeAgo(iso: string): string {
  if (!iso) return "";
  const ms = Date.now() - new Date(iso).getTime();
  const s = Math.floor(ms / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
}
