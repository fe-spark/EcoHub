import type { FilmSource } from "./types";

export type StatusTone = "running" | "enabled" | "disabled" | "stopping";

export function resolveSourceStatus(
  record: FilmSource,
  active: boolean,
): { label: string; tone: StatusTone } {
  const phase = record.progress?.status;

  if (active) {
    if (!record.state) {
      return { label: "已终止·等待完成", tone: "stopping" };
    }
    if (phase === "starting") {
      return { label: "排队中", tone: "running" };
    }
    if (phase === "page_done" || phase === "waiting_publish") {
      return { label: "等待收尾", tone: "running" };
    }
    if (phase === "finalizing") {
      return { label: "收尾发布中", tone: "running" };
    }
    return { label: "采集中", tone: "running" };
  }

  if (record.state) {
    return { label: "已启用", tone: "enabled" };
  }
  return { label: "已禁用", tone: "disabled" };
}
