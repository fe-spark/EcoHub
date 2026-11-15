import { useCallback, type Dispatch, type SetStateAction } from "react";
import { sameCollectProgress } from "./collect-queue";
import type { CollectProgress, FilmSource } from "./types";

/**
 * 服务端进度快照只覆盖进度。上次采集时间、上次采集页以采集站列表为准，这里不改。
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
          return {
            ...item,
            progress: incoming,
          };
        });
        return changed ? next : current;
      });
    },
    [setSiteList],
  );
}
