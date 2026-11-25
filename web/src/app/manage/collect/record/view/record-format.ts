import dayjs from "dayjs";
import { CollectRecordSourceDetail, isSingleFilmRecord, resolveBatchStatus } from "./types";

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
