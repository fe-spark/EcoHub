import { useCallback, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { mergeCollectProgress, type CollectProgress, type FilmSource } from "./types";

const TERMINAL = new Set(["done", "stopped", "failed"]);

function isShownProgress(status: string): boolean {
  return TERMINAL.has(status) || status === "starting" || status === "running";
}

/** 把采集进度 SSE 写进站点列表。终态保留 3 秒再清掉。停止中不会被清掉。 */
export function useCollectProgressStream(
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
  resetTimersRef: MutableRefObject<Map<string, NodeJS.Timeout>>,
) {
  return useCallback((progressList: CollectProgress[]) => {
    if (!Array.isArray(progressList)) return;
    const progressMap = new Map(progressList.map((p) => [String(p.id), p]));

    for (const p of progressList) {
      const id = String(p.id);
      if (TERMINAL.has(p.status)) {
        if (resetTimersRef.current.has(id)) continue;
        const timer = setTimeout(() => {
          resetTimersRef.current.delete(id);
          setSiteList((current) =>
            current.map((item) =>
              item.id === id && item.progress && TERMINAL.has(item.progress.status)
                ? { ...item, progress: null }
                : item,
            ),
          );
        }, 3000);
        resetTimersRef.current.set(id, timer);
        continue;
      }
      const existingTimer = resetTimersRef.current.get(id);
      if (existingTimer) {
        clearTimeout(existingTimer);
        resetTimersRef.current.delete(id);
      }
    }

    setSiteList((current) =>
      current.map((item) => {
        const p = progressMap.get(String(item.id));
        if (p) {
          const next = { ...item, progress: mergeCollectProgress(item.progress, p) };
          if (p.status === "done") {
            next.lastRunTime = new Date().toISOString();
          }
          return next;
        }
        if (item.progress && isShownProgress(item.progress.status)) {
          return { ...item, progress: null };
        }
        return item;
      }),
    );
  }, [resetTimersRef, setSiteList]);
}
