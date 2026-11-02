import { useEffect, useRef } from "react";
import { fetchEventStream } from "@/lib/sse-client";
import type { CollectHistoryStatus } from "./types";

export interface RecordLivePayload {
  batchId: string;
  status?: CollectHistoryStatus;
  finishedAt?: string;
  durationMs?: number;
  filmCount?: number;
  pageTotal?: number;
  pageSuccess?: number;
  pageFailed?: number;
  successSources?: number;
  failedSources?: number;
  errorMsg?: string;
  detailJson?: string;
}

export interface RecordStreamHandlers {
  onSnapshot?: (runningBatchIds: string[]) => void;
  onProgress?: (event: RecordLivePayload) => void;
  onFinished?: (event: RecordLivePayload) => void;
}

/**
 * 订阅当前页采集批次任务的实时进度与状态 SSE 长连接流
 */
export function useRecordStream(
  batchIds: string[],
  handlers: RecordStreamHandlers,
) {
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;
  const batchIdsRef = useRef(batchIds);
  batchIdsRef.current = batchIds;
  const key = [...batchIds].sort().join(",");

  useEffect(() => {
    if (!key) return;
    let aborted = false;
    let abortController: AbortController | null = null;
    let retryTimer: NodeJS.Timeout | null = null;

    async function connect() {
      if (aborted) return;
      abortController = new AbortController();
      const query = encodeURIComponent(batchIdsRef.current.join(","));
      try {
        await fetchEventStream(`/api/manage/collect/history/stream?batchIds=${query}`, {
          method: "GET",
          signal: abortController.signal,
          onMessage: (data: unknown, _raw: string, event?: string) => {
            if (event === "record_snapshot") {
              const recordObj = data && typeof data === "object" ? (data as Record<string, unknown>) : null;
              const ids = Array.isArray(recordObj?.batchIds) ? (recordObj.batchIds as string[]) : [];
              handlersRef.current.onSnapshot?.(ids);
              return;
            }
            if (event === "record_progress" && data && typeof data === "object") {
              handlersRef.current.onProgress?.(data as RecordLivePayload);
              return;
            }
            if (event === "record_finished" && data && typeof data === "object") {
              handlersRef.current.onFinished?.(data as RecordLivePayload);
              return;
            }
          },
        });
      } catch (err: unknown) {
        const status = err && typeof err === "object" && "status" in err ? err.status : undefined;
        if (status === 401 || status === 403) {
          aborted = true;
          return;
        }
      } finally {
        if (!aborted) {
          retryTimer = setTimeout(connect, 3000);
        }
      }
    }

    void connect();
    return () => {
      aborted = true;
      clearTimeout(retryTimer ?? undefined);
      abortController?.abort();
    };
  }, [key]);
}
