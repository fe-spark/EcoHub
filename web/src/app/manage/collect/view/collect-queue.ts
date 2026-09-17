import {
  isActiveCollectStatus,
  stationProgressPercent,
  type CollectProgress,
  type FilmSource,
} from "./types";

/** 无 queueId 的进度归入同一兜底队列，避免旧数据拆成 N 条。 */
export const UNTAGGED_COLLECT_QUEUE_ID = "__none";

export interface CollectQueueGroup {
  queueId: string;
  sourceIds: string[];
}

export interface CollectQueueProgressView {
  total: number;
  activeCount: number;
  doneCount: number;
  failedCount: number;
  stoppedCount: number;
  fetchingCount: number;
  wrappingCount: number;
  percent: number;
  success: number;
  failed: number;
  running: boolean;
  statsText: string;
}

export function collectQueueIdOf(progress?: CollectProgress | null): string | null {
  if (!progress) {
    return null;
  }
  const tagged = progress.queueId?.trim();
  return tagged || UNTAGGED_COLLECT_QUEUE_ID;
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
  activeIds: string[],
): CollectQueueProgressView | null {
  if (sourceIds.length === 0) {
    return null;
  }
  const byId = new Map(sites.map((item) => [item.id, item]));
  let percentSum = 0;
  let success = 0;
  let failed = 0;
  let fetchingCount = 0;
  let wrappingCount = 0;
  let doneCount = 0;
  let failedCount = 0;
  let stoppedCount = 0;
  let hasAnyProgress = false;

  for (const id of sourceIds) {
    const progress = byId.get(id)?.progress ?? null;
    if (progress) {
      hasAnyProgress = true;
      success += progress.success;
      failed += progress.failed;
      percentSum += stationProgressPercent(progress);
      const status = progress.status;
      if (status === "starting" || status === "running") {
        fetchingCount += 1;
      } else if (
        status === "page_done" ||
        status === "waiting_publish" ||
        status === "finalizing"
      ) {
        wrappingCount += 1;
      } else if (status === "failed") {
        failedCount += 1;
      } else if (status === "stopped") {
        stoppedCount += 1;
      } else {
        doneCount += 1;
      }
    } else if (activeIds.includes(id)) {
      fetchingCount += 1;
      hasAnyProgress = true;
    } else {
      doneCount += 1;
      percentSum += 100;
    }
  }

  const total = sourceIds.length;
  const activeCount = fetchingCount + wrappingCount;
  const percent = total > 0 ? Math.floor(percentSum / total) : 0;
  const running = activeCount > 0;
  if (!running && !hasAnyProgress) {
    return null;
  }

  const phaseParts: string[] = [`共 ${total} 站`];
  if (running) {
    if (fetchingCount > 0) {
      phaseParts.push(`采集中 ${fetchingCount}`);
    }
    if (wrappingCount > 0) {
      phaseParts.push(`收尾 ${wrappingCount}`);
    }
    if (doneCount > 0) {
      phaseParts.push(`完成 ${doneCount}`);
    }
    if (failedCount > 0) {
      phaseParts.push(`异常 ${failedCount}`);
    }
  } else {
    if (failedCount > 0) {
      phaseParts.push(`异常 ${failedCount}`);
    }
    if (stoppedCount > 0) {
      phaseParts.push(`终止 ${stoppedCount}`);
    }
    if (doneCount > 0 || (failedCount === 0 && stoppedCount === 0)) {
      phaseParts.push("已结束");
    }
  }
  if (success > 0 || failed > 0) {
    phaseParts.push(`已采集 ${success} 页`);
  }
  if (failed > 0) {
    phaseParts.push(`失败 ${failed} 页`);
  }

  return {
    total,
    activeCount,
    doneCount,
    failedCount,
    stoppedCount,
    fetchingCount,
    wrappingCount,
    percent: running ? Math.min(percent, 99) : Math.min(percent, 100),
    success,
    failed,
    running,
    statsText: phaseParts.join(" · "),
  };
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

export function newClientCollectQueueId(): string {
  return `q-local-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
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
