import type { SearchIndexProgress } from "./use-collect-stream";

export interface SealRingView {
  mode: "sealing" | "waiting";
  percent: number;
  tooltip: string;
}

/** 全局封口队列里这一站的圆环。不在队列里则不替换采集环。 */
export function sealRingForSource(
  sourceId: string,
  progress?: SearchIndexProgress | null,
): SealRingView | null {
  const site = progress?.sites?.find((item) => item.sourceId === sourceId);
  if (!site) {
    return null;
  }
  if (site.state === "sealing") {
    const percent = progress?.percent ?? 0;
    return {
      mode: "sealing",
      percent,
      tooltip: `封口中 ${percent}%`,
    };
  }
  if (site.state === "waiting") {
    return {
      mode: "waiting",
      percent: 0,
      tooltip: "等待中",
    };
  }
  return null;
}
