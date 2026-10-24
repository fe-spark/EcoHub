import { useCallback, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { isTerminalCollectStatus, nextCollectProgress } from "./collect-queue";
import type { CollectProgress, FilmSource } from "./types";

/** 把采集进度 SSE 写进站点列表。终态保留 3 秒再清掉。停止中不会被清掉。 */
export function useCollectProgressStream(
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
  resetTimersRef: MutableRefObject<Map<string, NodeJS.Timeout>>,
  onTerminalStatus?: (id: string, status: string) => void,
) {
  return useCallback((progressList: CollectProgress[]) => {
    if (!Array.isArray(progressList)) return;
    const progressMap = new Map(progressList.map((p) => [String(p.id), p]));

    for (const p of progressList) {
      const id = String(p.id);
      if (isTerminalCollectStatus(p.status)) {
        onTerminalStatus?.(id, p.status);
        if (resetTimersRef.current.has(id)) continue;
        const timer = setTimeout(() => {
          resetTimersRef.current.delete(id);
          setSiteList((current) =>
            current.map((item) =>
              item.id === id && item.progress && isTerminalCollectStatus(item.progress.status)
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
        const incoming = progressMap.get(String(item.id));
        const progress = nextCollectProgress(item.progress, incoming);
        if (progress === item.progress) {
          return item;
        }
        const isTerminal = isTerminalCollectStatus(progress?.status);
        const isDone = progress?.status === "done";
        const hasFinishedRun = isTerminal && (progress?.success ?? 0) > 0;
        return {
          ...item,
          progress,
          lastRunTime:
            isDone || hasFinishedRun ? new Date().toISOString() : item.lastRunTime,
          lastRunPageCount:
            hasFinishedRun ? progress.success : item.lastRunPageCount,
        };
      }),
    );
  }, [onTerminalStatus, resetTimersRef, setSiteList]);
}
