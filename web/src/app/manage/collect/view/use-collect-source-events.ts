import { useCallback, useRef } from "react";
import type { MessageInstance } from "antd/es/message/interface";
import type { FilmSource } from "./types";
import type {
  CategorySyncEvent,
  SourceDeleteEvent,
} from "./use-collect-stream";

interface UseCollectSourceEventsParams {
  setSiteList: React.Dispatch<React.SetStateAction<FilmSource[]>>;
  message: MessageInstance;
}

/**
 * 采集中心采集源实时状态事件 Hook
 * 负责分类快照、分类同步状态、删除快照与异步删除状态的消费与同步
 */
export function useCollectSourceEvents({
  setSiteList,
  message,
}: UseCollectSourceEventsParams) {
  const pendingCategoryStatusRef = useRef<Map<string, string>>(new Map());
  const syncingSourcesRef = useRef<Set<string>>(new Set());

  const handleCategorySnapshot = useCallback(
    (snapshot: Record<string, string>) => {
      if (!snapshot || typeof snapshot !== "object") return;
      setSiteList((current) =>
        current.map((item) => {
          const status = snapshot[item.id];
          if (!status) return item;
          return {
            ...item,
            categorySyncing: status === "syncing",
            categoryReady:
              status === "ready"
                ? true
                : status === "failed"
                  ? false
                  : item.categoryReady,
          };
        }),
      );
    },
    [setSiteList],
  );

  const handleCategoryStatus = useCallback(
    (evt: CategorySyncEvent) => {
      if (!evt || !evt.sourceId) return;
      pendingCategoryStatusRef.current.set(evt.sourceId, evt.status);

      if (evt.status === "syncing") {
        syncingSourcesRef.current.add(evt.sourceId);
      } else if (evt.status === "confirm") {
        syncingSourcesRef.current.delete(evt.sourceId);
      } else if (
        evt.status === "ready" ||
        evt.status === "failed" ||
        evt.status === "idle"
      ) {
        if (syncingSourcesRef.current.has(evt.sourceId)) {
          syncingSourcesRef.current.delete(evt.sourceId);
          if (evt.status === "ready") {
            message.success({
              key: `category-sync-${evt.sourceId}`,
              content: evt.message || "分类获取成功",
            });
          } else if (evt.status === "idle") {
            message.warning({
              key: `category-sync-${evt.sourceId}`,
              content: "采集中，已跳过获取分类",
            });
          } else {
            message.error({
              key: `category-sync-${evt.sourceId}`,
              content: "分类获取失败",
            });
          }
        }
      }

      setSiteList((current) =>
        current.map((item) =>
          item.id === evt.sourceId
            ? {
                ...item,
                categorySyncing: evt.status === "syncing",
                categoryReady:
                  evt.status === "ready"
                    ? true
                    : evt.status === "failed"
                      ? false
                      : item.categoryReady,
              }
            : item,
        ),
      );
    },
    [message, setSiteList],
  );

  const handleSourceDelete = useCallback(
    (evt: SourceDeleteEvent) => {
      if (evt.status === "deleting") {
        setSiteList((prev) =>
          prev.map((s) => (s.id === evt.sourceId ? { ...s, deleting: true } : s)),
        );
      } else if (evt.status === "deleted") {
        setSiteList((prev) => prev.filter((s) => s.id !== evt.sourceId));
        message.success("删除成功");
      } else if (evt.status === "failed") {
        setSiteList((prev) =>
          prev.map((s) => (s.id === evt.sourceId ? { ...s, deleting: false } : s)),
        );
        message.error("删除失败");
      }
    },
    [message, setSiteList],
  );

  const handleDeleteSnapshot = useCallback(
    (ids: string[]) => {
      const idSet = new Set(ids);
      setSiteList((prev) =>
        prev.map((s) => (idSet.has(s.id) ? { ...s, deleting: true } : s)),
      );
    },
    [setSiteList],
  );

  return {
    pendingCategoryStatusRef,
    syncingSourcesRef,
    handleCategorySnapshot,
    handleCategoryStatus,
    handleSourceDelete,
    handleDeleteSnapshot,
  };
}
