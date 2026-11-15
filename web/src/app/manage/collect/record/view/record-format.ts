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

export function formatSourceWindow(hour: number | undefined, batchHour: number): string {
  if (hour == null || hour === 0 || hour === batchHour) return "";
  if (hour === -1) return "全量";
  return hour > 0 ? `${hour} 小时` : "";
}

// 进行中的采集记录，单站用词和采集中心一致。批次结束后仍用账单用词。
export function liveSourceStatusLabel(status: string, success = 0, failed = 0): string | null {
  switch (status) {
    case "running":
      return "采集中";
    case "starting":
    case "waiting":
      return "等待中";
    case "paused":
      return "限流中";
    case "stopping":
      return "停止中";
    case "sealing":
      return "待检索";
    case "failed":
      return success > 0 ? "部分失败" : "采集失败";
    case "partial":
      return "部分失败";
    case "done":
    case "success":
      return failed > 0 ? "部分完成" : "采集完成";
    case "stopped":
      return "已停止";
    default:
      return null;
  }
}

export function formatCollectFilms(count: number, record?: { status?: string }) {
  if (count <= 0) {
    const status = record?.status ?? "";
    if (status === "running" || status === "starting" || status === "waiting") return "更新中";
    if (status === "interrupted") return "未结算";
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
      batchStatus === "success" ? "成功"
      : batchStatus === "failed" ? "失败"
      : batchStatus === "stopped" ? "已停止"
      : batchStatus === "interrupted" ? "异常中断"
      : batchStatus === "running" ? "进行中"
      : "";
    const tone = batchStatus === "success" ? "ok" : batchStatus === "failed" ? "fail" : "warn";
    return { primary: `${total} 站`, secondary, tone };
  }
  const buckets = {
    success: 0, failed: 0, partial: 0, stopped: 0, interrupted: 0,
    running: 0, sealing: 0, paused: 0, stopping: 0,
  };
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
      case "sealing":
        buckets.sealing++;
        break;
      case "paused":
        buckets.paused++;
        break;
      case "stopping":
        buckets.stopping++;
        break;
      default:
        buckets.running++;
        break;
    }
  }
  const total = buckets.success + buckets.failed + buckets.partial + buckets.stopped + buckets.interrupted
    + buckets.running + buckets.sealing + buckets.paused + buckets.stopping;
  if (total <= 0) {
    return { primary: "-", secondary: "", tone: "ok" };
  }

  const parts: { label: string; count: number }[] = [
    { label: "成功", count: buckets.success },
    { label: "失败", count: buckets.failed },
    { label: "部分失败", count: buckets.partial },
    { label: "已停止", count: buckets.stopped },
    { label: "异常中断", count: buckets.interrupted },
    { label: "采集中", count: buckets.running },
    { label: "待检索", count: buckets.sealing },
    { label: "限流中", count: buckets.paused },
    { label: "停止中", count: buckets.stopping },
  ].filter((part) => part.count > 0);
  const only = parts.length === 1 ? parts[0] : undefined;
  let secondary = parts.map((part) => `${part.count} ${part.label}`).join(" · ");
  if (only?.label === "成功") {
    secondary = "成功";
  } else if (only && total === 1) {
    secondary = only.label;
  } else if (only?.label === "失败") {
    secondary = "失败";
  } else if (only?.label === "已停止") {
    secondary = "已停止";
  } else if (only?.label === "异常中断") {
    secondary = "异常中断";
  } else if (only?.label === "采集中" || only?.label === "待检索" || only?.label === "限流中" || only?.label === "停止中") {
    secondary = only.label;
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
