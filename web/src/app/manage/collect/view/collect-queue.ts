import type { CollectProgress, FilmSource } from "./types";

export interface CollectQueueGroup {
  queueId: string;
  sourceIds: string[];
}

/** 检索索引封口的全局状态，只用来给队列条展示封口进度。 */
export interface QueueSealState {
  status: string;
  percent: number;
  current: number;
  total: number;
}

export interface CollectQueueProgressView {
  queueId?: string;
  total: number;
  doneCount: number;
  failedCount: number;
  stoppedCount: number;
  sealingCount: number;
  runningCount: number;
  stoppingCount: number;
  pageDone: number;
  pageTotal: number;
  failed: number;
  /** 还有站点在抓页或收尾 */
  running: boolean;
  /** 有站点采集完在等检索索引 */
  sealing: boolean;
  /** 检索索引正在写入 */
  sealIndexing: boolean;
  /** 检索索引封口已完成 */
  sealed: boolean;
  sealPercent: number;
  sealCurrent: number;
  sealTotal: number;
  percent: number;
}

/** 抓取或封口未结束都算队列仍活跃。 */
export function isActiveQueueView(view: CollectQueueProgressView): boolean {
  return view.running || view.sealing;
}

export function collectQueueIdOf(progress?: CollectProgress | null): string | null {
  const queueId = progress?.queueId?.trim();
  return queueId || null;
}

/** 按服务端下发的 queueId 分组。没有 queueId 的进度不会出现在队列条上。 */
export function groupCollectQueues(sites: FilmSource[]): CollectQueueGroup[] {
  const order: string[] = [];
  const byId = new Map<string, string[]>();
  for (const site of sites) {
    const queueId = collectQueueIdOf(site.progress);
    if (!queueId) continue;
    const ids = byId.get(queueId);
    if (ids) {
      ids.push(site.id);
      continue;
    }
    order.push(queueId);
    byId.set(queueId, [site.id]);
  }
  return order.map((queueId) => ({ queueId, sourceIds: byId.get(queueId) ?? [] }));
}

export function computeCollectQueueProgress(
  sourceIds: string[],
  sites: FilmSource[],
  seal?: QueueSealState | null,
): CollectQueueProgressView | null {
  if (sourceIds.length === 0) return null;
  const byId = new Map(sites.map((item) => [item.id, item]));
  let pageDone = 0;
  let pageTotal = 0;
  let failed = 0;
  let runningCount = 0;
  let stoppingCount = 0;
  let sealingCount = 0;
  let doneCount = 0;
  let failedCount = 0;
  let stoppedCount = 0;
  let sealedCount = 0;
  let counted = 0;

  for (const id of sourceIds) {
    const progress = byId.get(id)?.progress;
    if (!progress) continue;
    counted += 1;
    failed += progress.failed;
    if (progress.sealed) sealedCount += 1;
    const pages = progress.total;
    if (pages > 0) {
      pageTotal += pages;
      pageDone += Math.min(progress.success + progress.failed, pages);
    }
    switch (progress.status) {
      case "starting":
      case "running":
        runningCount += 1;
        break;
      case "stopping":
        stoppingCount += 1;
        break;
      case "sealing":
        sealingCount += 1;
        break;
      case "failed":
        failedCount += 1;
        break;
      case "stopped":
        stoppedCount += 1;
        break;
      default:
        doneCount += 1;
    }
  }

  if (counted === 0) return null;

  const running = runningCount + stoppingCount > 0;
  const sealing = sealingCount > 0;
  const sealIndexing = sealing && seal?.status === "indexing";
  // 封口已完成时进度条停在 100%，不再倒回抓页进度。
  const sealed = !running && !sealing && sealedCount > 0;
  const pagePercent = pageTotal > 0 ? Math.floor((pageDone / pageTotal) * 100) : 0;
  const percent = running
    ? Math.min(pagePercent, 99)
    : sealIndexing
      ? Math.max(0, Math.min(seal?.percent ?? 0, 100))
      : sealing
        ? 0
        : sealed
          ? 100
          : Math.min(pagePercent, 100);

  return {
    total: sourceIds.length,
    doneCount,
    failedCount,
    stoppedCount,
    sealingCount,
    runningCount,
    stoppingCount,
    pageDone,
    pageTotal,
    failed,
    running,
    sealing,
    sealIndexing,
    sealed,
    sealPercent: seal?.percent ?? 0,
    sealCurrent: seal?.current ?? 0,
    sealTotal: seal?.total ?? 0,
    percent,
  };
}

export type QueueEndLevel = "success" | "info" | "warning" | "error";

export function queueEndNotice(view: CollectQueueProgressView): {
  level: QueueEndLevel;
  text: string;
} {
  // 有站点正常跑完时，个别站被终止只算部分终止，整体仍按完成收尾。
  if (view.doneCount > 0) {
    if (view.failedCount > 0) {
      return { level: "warning", text: "采集部分失败" };
    }
    return { level: "success", text: "采集完成" };
  }
  if (view.failedCount > 0) {
    return { level: "error", text: "采集失败" };
  }
  if (view.stoppedCount > 0) {
    return { level: "info", text: "采集已停止" };
  }
  return { level: "success", text: "采集完成" };
}

export function queueProgressTitle(
  running: boolean,
  queueCount: number,
  seq: number,
): string {
  const name = queueCount > 1 ? `队列 ${seq} 总进度` : "队列总进度";
  if (running) return name;
  return `${name} · 已结束`;
}

export function tourProgressPhase(
  session: CollectQueueProgressView,
): "running" | "done" | "failed" | "stopped" {
  if (isActiveQueueView(session)) return "running";
  if (session.doneCount > 0) return "done";
  if (session.failedCount > 0) return "failed";
  if (session.stoppedCount > 0) return "stopped";
  return "done";
}

/** 进度快照里同一条进度字段一致时复用旧对象，避免每 300ms 全量重渲染。 */
export function sameCollectProgress(
  a?: CollectProgress | null,
  b?: CollectProgress | null,
): boolean {
  if (a === b) return true;
  if (!a || !b) return false;
  return (
    a.id === b.id &&
    a.name === b.name &&
    a.status === b.status &&
    a.total === b.total &&
    a.current === b.current &&
    a.success === b.success &&
    a.failed === b.failed &&
    a.queueId === b.queueId &&
    a.sealed === b.sealed &&
    samePageNumbers(a.failedPages, b.failedPages)
  );
}

function samePageNumbers(a?: number[], b?: number[]): boolean {
  const left = a ?? [];
  const right = b ?? [];
  if (left.length !== right.length) return false;
  for (let i = 0; i < left.length; i += 1) {
    if (left[i] !== right[i]) return false;
  }
  return true;
}
