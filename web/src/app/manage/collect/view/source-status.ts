import {
  resolveCollectProgressStatusText,
  type FilmSource,
} from "./types";

/** 卡片只保留 4 类采集色调：进行中、待检索、已停止、失败。 */
export type StatusTone =
  | "running"
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

  if (active) {
    if (!record.state) {
      return { label: "已终止·等待完成", tone: "stopped" };
    }
    // 排队、抓页、停止中统一显示为采集中，减少状态文案。
    return { label: "采集中", tone: "running" };
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
