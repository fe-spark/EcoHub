import { useEffect, useRef } from "react";
import { fetchEventStream } from "@/lib/sse-client";
import type { CollectProgress } from "./types";

export interface CategoryTreeChange {
  typeId: number;
  kind: "renamed" | "removed" | "moved" | string;
  name?: string;
  from?: string;
  to?: string;
}

export interface CategorySyncEvent {
  sourceId: string;
  status: "syncing" | "ready" | "failed" | "confirm" | "idle" | string;
  name?: string;
  changes?: CategoryTreeChange[];
  message?: string;
  dataCleared?: boolean;
}

export interface CategoryReadyEvent {
  sourceId: string;
  categoryReady: boolean;
}

export interface SourceDeleteEvent {
  sourceId: string;
  status: "deleting" | "deleted" | "failed" | string;
  message?: string;
}

export interface CollectStreamHandlers {
  onProgress?: (progressList: CollectProgress[]) => void;
  onCategorySnapshot?: (snapshot: Record<string, string>) => void;
  onCategoryStatus?: (event: CategorySyncEvent) => void;
  onCategoryReady?: (event: CategoryReadyEvent) => void;
  onSourceDelete?: (event: SourceDeleteEvent) => void;
  onDeleteSnapshot?: (deletingIds: string[]) => void;
  onReconnect?: () => void;
}

/**
 * 采集中心 SSE 进度与分类状态流订阅 Hook
 * 监听服务端广播的采集进度变更及采集站分类补拉就绪状态
 */
export function useCollectStream(
  handlers: CollectStreamHandlers | ((progressList: CollectProgress[]) => void),
) {
  const handlersRef = useRef<CollectStreamHandlers>({});
  handlersRef.current =
    typeof handlers === "function" ? { onProgress: handlers } : handlers;

  useEffect(() => {
    let aborted = false;
    let abortController: AbortController | null = null;
    let retryTimer: NodeJS.Timeout | null = null;
    let hasConnectedOnce = false;

    async function connect() {
      if (aborted) return;
      abortController = new AbortController();
      try {
        await fetchEventStream("/api/manage/collect/progress-stream", {
          method: "GET",
          signal: abortController.signal,
          onOpen: () => {
            if (hasConnectedOnce) {
              handlersRef.current.onReconnect?.();
            }
            hasConnectedOnce = true;
          },
          onMessage: (data: any, _raw: string, event?: string) => {
            if (
              event === "delete_snapshot" ||
              (Array.isArray(data) && data.length > 0 && typeof data[0] === "string")
            ) {
              handlersRef.current.onDeleteSnapshot?.(data as string[]);
            } else if (
              event === "source_delete" ||
              (data && typeof data === "object" && "sourceId" in data && ("status" in data && (data.status === "deleting" || data.status === "deleted" || data.status === "failed")))
            ) {
              handlersRef.current.onSourceDelete?.(data as SourceDeleteEvent);
            } else if (
              event === "category_snapshot" ||
              (!Array.isArray(data) && data && typeof data === "object" && !data.sourceId)
            ) {
              handlersRef.current.onCategorySnapshot?.(data);
            } else if (
              event === "category_status" ||
              (data && typeof data === "object" && "sourceId" in data && "status" in data)
            ) {
              handlersRef.current.onCategoryStatus?.(data as CategorySyncEvent);
            } else if (
              event === "category_ready" ||
              (data && typeof data === "object" && "sourceId" in data && "categoryReady" in data)
            ) {
              handlersRef.current.onCategoryReady?.(data as CategoryReadyEvent);
            } else if (event === "progress" || Array.isArray(data)) {
              handlersRef.current.onProgress?.(data);
            }
          },
        });
      } catch {
        // 网络异常由重连逻辑处理
      } finally {
        if (!aborted) {
          retryTimer = setTimeout(connect, 3000);
        }
      }
    }

    void connect();

    return () => {
      aborted = true;
      if (retryTimer) {
        clearTimeout(retryTimer);
      }
      abortController?.abort();
    };
  }, []);
}
