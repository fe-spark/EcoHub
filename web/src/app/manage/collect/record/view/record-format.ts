import dayjs from "dayjs";
import {
  CollectHistoryRecord,
  CollectRecordSourceDetail,
  isSingleFilmRecord,
  resolveBatchStatus,
} from "./types";

export const COLLECT_DURATION_MAP: Record<number, string> = {
  24: "采集今日",
  72: "采集三天",
  168: "采集一周",
  360: "采集半月",
  720: "采集一月",
  2160: "采集三月",
  4320: "采集半年",
  [-1]: "全量采集",
};

export function getHourLabel(hour: number, record?: { trigger?: string; tag?: string }): string {
  if (record && isSingleFilmRecord(record)) return "";
  if (COLLECT_DURATION_MAP[hour]) return COLLECT_DURATION_MAP[hour];
  if (hour === -1) return "全量采集";
  if (hour > 0) return `采集 ${hour} 小时`;
  return "";
}

export function formatDuration(ms: number): string {
  if (ms <= 0) return "< 1 秒";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const remainingSec = seconds % 60;
  if (minutes < 60) {
    return remainingSec > 0 ? `${minutes} 分 ${remainingSec} 秒` : `${minutes} 分钟`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMin = minutes % 60;
  return `${hours} 小时 ${remainingMin} 分`;
}

export function effectiveDurationMs(record: {
  status?: string;
  startedAt?: string;
  finishedAt?: string;
  durationMs: number;
}): number {
  if (record.durationMs > 0) return record.durationMs;
  if (record.finishedAt && record.startedAt) {
    return Math.max(0, new Date(record.finishedAt).getTime() - new Date(record.startedAt).getTime());
  }
  if (record.status === "running" && record.startedAt) {
    return Math.max(0, Date.now() - new Date(record.startedAt).getTime());
  }
  return 0;
}

export function formatSourceWindow(hour: number | undefined, batchHour: number): string {
  if (hour == null || hour === 0 || hour === batchHour) return "";
  if (hour === -1) return "全量";
  return hour > 0 ? `${hour} 小时` : "";
}

export function formatCollectFilms(count: number, record?: { status?: string }): string {
  if (count > 0) return `+${count} 部`;
  const status = record?.status ?? "";
  if (status === "running") return "更新中";
  if (status === "interrupted") return "未结算";
  return "无更新";
}

export function parseSourceDetails(detailJson?: string): CollectRecordSourceDetail[] {
  if (!detailJson) return [];
  try {
    const parsed = JSON.parse(detailJson);
    return Array.isArray(parsed) ? (parsed as CollectRecordSourceDetail[]) : [];
  } catch {
    return [];
  }
}

export function formatBatchSourceStat(
  record: { status?: string; sourceCount?: number },
  details: { status?: string }[],
): { primary: string; secondary: string; tone: "ok" | "fail" | "warn" } {
  const batchStatus = resolveBatchStatus({ status: record.status ?? "" });
  if (details.length === 0) {
    const total = Math.max(0, record.sourceCount ?? 0);
    if (total <= 0) return { primary: "-", secondary: "", tone: "ok" };
    const tone = batchStatus === "success" ? "ok" : batchStatus === "failed" ? "fail" : "warn";
    return { primary: `共 ${total} 站`, secondary: "", tone };
  }

  const buckets = { success: 0, failed: 0, partial: 0, stopped: 0, interrupted: 0, running: 0 };
  for (const detail of details) {
    switch (detail.status) {
      case "done":
      case "success":
        buckets.success++;
        break;
      case "failed":
        buckets.failed++;
        break;
      case "partial":
        buckets.partial++;
        break;
      case "stopped":
        buckets.stopped++;
        break;
      case "interrupted":
        buckets.interrupted++;
        break;
      default:
        buckets.running++;
        break;
    }
  }

  const total =
    buckets.success +
    buckets.failed +
    buckets.partial +
    buckets.stopped +
    buckets.interrupted +
    buckets.running;
  if (total <= 0) return { primary: "-", secondary: "", tone: "ok" };

  const parts = [
    { label: "成功", count: buckets.success },
    { label: "失败", count: buckets.failed },
    { label: "部分失败", count: buckets.partial },
    { label: "已停止", count: buckets.stopped },
    { label: "异常中断", count: buckets.interrupted },
    { label: "采集中", count: buckets.running },
  ].filter((part) => part.count > 0);

  // 所有站点状态单一且与批次状态一致时，不再重复输出拆分
  const secondary = parts.length > 1 ? parts.map((p) => `${p.count} ${p.label}`).join(" · ") : "";
  const tone =
    parts.length === 1 && parts[0].label === "成功"
      ? "ok"
      : batchStatus === "failed"
        ? "fail"
        : "warn";
  return { primary: `共 ${total} 站`, secondary, tone };
}

export function formatStartedAt(val: string) {
  if (!val) return { primary: "-", full: "" };
  const t = dayjs(val);
  if (!t.isValid()) return { primary: val, full: val };
  const formatted = t.format("YYYY-MM-DD HH:mm:ss");
  return { primary: formatted, full: formatted };
}

// 旧记录把失败页缩成「第 1, 2... 页」，并可能带截断的 url=...；页码由 failedPages 完整展示，这里只留抓取原因。
export function collectErrorReason(text: string): string {
  let message = text.trim();
  const marker = "抓取失败:";
  const at = message.lastIndexOf(marker);
  if (at >= 0) {
    message = message.slice(at + marker.length).trim();
  }
  message = message.replace(/共\s*\d+\s*页失败\s*[（(]第[\s\S]*?\.\.\.\s*页[）)]/g, "").trim();
  message = message.replace(/,\s*url=https?:\/\/\S+$/i, "").trim();
  return message;
}

const FETCH_ERROR = /^(GET|POST|PUT|DELETE|HEAD|PATCH)\s+"([^"]+)"\s*:\s*([\s\S]+)$/i;

const REASON_LABELS: Array<[RegExp, string]> = [
  [/context deadline exceeded|client\.timeout exceeded|i\/o timeout/i, "请求超时"],
  [/context canceled/i, "请求已取消"],
  [/connection refused/i, "连接被拒绝"],
  [/no such host/i, "域名无法解析"],
  [/tls handshake timeout/i, "TLS 握手超时"],
  [/unexpected eof|^eof$/i, "连接中断"],
  [/certificate/i, "证书校验失败"],
  [/network is unreachable|no route to host/i, "网络不可达"],
];

export function parseCollectError(text: string): {
  summary: string;
  detail: string;
  method: string;
  url: string;
} {
  const message = collectErrorReason(text);
  const matched = message.match(FETCH_ERROR);
  const detail = (matched?.[3] ?? message).trim();
  const summary = REASON_LABELS.find(([pattern]) => pattern.test(detail))?.[1] ?? "";
  return {
    summary,
    detail,
    method: matched ? matched[1].toUpperCase() : "",
    url: matched?.[2] ?? "",
  };
}

function isDetailAbnormal(detail: CollectRecordSourceDetail): boolean {
  return (
    detail.status === "failed" ||
    detail.status === "partial" ||
    detail.status === "interrupted" ||
    Boolean(detail.error?.trim())
  );
}

/** 格式化批次分页进度：展示成功/总页数、失败页数与失败页码预览。 */
export function formatBatchPageSummary(
  record: Pick<
    CollectHistoryRecord,
    "status" | "trigger" | "tag" | "pageTotal" | "pageSuccess" | "pageFailed"
  >,
  details: CollectRecordSourceDetail[],
): {
  primary: string;
  failedText: string;
  secondary: string;
  tone: "ok" | "fail" | "running" | "muted";
} {
  const total = Math.max(0, record.pageTotal ?? 0);
  const success = Math.max(0, record.pageSuccess ?? 0);
  const failed = Math.max(0, record.pageFailed ?? 0);

  if (record.status === "running") {
    if (total > 0) {
      const pct = Math.min(100, Math.round((success / total) * 100));
      return {
        primary: `${success} / ${total} 页`,
        failedText: failed > 0 ? `失败 ${failed} 页` : "",
        secondary: `已完成 ${pct}%`,
        tone: failed > 0 ? "fail" : "running",
      };
    }
    return { primary: "探测页数中", failedText: "", secondary: "", tone: "running" };
  }

  if (isSingleFilmRecord(record)) {
    if (failed > 0) {
      return { primary: "校验失败", failedText: "", secondary: "", tone: "fail" };
    }
    return {
      primary: success > 0 || total > 0 ? "已完成校验" : "-",
      failedText: "",
      secondary: "",
      tone: success > 0 || total > 0 ? "ok" : "muted",
    };
  }

  if (total === 0 && success === 0 && failed === 0) {
    return { primary: "-", failedText: "", secondary: "", tone: "muted" };
  }

  const primary = total > 0 ? `${success} / ${total} 页` : `成功 ${success} 页`;
  const failedText = failed > 0 ? `失败 ${failed} 页` : "";

  if (failed > 0) {
    const failedSources = details.filter(
      (d) => Array.isArray(d.failedPages) && d.failedPages.length > 0,
    );
    let secondary = "";
    if (failedSources.length === 1) {
      const pages = failedSources[0].failedPages!;
      const preview = pages.slice(0, 4).join(", ");
      secondary = `失败页：${preview}${pages.length > 4 ? ` 等 ${pages.length} 页` : ""}`;
    } else if (failedSources.length > 1) {
      secondary = `${failedSources.length} 个站点存在失败页`;
    }
    return { primary, failedText, secondary, tone: "fail" };
  }

  if (total > 0) {
    const pct = Math.min(100, Math.round((success / total) * 100));
    return {
      primary,
      failedText: "",
      secondary: pct === 100 ? "100% 完成" : `完成 ${pct}%`,
      tone: "ok",
    };
  }

  return { primary, failedText: "", secondary: "", tone: "ok" };
}

/** 提取当前主站或单站的去重入库指标，避免多站重复片跨站累加失真。 */
export function resolveBatchFilmIngress(
  record: Pick<CollectHistoryRecord, "status" | "trigger" | "tag" | "filmCount" | "sourceCount">,
  details: CollectRecordSourceDetail[],
  primaryId?: string,
): { label: string; subLabel: string; tone: "active" | "muted" } {
  if (record.status === "running") {
    return { label: "采集中", subLabel: "", tone: "muted" };
  }

  if (isSingleFilmRecord(record)) {
    const count = Math.max(0, record.filmCount ?? 0);
    return {
      label: count > 0 ? `+${count.toLocaleString("zh-CN")} 部` : "无变更",
      subLabel: "单片校验",
      tone: count > 0 ? "active" : "muted",
    };
  }

  if (details.length === 1) {
    const single = details[0];
    const count = Math.max(0, single.filmsTotal ?? record.filmCount ?? 0);
    const isPrimary = Boolean(primaryId && single.sourceId === primaryId);
    return {
      label:
        count > 0
          ? `+${count.toLocaleString("zh-CN")} 部`
          : formatCollectFilms(0, { status: record.status }),
      subLabel: isPrimary ? "主站入库" : "单站入库",
      tone: count > 0 ? "active" : "muted",
    };
  }

  if (details.length > 1) {
    if (!primaryId) {
      return { label: "—", subLabel: "未配置主站", tone: "muted" };
    }
    const primaryDetail = details.find((d) => d.sourceId === primaryId);
    if (!primaryDetail) {
      return { label: "未含主站", subLabel: "多站批采", tone: "muted" };
    }
    const count = Math.max(0, primaryDetail.filmsTotal ?? 0);
    return {
      label:
        count > 0
          ? `+${count.toLocaleString("zh-CN")} 部`
          : formatCollectFilms(0, { status: record.status }),
      subLabel: `主站「${primaryDetail.sourceName || primaryDetail.sourceId}」`,
      tone: count > 0 ? "active" : "muted",
    };
  }

  const totalSites = Math.max(0, record.sourceCount ?? 0);
  if (totalSites <= 1) {
    const count = Math.max(0, record.filmCount ?? 0);
    return {
      label:
        count > 0
          ? `+${count.toLocaleString("zh-CN")} 部`
          : formatCollectFilms(0, { status: record.status }),
      subLabel: "",
      tone: count > 0 ? "active" : "muted",
    };
  }

  return { label: "—", subLabel: "历史批次", tone: "muted" };
}

export interface BatchErrorPreview {
  sourceName: string;
  summary: string;
  detail: string;
  abnormalCount: number;
  modalTitle: string;
  modalText: string;
  modalPages?: number[];
}

/** 提取批次首条异常站点的结构化错误预览，支持主表一键查看详情。 */
export function resolveBatchErrorPreview(
  record: Pick<CollectHistoryRecord, "status" | "errorMsg">,
  details: CollectRecordSourceDetail[],
): BatchErrorPreview | null {
  const abnormalSources = details.filter(isDetailAbnormal);
  const firstWithDetail =
    abnormalSources.find((d) => Boolean(d.error?.trim() || (d.failedPages?.length ?? 0) > 0)) ||
    abnormalSources[0];

  const rawText = firstWithDetail?.error?.trim() || record.errorMsg?.trim() || "";
  const pages = firstWithDetail?.failedPages;
  if (!rawText && (!pages || pages.length === 0) && abnormalSources.length === 0) {
    return null;
  }

  const parsed = parseCollectError(rawText);
  const sourceName = firstWithDetail ? firstWithDetail.sourceName || firstWithDetail.sourceId : "";
  const fallbackDetail =
    pages && pages.length > 0 ? `失败 ${pages.length} 页` : "存在异常站点";

  return {
    sourceName,
    summary: parsed.summary,
    detail: parsed.detail || fallbackDetail,
    abnormalCount: abnormalSources.length || (rawText ? 1 : 0),
    modalTitle: sourceName ? `${sourceName} · 异常原因` : "异常原因",
    modalText: rawText,
    modalPages: pages,
  };
}
