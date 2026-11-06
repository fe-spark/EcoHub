import { resolveCollectProgressStatusText, type CollectProgress } from "./types";

export type RingTone = "primary" | "processing" | "success" | "warning" | "error";
export type RingCenter = "empty" | "percent" | "success" | "fail";

export interface RingView {
  percent: number;
  tone: RingTone;
  solid: boolean;
  center: RingCenter;
  tooltip: string;
}

function collectPercent(progress: CollectProgress): number {
  const total = Math.max(progress.total ?? 0, 0);
  const failed = progress.failed ?? 0;
  const done = Math.min(Math.max((progress.success ?? 0) + failed, 0), total || failed);
  if (progress.status === "done") return 100;
  if (progress.status === "starting") return 0;
  if (total <= 0) return 0;
  return Math.min(Math.floor((done / total) * 100), 99);
}

function collectTooltip(progress: CollectProgress): string {
  const total = Math.max(progress.total ?? 0, 0);
  const failed = progress.failed ?? 0;
  const finished = Math.max((progress.success ?? 0) + failed, 0);
  const done = Math.min(finished, total || finished);
  const zeroPageFinished =
    total === 0 &&
    (progress.status === "done" || progress.status === "failed" || progress.status === "stopped");
  const progressText =
    total > 0
      ? `${done}/${total} 页`
      : zeroPageFinished
        ? "无新内容"
        : done > 0
          ? `${done} 页`
          : progress.status === "starting"
            ? "排队中"
            : "即将开始采集";
  const failedPages = progress.failedPages ?? [];
  const failedInfo =
    failed > 0
      ? ` · 失败 ${failed} 页${failedPages.length > 0 ? `（${failedPages.join("、")}）` : ""}`
      : "";
  const countLine = `${progressText}${failedInfo}`;
  return `${resolveCollectProgressStatusText(progress)} · ${countLine}`;
}

/** 封口等待环优先，其余按采集进度渲染。 */
export function resolveRingView(progress?: CollectProgress | null): RingView | null {
  if (!progress) return null;
  if (progress.status === "sealing") {
    return {
      percent: 0,
      tone: "processing",
      solid: true,
      center: "empty",
      tooltip: "待检索",
    };
  }
  const failed = progress.failed ?? 0;
  const fullFail = progress.status === "failed" && (progress.success ?? 0) === 0;
  const partialFail = failed > 0 && !fullFail;
  const scraping =
    progress.status === "starting" ||
    progress.status === "running" ||
    progress.status === "stopping";
  const tooltip = collectTooltip(progress);
  if (fullFail) {
    return { percent: collectPercent(progress), tone: "error", solid: false, center: "fail", tooltip };
  }
  if (progress.status === "done") {
    return {
      percent: 100,
      tone: partialFail ? "warning" : "success",
      solid: false,
      center: "success",
      tooltip,
    };
  }
  return {
    percent: collectPercent(progress),
    // 采集中的失败页用错误色，和正常采集的主色拉开。结束后的部分失败仍用警告色。
    tone: partialFail ? (scraping ? "error" : "warning") : "primary",
    solid: false,
    center: "percent",
    tooltip,
  };
}
