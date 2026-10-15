import {
  resolveCollectProgressStatusText,
  type FilmSource,
} from "./types";

export type StatusTone = "running" | "enabled" | "disabled" | "stopping" | "failed";

export function resolveSourceStatus(
  record: FilmSource,
  active: boolean,
): { label: string; tone: StatusTone } {
  if (record.categorySyncing) {
    return { label: "分类采集中", tone: "running" };
  }

  if (record.categoryReady === false) {
    return { label: "缺分类", tone: "disabled" };
  }

  const phase = record.progress?.status;

  if (active) {
    if (!record.state) {
      return { label: "已终止·等待完成", tone: "stopping" };
    }
    if (phase === "starting") {
      return { label: "排队中", tone: "running" };
    }
    return { label: "采集中", tone: "running" };
  }

  // 非活跃态：如果仍有进度残留（倒计时保留期或异常），反映真实终态
  if (phase === "failed") {
    const success = record.progress?.success ?? 0;
    return {
      label: resolveCollectProgressStatusText(record.progress),
      tone: success > 0 ? "stopping" : "failed",
    };
  }
  if (phase === "stopped") {
    return { label: resolveCollectProgressStatusText(record.progress), tone: "stopping" };
  }
  if (phase === "done") {
    const failed = record.progress?.failed ?? 0;
    return {
      label: resolveCollectProgressStatusText(record.progress),
      tone: failed > 0 ? "stopping" : "enabled",
    };
  }

  if (record.state) {
    return { label: "已启用", tone: "enabled" };
  }
  return { label: "已禁用", tone: "disabled" };
}
