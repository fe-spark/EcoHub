import dayjs from "dayjs";
import { resolveBatchStatus } from "./types";

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

function pageMissed(succ: number, fail: number, total: number): number {
  if (total <= 0) return 0;
  return Math.max(0, total - succ - fail);
}

/** 终态页账：成功、失败、未抓、总页。运行中不要把还没轮到的页写成未抓。 */
export function formatPageLedger(succ: number, fail: number, total: number): string {
  const missed = pageMissed(succ, fail, total);
  const parts = [`成功 ${succ}`];
  if (fail > 0) parts.push(`失败 ${fail}`);
  if (missed > 0) parts.push(`未抓 ${missed}`);
  if (total > 0) parts.push(`共 ${total}`);
  return `${parts.join(" · ")} 页`;
}

export function formatCollectPages(record: {
  status?: string;
  pageSuccess?: number;
  pageTotal?: number;
  pageFailed?: number;
}): { primary: string; secondary: string; alert: boolean } {
  const succ = Math.max(0, record.pageSuccess ?? 0);
  const total = Math.max(0, record.pageTotal ?? 0);
  const fail = Math.max(0, record.pageFailed ?? 0);
  const status = record.status ?? "";
  const missed = pageMissed(succ, fail, total);
  const ledger = formatPageLedger(succ, fail, total);
  const isOngoing = status === "running" || status === "starting" || status === "waiting";

  if (status === "stopped" || status === "interrupted") {
    return {
      primary: status === "interrupted" ? "异常中断" : "已停止",
      secondary: total > 0 || succ > 0 || fail > 0 ? ledger : "已停止",
      alert: fail > 0,
    };
  }
  if (status === "failed" && succ === 0 && fail === 0) {
    return {
      primary: "未能拉取",
      secondary: total === 0 ? "探测失败" : ledger,
      alert: true,
    };
  }
  if (isOngoing) {
    const percent = total > 0 ? Math.min(100, Math.floor((succ / total) * 100)) : 0;
    const failNote = fail > 0 ? ` · 失败 ${fail}` : "";
    return {
      primary: total > 0 ? `成功 ${succ} / 共 ${total} 页` : `已拉取 ${succ} 页`,
      secondary: total > 0 ? `拉取中 · ${percent}%${failNote}` : "正在探测页码",
      alert: fail > 0,
    };
  }
  if (fail > 0) {
    return { primary: `${fail} 页失败`, secondary: ledger, alert: true };
  }
  if (missed > 0) {
    return { primary: `${missed} 页未抓`, secondary: ledger, alert: false };
  }
  return { primary: `共 ${total} 页`, secondary: "全部完成", alert: false };
}

export function formatSourcePages(item: {
  status?: string;
  pageTotal?: number;
  successCnt?: number;
  failedCnt?: number;
  failedPages?: number[];
  error?: string;
}): string {
  const total = Math.max(0, item.pageTotal ?? 0);
  const succ = Math.max(0, item.successCnt ?? 0);
  const fail = Math.max(0, item.failedCnt ?? 0, item.failedPages?.length ?? 0);
  const isOngoing = item.status === "running" || item.status === "starting" || item.status === "waiting";

  if (total <= 0) {
    if (fail > 0) return `失败 ${fail} 页`;
    const isProbeErr = item.error ? item.error.includes("探测") : true;
    if (item.status === "failed") return isProbeErr ? "探测失败" : "抓取失败";
    if (item.status === "stopped") return "已停止";
    if (item.status === "interrupted") return "中断";
    if (isOngoing) return "探测页码中";
    return "-";
  }
  if (isOngoing) {
    const parts = [`成功 ${succ} / 共 ${total} 页`];
    if (fail > 0) parts.push(`失败 ${fail} 页`);
    return parts.join("，");
  }
  const ledger = formatPageLedger(succ, fail, total);
  if (item.status === "stopped") return `已停止 · ${ledger}`;
  if (item.status === "interrupted") return `中断 · ${ledger}`;
  return ledger;
}

export function formatSourceWindow(hour: number | undefined, batchHour: number): string {
  if (hour == null || hour === 0 || hour === batchHour) return "";
  if (hour === -1) return "全量";
  return hour > 0 ? `${hour} 小时` : "";
}

export function formatCollectFilms(count: number, record?: { status?: string }) {
  if (count <= 0) {
    const status = record?.status ?? "";
    if (status === "running" || status === "starting" || status === "waiting") return "更新中";
    return "无更新";
  }
  return `+${count} 部`;
}

export function formatBatchSourceStat(
  record: {
    status?: string;
    sourceCount?: number;
    failedSources?: number;
    successSources?: number;
    pageSuccess?: number;
    pageFailed?: number;
  },
  details: { status?: string }[],
): { primary: string; secondary: string; tone: "ok" | "fail" | "warn" } {
  const batchStatus = resolveBatchStatus({
    status: record.status ?? "",
    pageSuccess: record.pageSuccess,
    pageFailed: record.pageFailed,
    failedSources: record.failedSources,
    successSources: record.successSources,
  });
  if (details.length === 0) {
    const total = Math.max(0, record.sourceCount ?? 0);
    if (total <= 0) return { primary: "-", secondary: "", tone: "ok" };
    const secondary =
      batchStatus === "success" ? "全部成功"
      : batchStatus === "failed" ? "全部失败"
      : batchStatus === "stopped" ? "已停止"
      : batchStatus === "interrupted" ? "中断"
      : batchStatus === "running" ? "采集中"
      : "";
    const tone = batchStatus === "success" ? "ok" : batchStatus === "failed" ? "fail" : "warn";
    return { primary: `${total} 站`, secondary, tone };
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
  const total = buckets.success + buckets.failed + buckets.partial + buckets.stopped + buckets.interrupted + buckets.running;
  if (total <= 0) {
    return { primary: "-", secondary: "", tone: "ok" };
  }

  const parts: { label: string; count: number }[] = [
    { label: "成功", count: buckets.success },
    { label: "失败", count: buckets.failed },
    { label: "部分失败", count: buckets.partial },
    { label: "已停止", count: buckets.stopped },
    { label: "中断", count: buckets.interrupted },
    { label: "采集中", count: buckets.running },
  ].filter((part) => part.count > 0);
  const only = parts.length === 1 ? parts[0] : undefined;
  let secondary = parts.map((part) => `${part.count} ${part.label}`).join(" · ");
  if (only?.label === "成功") {
    secondary = "全部成功";
  } else if (only && total === 1) {
    secondary = only.label;
  } else if (only?.label === "失败") {
    secondary = "全部失败";
  } else if (only?.label === "已停止") {
    secondary = "全部已停止";
  } else if (only?.label === "中断") {
    secondary = "全部中断";
  } else if (only?.label === "采集中") {
    secondary = "采集中";
  }

  const tone = only?.label === "成功" ? "ok" : batchStatus === "failed" ? "fail" : "warn";
  return { primary: `${total} 站`, secondary, tone };
}

export function formatStartedAt(val: string) {
  if (!val) return { primary: "-", full: "" };
  const t = dayjs(val);
  if (!t.isValid()) return { primary: val, full: val };
  return { primary: t.format("YYYY-MM-DD HH:mm:ss"), full: t.format("YYYY-MM-DD HH:mm:ss") };
}
