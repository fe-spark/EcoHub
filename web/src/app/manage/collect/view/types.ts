export interface FilmSource {
  id: string;
  name: string;
  uri: string;
  state: boolean;
  sort?: number;
  isPosterSource?: boolean;
  isPrimary?: boolean;
  cd?: number;
  format?: "json" | "xml";
  domainReplaceRules?: string;
  /** 同一站相邻两次抓页的最小间隔，毫秒。0 为不等待。 */
  collectIntervalMs?: number;
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
  | "stopping"
  | "sealing"
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
  /** 服务端分配的采集队列标识；同一次派发的站点共用一个。 */
  queueId?: string;
  /** 本轮采集失败的页码，按抓取顺序记录。 */
  failedPages?: number[];
  /** 本轮的检索索引封口已完成。 */
  sealed?: boolean;
}

/** 仍在拉页、排队，或限流暂停中。停止中的收尾不算，避免按钮提前变成开始采集。 */
export function isActiveCollectStatus(status?: string | null): boolean {
  return status === "starting" || status === "running" || status === "paused";
}

/** 抓页结束、等检索索引封口。 */
export function isSealingStatus(status?: string | null): boolean {
  return status === "sealing";
}

/** 含停止后的收尾。这段时间按钮保持「停止采集」并显示 loading。 */
export function isCollectBusyStatus(status?: string | null): boolean {
  return isActiveCollectStatus(status) || status === "stopping";
}

/** 抓页、停止收尾、待检索都算采集中。 */
export function isCollectingStatus(status?: string | null): boolean {
  return isCollectBusyStatus(status) || isSealingStatus(status);
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
    case "paused":
      return "限流中";
    case "stopping":
      return "停止中";
    case "sealing":
      return "待检索";
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
  if (progress.status === "paused") {
    return "限流中";
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
  cd?: number;
  domainReplaceRules?: string;
  proxyCollect?: boolean;
  collectIntervalMs?: number;
}

/** 新采集站的抓页间隔。0 仍表示不等待。 */
export const COLLECT_INTERVAL_MS_DEFAULT = 200;

export const SOURCE_FORM_DEFAULTS: SourceFormValues = {
  name: "",
  uri: "",
  state: true,
  format: "json",
  isPosterSource: false,
  cd: 24,
  domainReplaceRules: "",
  proxyCollect: false,
  collectIntervalMs: COLLECT_INTERVAL_MS_DEFAULT,
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

export function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    state: Boolean(item.state),
    sort: Number(item.sort ?? 0),
    isPosterSource: Boolean(item.isPosterSource),
    isPrimary: Boolean(item.isPrimary),
    cd: Number(item.cd > 0 ? item.cd : 24),
    format: (item.format as "json" | "xml") || "json",
    lastRunTime: item.lastRunTime,
    lastRunPageCount: item.lastRunPageCount ? Number(item.lastRunPageCount) : 0,
    sitePageTotal: item.sitePageTotal ? Number(item.sitePageTotal) : 0,
    progress: item.progress ?? null,
    proxyCollect: Boolean(item.proxyCollect),
    collectIntervalMs: Math.max(0, Number(item.collectIntervalMs) || 0),
    categoryReady: item.categoryReady !== undefined ? Boolean(item.categoryReady) : true,
    categorySyncing: Boolean(item.categorySyncing),
    deleting: Boolean(item.deleting),
    createdAt: item.createdAt,
  };
}

export interface SourceIncident {
  sourceId: string;
  sourceName: string;
  consecutiveFailures: number;
  latestFailedAt: string;
  latestError?: string;
  latestBatchId: string;
  tripped: boolean;
}

