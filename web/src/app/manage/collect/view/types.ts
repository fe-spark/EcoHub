export interface FilmSource {
  id: string;
  name: string;
  uri: string;
  state: boolean;
  grade: number;
  interval: number;
  cd?: number;
  lastCollectTime?: string;
  progress?: CollectProgress | null;
}

export type CollectProgressStatus =
  | "starting"
  | "running"
  | "page_done"
  | "waiting_publish"
  | "finalizing"
  | "done"
  | "failed"
  | "stopped"
  | string;

export interface CollectProgress {
  id: string;
  name: string;
  total: number;
  current: number;
  success: number;
  failed: number;
  status: CollectProgressStatus;
}

/** 仍处于采集生命周期、列表应展示进度的状态 */
export function isActiveCollectStatus(status?: string | null): boolean {
  return (
    status === "starting" ||
    status === "running" ||
    status === "page_done" ||
    status === "waiting_publish" ||
    status === "finalizing"
  );
}

export function resolveCollectStatusText(status?: string | null): string {
  switch (status) {
    case "starting":
      return "等待中";
    case "running":
      return "采集中";
    case "page_done":
      return "分页完成";
    case "waiting_publish":
      return "等待收尾";
    case "finalizing":
      return "收尾发布中";
    case "done":
      return "已完成";
    case "failed":
      return "失败";
    case "stopped":
      return "已停止";
    default:
      return status ? String(status) : "采集中";
  }
}

export interface BatchOption {
  id: string;
  name: string;
  grade?: number;
  state?: boolean;
}

/** 失效源检测结果项 */
export interface InvalidSourceItem {
  id: string;
  name: string;
  uri: string;
  grade: number;
  state: boolean;
  reason: string;
}

/** 检测或删除时被跳过的采集站 */
export interface CleanupSkippedItem {
  id: string;
  name?: string;
  reason: string;
}

export interface CheckAllResult {
  checked: number;
  ok: number;
  failed: InvalidSourceItem[];
  skipped: CleanupSkippedItem[];
}

export interface DelBatchResult {
  deleted: string[];
  skipped: CleanupSkippedItem[];
}

export interface SourceFormValues {
  name: string;
  uri: string;
  state: boolean;
  grade: number;
  interval: number;
  cd: number;
}

export const SOURCE_FORM_DEFAULTS: SourceFormValues = {
  name: "",
  uri: "",
  state: false,
  grade: 1,
  interval: 0,
  cd: 24,
};

export const collectDuration = [
  { label: "采集今日", time: 24 },
  { label: "采集三天", time: 72 },
  { label: "采集一周", time: 168 },
  { label: "采集半月", time: 360 },
  { label: "采集一月", time: 720 },
  { label: "采集三月", time: 2160 },
  { label: "采集半年", time: 4320 },
  { label: "全量采集", time: -1 },
];

/** 采集站数量上限（前后端一致） */
export const MAX_COLLECT_SOURCES = 12;

/**
 * 单站进度百分比。
 * 活跃态与 CollectProgressView 一致；终态（done/failed/stopped）计 100%，便于批量总进度收口。
 */
export function stationProgressPercent(progress?: CollectProgress | null): number {
  if (!progress) {
    return 0;
  }
  if (
    progress.status === "done" ||
    progress.status === "failed" ||
    progress.status === "stopped"
  ) {
    return 100;
  }
  const total = Math.max(progress.total, 0);
  const finished = Math.max(progress.success + progress.failed, 0);
  const done = Math.min(finished, total || finished);
  const rawPercent = total > 0 ? Math.floor((done / total) * 100) : 0;
  const inPostPagePhase =
    progress.status === "page_done" ||
    progress.status === "waiting_publish" ||
    progress.status === "finalizing";
  const zeroPageFinished = total === 0 && inPostPagePhase;
  if (inPostPagePhase || zeroPageFinished) {
    return 99;
  }
  if (progress.status === "starting") {
    return 0;
  }
  return Math.min(rawPercent, 99);
}
