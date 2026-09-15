export interface WebdavConfigPublic {
  serverUrl?: string;
  username?: string;
  passwordSet?: boolean;
  rootPath?: string;
  mediaType?: "movie" | "tv";
  tmdbApiKeySet?: boolean;
  tmdbBaseUrl?: string;
  scanIntervalMin?: number;
  minFileBytes?: number;
  playFromName?: string;
}

export interface WebdavScanSummary {
  found: number;
  tmdbHit: number;
  unmatched: number;
  skipped: number;
  status?: string;
  errorSummary?: string;
  lastScan?: string;
}

export interface FilmSource {
  id: string;
  name: string;
  uri: string;
  state: boolean;
  grade: number;
  isPosterSource?: boolean;
  interval: number;
  cd?: number;
  domainReplaceRules?: string;
  lastCollectTime?: string;
  progress?: CollectProgress | null;
  sourceType?: "maccms" | "webdav";
  webdav?: WebdavConfigPublic;
  scanSummary?: WebdavScanSummary | null;
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
  kind?: "maccms" | "webdav";
  phase?: "listing" | "parsing" | "matching" | "saving" | "done" | string;
  found?: number;
  parsed?: number;
  tmdbHit?: number;
  unmatched?: number;
  skipped?: number;
  error?: string;
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

export function isCollectWrapUpStatus(status?: string | null): boolean {
  return (
    status === "page_done" ||
    status === "waiting_publish" ||
    status === "finalizing"
  );
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
    case "page_done":
      return "分页完成";
    case "waiting_publish":
      return "等待收尾";
    case "finalizing":
      return "收尾发布中";
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
  if (progress.kind === "webdav") {
    if (progress.status === "running") {
      switch (progress.phase) {
        case "listing":
          return "目录扫描中";
        case "parsing":
          return "文件解析中";
        case "matching":
          return "TMDB与主站匹配中";
        case "saving":
          return "播放列表入库中";
        default:
          return "WebDAV扫描中";
      }
    }
    if (progress.status === "done") {
      return progress.failed > 0 ? "部分完成" : "扫描完成";
    }
    if (progress.status === "failed") {
      return progress.success > 0 ? "部分失败" : "扫描失败";
    }
  }
  if (isCollectWrapUpStatus(progress.status) && isPartialCollectFailure(progress)) {
    return progress.status === "finalizing" ? "部分失败，收尾发布中" : "部分失败，等待收尾";
  }
  if (progress.status === "failed") {
    return progress.success > 0 ? "部分失败" : "采集失败";
  }
  if (progress.status === "done" && progress.failed > 0) {
    return "部分完成";
  }
  return resolveCollectStatusText(progress.status);
}

export interface BatchOption {
  id: string;
  name: string;
  grade?: number;
  state?: boolean;
  sourceType?: "maccms" | "webdav";
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
  sourceType: "maccms" | "webdav";
  name: string;
  uri: string;
  state: boolean;
  grade: number;
  isPosterSource: boolean;
  interval: number;
  cd: number;
  domainReplaceRules?: string;
  webdav?: {
    serverUrl: string;
    username?: string;
    password?: string;
    passwordSet?: boolean;
    rootPath: string;
    mediaType?: "movie" | "tv";
    playFromName?: string;
    scanIntervalMin: number;
    minFileBytes: number;
    tmdbApiKey?: string;
    tmdbApiKeySet?: boolean;
    tmdbBaseUrl?: string;
  };
}

export const SOURCE_FORM_DEFAULTS: SourceFormValues = {
  sourceType: "maccms",
  name: "",
  uri: "",
  state: false,
  grade: 1,
  isPosterSource: false,
  interval: 0,
  cd: 24,
  domainReplaceRules: "",
  webdav: {
    serverUrl: "",
    username: "",
    password: "",
    rootPath: "/",
    mediaType: undefined,
    playFromName: "",
    scanIntervalMin: 0,
    minFileBytes: 52428800,
    tmdbApiKey: "",
    tmdbBaseUrl: "",
  },
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

/** 建议的采集站数量。超出不阻断添加，但需提示同时采集的性能影响。 */
export const RECOMMENDED_MAX_COLLECT_SOURCES = 12;

export const COLLECT_SOURCE_OVERLOAD_HINT =
  "采集站超过 12 个后，同时采集会明显增加 CPU、内存、数据库和网络压力，任务更容易排队、变慢或超时。可以继续添加，但建议只保留常用源。";

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

export interface WebdavScanReport {
  id: number;
  sourceId: string;
  startedAt: string;
  finishedAt: string;
  status: string;
  found: number;
  parsed: number;
  tmdbHit: number;
  unmatched: number;
  skipped: number;
  saved: number;
  deleted: number;
  failed: number;
  truncated: boolean;
  errorSummary?: string;
}

export interface WebdavScanItem {
  id: number;
  sourceId: string;
  pathHash: string;
  relPath: string;
  size: number;
  lastModified: string;
  fingerprint: string;
  groupKey: string;
  title: string;
  year: number;
  season: number;
  episode: number;
  tmdbId: number;
  status:
    | "scraped"
    | "bound"
    | "unmatched"
    | "category_mismatch"
    | "parse_failed"
    | "failed"
    | "missing";
  hint?: string;
  lastError?: string;
  createdAt?: string;
  updatedAt?: string;
}

export interface WebdavBindRequest {
  sourceId: string;
  itemIds: number[];
  tmdbId: number;
  mediaType: "movie" | "tv";
}

export interface WebdavRescrapeRequest {
  sourceId: string;
  itemIds?: number[];
  groupKey?: string;
}

