export interface FilmSource {
  id: string;
  name: string;
  uri: string;
  state: boolean;
  sort?: number;
  isPosterSource?: boolean;
  isPrimary?: boolean;
  interval: number;
  cd?: number;
  format?: "json" | "xml";
  domainReplaceRules?: string;
  lastRunTime?: string;
  lastRunPageCount?: number;
  sitePageTotal?: number;
  progress?: CollectProgress | null;
  proxyCollect?: boolean;
  /** 该站是否已有分类副本。没有分类时采集中心不能开始采集。 */
  categoryReady?: boolean;
  /** 该站当前是否正在采集分类 */
  categorySyncing?: boolean;
  /** 该站当前是否正在执行删除 */
  deleting?: boolean;
  createdAt?: string;
}

export type CollectProgressStatus =
  | "starting"
  | "running"
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
  /** 同一次采集队列的标识；不同队列各自一条总进度 */
  queueId?: string;
  /** 本地 starting 记下的上一轮 queueId。空字符串表示当时没有上一轮。 */
  staleQueueId?: string;
}

/** 仍在拉页或排队。停止中的收尾不算，避免按钮提前变成开始采集。 */
export function isActiveCollectStatus(status?: string | null): boolean {
  return status === "starting" || status === "running";
}

/** 含停止后的收尾。这段时间按钮保持「停止采集」并显示 loading。 */
export function isCollectBusyStatus(status?: string | null): boolean {
  return isActiveCollectStatus(status) || status === "stopping";
}

export function isPartialCollectFailure(progress?: CollectProgress | null): boolean {
  return (progress?.success ?? 0) > 0 && (progress?.failed ?? 0) > 0;
}

export function resolveCollectStatusText(status?: string | null): string {
  switch (status) {
    case "starting":
      return "等待中";
    case "running":
      return "采集中";
    case "stopping":
      return "停止中";
    case "done":
      return "采集完成";
    case "failed":
      return "采集失败";
    case "stopped":
      return "已停止";
    default:
      return status ? String(status) : "采集中";
  }
}

export function resolveCollectProgressStatusText(progress?: CollectProgress | null): string {
  if (!progress) {
    return "暂无采集进度";
  }
  if (progress.status === "failed") {
    return progress.success > 0 ? "部分失败" : "采集失败";
  }
  if (progress.status === "done" && progress.failed > 0) {
    return "部分完成";
  }
  return resolveCollectStatusText(progress.status);
}

export interface CollectBatchItem {
  id: string;
  name?: string;
}

export interface CollectBatchResult {
  started: CollectBatchItem[];
  skipped: CleanupSkippedItem[];
}

/** 失效源检测结果项 */
export interface InvalidSourceItem {
  id: string;
  name: string;
  uri: string;
  sort?: number;
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
  format: "json" | "xml";
  isPosterSource: boolean;
  interval?: number;
  cd?: number;
  domainReplaceRules?: string;
  proxyCollect?: boolean;
}

export const SOURCE_FORM_DEFAULTS: SourceFormValues = {
  name: "",
  uri: "",
  state: true,
  format: "json",
  isPosterSource: false,
  interval: 0,
  cd: 24,
  domainReplaceRules: "",
  proxyCollect: false,
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

/** 超过该数量时弹窗提醒：排队、写库、快照和内存占用都会上升 */
export const COLLECT_SOURCE_WARN_COUNT = 12;

export interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
}

/** 点击停止后先标成停止中，避免服务端进度还没回来时按钮跳成开始采集。 */
export function progressAsStopping(
  progress: CollectProgress | null | undefined,
  id: string,
  name: string,
): CollectProgress {
  if (progress) {
    return { ...progress, status: "stopping" };
  }
  return { id, name, total: 0, current: 0, success: 0, failed: 0, status: "stopping" };
}

export function makeStartingProgress(
  id: string,
  name: string,
  queueId: string,
  staleQueueId?: string | null,
): CollectProgress {
  return {
    id,
    name,
    total: 0,
    current: 0,
    success: 0,
    failed: 0,
    status: "starting",
    queueId,
    staleQueueId: staleQueueId == null ? undefined : staleQueueId.trim(),
  };
}

export function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    state: Boolean(item.state),
    sort: Number(item.sort ?? 0),
    isPosterSource: Boolean(item.isPosterSource),
    isPrimary: Boolean(item.isPrimary),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd > 0 ? item.cd : 24),
    format: (item.format as "json" | "xml") || "json",
    lastRunTime: item.lastRunTime,
    lastRunPageCount: item.lastRunPageCount ? Number(item.lastRunPageCount) : 0,
    sitePageTotal: item.sitePageTotal ? Number(item.sitePageTotal) : 0,
    progress: item.progress ?? null,
    proxyCollect: Boolean(item.proxyCollect),
    categoryReady: item.categoryReady !== undefined ? Boolean(item.categoryReady) : true,
    categorySyncing: Boolean(item.categorySyncing),
    deleting: Boolean(item.deleting),
    createdAt: item.createdAt,
  };
}

