import {
  isActiveCollectStatus,
  type CollectProgress,
  type FilmSource,
} from "./types";

export interface CollectQueueGroup {
  queueId: string;
  sourceIds: string[];
}

export interface CollectQueueProgressView {
  total: number;
  doneCount: number;
  failedCount: number;
  stoppedCount: number;
  fetchingCount: number;
  stoppingCount: number;
  percent: number;
  pageDone: number;
  pageTotal: number;
  failed: number;
  running: boolean;
}

export function collectQueueIdOf(progress?: CollectProgress | null): string | null {
  const queueId = progress?.queueId?.trim();
  return queueId || null;
}

/** 按采集队列分组：有 queueId 的各成一条，未打标的进度合在一起。 */
export function groupCollectQueues(sites: FilmSource[]): CollectQueueGroup[] {
  const order: string[] = [];
  const byId = new Map<string, string[]>();
  for (const site of sites) {
    const queueId = collectQueueIdOf(site.progress);
    if (!queueId) {
      continue;
    }
    const ids = byId.get(queueId);
    if (ids) {
      ids.push(site.id);
      continue;
    }
    order.push(queueId);
    byId.set(queueId, [site.id]);
  }
  return order.map((queueId) => ({
    queueId,
    sourceIds: byId.get(queueId) ?? [],
  }));
}

export function computeCollectQueueProgress(
  sourceIds: string[],
  sites: FilmSource[],
): CollectQueueProgressView | null {
  if (sourceIds.length === 0) {
    return null;
  }
  const byId = new Map(sites.map((item) => [item.id, item]));
  let pageDone = 0;
  let pageTotal = 0;
  let failed = 0;
  let fetchingCount = 0;
  let stoppingCount = 0;
  let doneCount = 0;
  let failedCount = 0;
  let stoppedCount = 0;
  let counted = 0;

  for (const id of sourceIds) {
    const progress = byId.get(id)?.progress;
    if (!progress) {
      continue;
    }
    counted += 1;
    failed += progress.failed;
    const pages = progress.total;
    if (pages > 0) {
      pageTotal += pages;
      pageDone += Math.min(progress.success + progress.failed, pages);
    }
    const status = progress.status;
    if (isActiveCollectStatus(status)) {
      fetchingCount += 1;
    } else if (status === "stopping") {
      stoppingCount += 1;
    } else if (status === "failed") {
      failedCount += 1;
    } else if (status === "stopped") {
      stoppedCount += 1;
    } else {
      doneCount += 1;
    }
  }

  if (counted === 0) {
    return null;
  }
  const running = fetchingCount + stoppingCount > 0;
  const percent = pageTotal > 0 ? Math.floor((pageDone / pageTotal) * 100) : 0;
  return {
    total: sourceIds.length,
    doneCount,
    failedCount,
    stoppedCount,
    fetchingCount,
    stoppingCount,
    percent: running ? Math.min(percent, 99) : Math.min(percent, 100),
    pageDone,
    pageTotal,
    failed,
    running,
  };
}

function isPromotedClientQueue(
  prev: { queueId: string; staleQueueId?: string },
  nextQueueId: string,
): boolean {
  if (!isClientCollectQueueId(prev.queueId)) {
    return false;
  }
  const nextId = nextQueueId.trim();
  if (!nextId || nextId === prev.queueId || isClientCollectQueueId(nextId)) {
    return false;
  }
  const stale = prev.staleQueueId?.trim() || "";
  return nextId !== stale;
}

/** 同一条队列从进行中收到终态才算结束。q-local 接到新的服务端 queueId 也算。上一轮 id 不算。 */
export function finishedQueueViews<
  T extends { seq: number; queueId: string; view: CollectQueueProgressView },
>(prev: (T & { staleQueueId?: string })[], next: T[]): CollectQueueProgressView[] {
  const ended: CollectQueueProgressView[] = [];
  for (const bar of prev) {
    if (!bar.view.running) {
      continue;
    }
    const current = next.find(
      (item) =>
        item.seq === bar.seq &&
        (item.queueId === bar.queueId || isPromotedClientQueue(bar, item.queueId)),
    );
    if (current && !current.view.running) {
      ended.push(current.view);
    }
  }
  return ended;
}

export type QueueEndLevel = "success" | "info" | "warning" | "error";

export function queueEndNotice(view: CollectQueueProgressView): {
  level: QueueEndLevel;
  text: string;
} {
  if (view.stoppedCount > 0) {
    return { level: "info", text: "采集已停止" };
  }
  if (view.failedCount > 0) {
    return {
      level: view.doneCount > 0 ? "warning" : "error",
      text: view.doneCount > 0 ? "采集部分失败" : "采集失败",
    };
  }
  return { level: "success", text: "采集完成" };
}

export function queueProgressTitle(
  running: boolean,
  queueCount: number,
  seq: number,
): string {
  const name = queueCount > 1 ? `队列 ${seq} 总进度` : "队列总进度";
  if (running) {
    return name;
  }
  return `${name} · 已结束`;
}

const CLIENT_QUEUE_PREFIX = "q-local-";
const TERMINAL_COLLECT_STATUS = new Set(["done", "failed", "stopped"]);

export function newClientCollectQueueId(): string {
  return `${CLIENT_QUEUE_PREFIX}${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
}

export function isClientCollectQueueId(queueId?: string | null): boolean {
  return !!queueId?.startsWith(CLIENT_QUEUE_PREFIX);
}

export function isTerminalCollectStatus(status?: string | null): boolean {
  return !!status && TERMINAL_COLLECT_STATUS.has(status);
}

function isLocalStart(progress?: CollectProgress | null): boolean {
  return progress?.status === "starting" && isClientCollectQueueId(progress.queueId);
}

function dropStaleTerminal(current: CollectProgress, incoming: CollectProgress): boolean {
  if (!isTerminalCollectStatus(incoming.status)) {
    return false;
  }
  // 没记下上一轮时，终态一律丢掉，避免把还在播的旧完成当成这次结果。
  if (current.staleQueueId == null) {
    return true;
  }
  const incomingId = incoming.queueId?.trim() ?? "";
  const staleId = current.staleQueueId.trim();
  return incomingId === "" || incomingId === staleId || incomingId === current.queueId;
}

/** 站点进度的下一次值。本地刚发起时丢掉上一轮或空 queueId 的终态；新 queueId 的终态直接采用。 */
export function nextCollectProgress(
  current: CollectProgress | null | undefined,
  incoming: CollectProgress | undefined,
): CollectProgress | null {
  if (isLocalStart(current) && current) {
    if (!incoming || dropStaleTerminal(current, incoming)) {
      return current;
    }
  }
  if (
    current?.status === "stopping" &&
    incoming &&
    isActiveCollectStatus(incoming.status) &&
    incoming.queueId === current.queueId
  ) {
    return current;
  }
  if (incoming) {
    return incoming;
  }
  if (!current || current.status === "stopping") {
    return current ?? null;
  }
  if (
    isActiveCollectStatus(current.status) ||
    isTerminalCollectStatus(current.status)
  ) {
    return null;
  }
  return current;
}

/** 进度条对齐：同 queueId 原条；仅 q-local 乐观条可按站点接到服务端 id。单站重采不得并回旧批次。 */
export function matchPrevCollectQueueBar<T extends { queueId: string; sourceIds: string[] }>(
  group: { queueId: string; sourceIds: string[] },
  prev: T[],
  usedPrev: Set<string>,
): T | undefined {
  const exact = prev.find((bar) => bar.queueId === group.queueId);
  if (exact && !usedPrev.has(exact.queueId)) {
    return exact;
  }
  const groupSet = new Set(group.sourceIds);
  for (const bar of prev) {
    if (usedPrev.has(bar.queueId) || !isClientCollectQueueId(bar.queueId)) {
      continue;
    }
    if (bar.sourceIds.some((id) => groupSet.has(id))) {
      return bar;
    }
  }
  return undefined;
}

export function tourProgressPhase(
  session: CollectQueueProgressView,
): "running" | "done" | "failed" | "stopped" {
  if (session.running) {
    return "running";
  }
  if (session.stoppedCount > 0) {
    return "stopped";
  }
  if (session.failedCount > 0) {
    return "failed";
  }
  return "done";
}
