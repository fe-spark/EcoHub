import { useCallback, type Dispatch, type SetStateAction } from "react";
import { sameCollectProgress } from "./collect-queue";
import type { CollectProgress, FilmSource } from "./types";

/**
 * 服务端进度快照是采集状态的唯一数据源。
 * 进行中的成功页直接显示为上次采集页；本轮完成时把这次的页数和时间留下。
 */
export function useCollectProgressStream(
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
) {
  return useCallback(
    (snapshot: CollectProgress[]) => {
      if (!Array.isArray(snapshot)) return;
      const byId = new Map(snapshot.map((p) => [String(p.id), p]));
      setSiteList((current) => {
        let changed = false;
        const next = current.map((item) => {
          const incoming = byId.get(item.id) ?? null;
          if (sameCollectProgress(item.progress, incoming)) {
            return item;
          }
          changed = true;
          const finished = incoming?.status === "done";
          return {
            ...item,
            progress: incoming,
            lastRunTime: finished ? new Date().toISOString() : item.lastRunTime,
            lastRunPageCount:
              finished && (incoming?.success ?? 0) > 0
                ? incoming.success
                : item.lastRunPageCount,
          };
        });
        return changed ? next : current;
      });
    },
    [setSiteList],
  );
}
