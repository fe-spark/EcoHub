import { useEffect, useRef } from "react";
import { fetchEventStream } from "@/lib/sse-client";
import type { CollectProgress } from "./types";

export interface CategorySyncEvent {
  sourceId: string;
  status: "syncing" | "ready" | "failed" | string;
}

export interface CategoryReadyEvent {
  sourceId: string;
  categoryReady: boolean;
}

export interface CollectStreamHandlers {
  onProgress?: (progressList: CollectProgress[]) => void;
  onCategorySnapshot?: (snapshot: Record<string, string>) => void;
  onCategoryStatus?: (event: CategorySyncEvent) => void;
  onCategoryReady?: (event: CategoryReadyEvent) => void;
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

    async function connect() {
      if (aborted) return;
      abortController = new AbortController();
      try {
        await fetchEventStream("/api/manage/collect/progress-stream", {
          method: "GET",
          signal: abortController.signal,
          onMessage: (data: any, _raw: string, event?: string) => {
            if (
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
