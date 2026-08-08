export interface FilmSource {
  id: string;
  name: string;
  uri: string;
  syncPictures: boolean;
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
  syncPictures: boolean;
  state: boolean;
  grade: number;
  interval: number;
}

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
