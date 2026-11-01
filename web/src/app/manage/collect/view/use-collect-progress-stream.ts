import { useCallback, type Dispatch, type MutableRefObject, type SetStateAction } from "react";
import { isTerminalCollectStatus, nextCollectProgress } from "./collect-queue";
import type { CollectProgress, FilmSource } from "./types";
import type { SearchIndexProgress } from "./use-collect-stream";

/** 把采集进度 SSE 写进站点列表。终态保留 3 秒再清掉。停止中不会被清掉。 */
export function useCollectProgressStream(
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
  resetTimersRef: MutableRefObject<Map<string, NodeJS.Timeout>>,
  onTerminalStatus?: (id: string, status: string) => void,
  searchIndexRef?: MutableRefObject<SearchIndexProgress | undefined>,
) {
  return useCallback((progressList: CollectProgress[]) => {
    if (!Array.isArray(progressList)) return;
    const progressMap = new Map(progressList.map((p) => [String(p.id), p]));

    const armClear = (id: string, queueId?: string, retryCount = 0) => {
      const timer = setTimeout(() => {
        // 最多延迟保持 20 次（约 60 秒），防止后端索引重建意外失联导致定时器无限自旋
        if (queueId && searchIndexRef?.current?.status === "indexing" && retryCount < 20) {
          armClear(id, queueId, retryCount + 1);
          return;
        }
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
    };

    for (const p of progressList) {
      const id = String(p.id);
      if (isTerminalCollectStatus(p.status)) {
        onTerminalStatus?.(id, p.status);
        if (resetTimersRef.current.has(id)) continue;
        armClear(id, p.queueId);
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
  }, [onTerminalStatus, resetTimersRef, searchIndexRef, setSiteList]);
}
