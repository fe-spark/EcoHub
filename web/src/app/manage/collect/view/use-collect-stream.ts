import { useEffect, useRef } from "react";
import { fetchEventStream } from "@/lib/sse-client";
import type { CollectProgress } from "./types";

/**
 * 采集中心 SSE 进度流订阅 Hook
 * 监听服务端广播的采集进度变更，替代高频轮询
 */
export function useCollectStream(
  onProgress: (progressList: CollectProgress[]) => void,
) {
  const onProgressRef = useRef(onProgress);
  onProgressRef.current = onProgress;

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
          onMessage: (data: any) => {
            if (Array.isArray(data)) {
              onProgressRef.current(data);
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
