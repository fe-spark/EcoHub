import {
  resolveCollectProgressStatusText,
  type FilmSource,
} from "./types";

/** 卡片色调。采集中出现失败页时单独用 runningFailed，和正常采集中区分。 */
export type StatusTone =
  | "running"
  | "runningFailed"
  | "paused"
  | "sealing"
  | "stopped"
  | "failed"
  | "enabled"
  | "disabled"
  | "deleting";

export function resolveSourceStatus(
  record: FilmSource,
  active: boolean,
  deleting = false,
): { label: string; tone: StatusTone } {
  if (deleting || record.deleting) {
    return { label: "删除中", tone: "deleting" };
  }

  const phase = record.progress?.status;

  if (phase === "sealing") {
    return { label: "待检索", tone: "sealing" };
  }

  if (phase === "paused") {
    return { label: resolveCollectProgressStatusText(record.progress), tone: "paused" };
  }

  if (active) {
    if (!record.state) {
      return { label: "已终止·等待完成", tone: "stopped" };
    }
    // 排队、抓页、停止中统一显示为采集中。有失败页时换红色，圆点仍表示进行中。
    const failed = record.progress?.failed ?? 0;
    return { label: "采集中", tone: failed > 0 ? "runningFailed" : "running" };
  }

  // 非活跃态：残留进度反映真实终态
  if (phase === "failed") {
    return {
      label: resolveCollectProgressStatusText(record.progress),
      tone: "failed",
    };
  }
  if (phase === "stopped") {
    return { label: "已停止", tone: "stopped" };
  }
  if (phase === "done") {
    const failed = record.progress?.failed ?? 0;
    return {
      label: resolveCollectProgressStatusText(record.progress),
      tone: failed > 0 ? "stopped" : "enabled",
    };
  }

  if (record.state) {
    return { label: "已启用", tone: "enabled" };
  }
  return { label: "已禁用", tone: "disabled" };
}
