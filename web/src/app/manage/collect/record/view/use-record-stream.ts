import { useEffect, useRef } from "react";
import { fetchEventStream } from "@/lib/sse-client";
import type { RecordRetryEvent } from "./record-retry-state";

interface RecordStreamHandlers {
  onSnapshot: (ids: number[]) => void;
  onRecord: (event: RecordRetryEvent) => void;
}

/**
 * 只订阅当前页失败记录的重试状态。页码或记录变化时重新连接。
 */
export function useRecordStream(ids: number[], handlers: RecordStreamHandlers) {
  const handlersRef = useRef(handlers);
  handlersRef.current = handlers;
  const idsRef = useRef(ids);
  idsRef.current = ids;
  const idsKey = [...ids].sort((a, b) => a - b).join(",");

  useEffect(() => {
    if (!idsKey) {
      return;
    }
    let aborted = false;
    let abortController: AbortController | null = null;
    let retryTimer: ReturnType<typeof setTimeout> | null = null;

    async function connect() {
      if (aborted) {
        return;
      }
      abortController = new AbortController();
      const query = idsRef.current.join(",");
      try {
        await fetchEventStream(`/api/manage/collect/record/stream?ids=${query}`, {
          method: "GET",
          signal: abortController.signal,
          onMessage: (data: any, _raw: string, event?: string) => {
            if (event === "record_snapshot") {
              const raw = Array.isArray(data?.ids) ? data.ids : [];
              handlersRef.current.onSnapshot(raw.map((id: unknown) => Number(id)));
              return;
            }
            if (event === "record" && data && typeof data === "object") {
              handlersRef.current.onRecord(data as RecordRetryEvent);
            }
          },
        });
      } catch (err: unknown) {
        const status = err && typeof err === "object" && "status" in err ? err.status : undefined;
        const message = err instanceof Error ? err.message : "";
        if (status === 401 || status === 403 || /401|403/.test(message)) {
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
      if (retryTimer) {
        clearTimeout(retryTimer);
      }
      abortController?.abort();
    };
  }, [idsKey]);
}
