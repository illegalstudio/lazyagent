import { get } from "svelte/store";
import {
  sessions, selectedId, selectedDetail, cloudStatus, windowMinutes, activityFilter, searchQuery,
  isDetached, isPinned,
} from "./stores";
import * as SessionService from "../bindings/github.com/illegalstudio/lazyagent/internal/tray/sessionservice";

let sessionRequest = 0;
let detailRequest = 0;

export async function loadSessions(): Promise<void> {
  const request = ++sessionRequest;
  try {
    const items = await SessionService.GetSessions();
    const status = await SessionService.GetCloudStatus();
    if (request !== sessionRequest) return;
    sessions.set(items || []);
    cloudStatus.set(status);
  } catch {
    // Dev mode without Go backend
  }
}

export async function loadDetail(id: string): Promise<void> {
  const request = ++detailRequest;
  try {
    const detail = await SessionService.GetSessionDetail(id);
    if (request === detailRequest && get(selectedId) === id) selectedDetail.set(detail as any);
  } catch {
    if (request === detailRequest && get(selectedId) === id) selectedDetail.set(null);
  }
}

export async function toggleIdleCloud(): Promise<void> {
  await SessionService.SetCloudShowIdle(!get(cloudStatus).showIdle);
  await loadSessions();
}

const allFilters = ["", "idle", "waiting", "thinking", "compacting", "reading", "writing", "running", "searching", "browsing", "spawning"];

export function cycleFilter(): void {
  const idx = allFilters.indexOf(get(activityFilter));
  const next = allFilters[(idx + 1) % allFilters.length];
  activityFilter.set(next);
  SessionService.SetActivityFilter(next).catch(() => {});
  loadSessions();
}

export function adjustWindow(delta: number): void {
  const next = Math.max(10, Math.min(480, get(windowMinutes) + delta));
  windowMinutes.set(next);
  SessionService.SetWindowMinutes(next).catch(() => {});
  loadSessions();
}

export function toggleDetach(): void {
  if (get(isDetached)) {
    SessionService.Attach().catch(() => {});
  } else {
    SessionService.Detach().catch(() => {});
  }
}

export function togglePin(): void {
  SessionService.TogglePin().catch(() => {});
}

export function setSearch(value: string): void {
  searchQuery.set(value);
  SessionService.SetSearchQuery(value).catch(() => {});
  loadSessions();
}

export function syncDetachState(): void {
  SessionService.IsDetached().then((d) => isDetached.set(d)).catch(() => {});
  SessionService.IsPinned().then((p) => isPinned.set(p)).catch(() => {});
}
