"use client";

import { useEffect, useRef, useState } from "react";
import { fetchEventStream } from "@/lib/sse-client";

export interface LogEntry {
  seq: number;
  line: string;
  /** info | warn | error，缺省按 info */
  level?: string;
}

export interface LogBatch {
  entries: LogEntry[];
  nextSeq: number;
  minSeq?: number;
  expired: boolean;
  /** 最近日志快照，页面应替换而不是追加 */
  reset?: boolean;
  error?: string;
}

const INITIAL_LOG_LINES = 500;
const RETRY_MS = 1500;

/** 订阅运行日志 SSE。暂停时断开；恢复后从当前游标续上。断线 1.5 秒后重连。 */
export function useSystemLogStream(
  enabled: boolean,
  onBatch: (batch: LogBatch) => void,
) {
  const onBatchRef = useRef(onBatch);
  onBatchRef.current = onBatch;
  const cursorRef = useRef(0);
  const shotRef = useRef<AbortController | null>(null);
  const restartRef = useRef<(mode: "snapshot" | "follow") => void>(() => undefined);
  const [cursor, setCursor] = useState(0);
  const [loading, setLoading] = useState(false);
  const [alive, setAlive] = useState(false);
  const [failed, setFailed] = useState(false);

  const rememberRef = useRef<(batch: LogBatch) => void>(() => undefined);
  rememberRef.current = (batch: LogBatch) => {
    if (typeof batch.nextSeq === "number" && batch.nextSeq > 0) {
      cursorRef.current = batch.nextSeq;
      setCursor(batch.nextSeq);
    }
    onBatchRef.current(batch);
  };

  useEffect(() => {
    shotRef.current?.abort();
    if (!enabled) {
      setAlive(false);
      return;
    }

    let stopped = false;
    let ctrl: AbortController | null = null;
    let timer: number | null = null;
    let mode: "snapshot" | "follow" = cursorRef.current > 0 ? "follow" : "snapshot";

    const clearTimer = () => {
      if (timer !== null) {
        window.clearTimeout(timer);
        timer = null;
      }
    };

    const run = async () => {
      if (stopped) {
        return;
      }
      const current = new AbortController();
      ctrl = current;
      const snapshot = mode === "snapshot";
      const after = snapshot ? 0 : cursorRef.current;
      mode = "follow";
      if (snapshot) {
        setLoading(true);
      }
      try {
        await fetchEventStream(
          `/api/manage/system/logs/stream?lines=${INITIAL_LOG_LINES}&after=${after}`,
          {
            method: "GET",
            signal: current.signal,
            onOpen: () => {
              if (stopped || current.signal.aborted) {
                return;
              }
              setFailed(false);
              setAlive(true);
            },
            onMessage: (data: LogBatch) => {
              if (!data || !Array.isArray(data.entries)) {
                return;
              }
              setFailed(false);
              setAlive(true);
              if (snapshot) {
                setLoading(false);
              }
              rememberRef.current(data);
            },
          },
        );
      } catch (err) {
        if (stopped || current.signal.aborted) {
          return;
        }
        const msg = err instanceof Error ? err.message : "";
        setFailed(true);
        setAlive(false);
        setLoading(false);
        if (/401|403|鉴权|权限不足/.test(msg)) {
          return;
        }
      } finally {
        if (snapshot && ctrl === current) {
          setLoading(false);
        }
      }
      if (stopped || current.signal.aborted) {
        return;
      }
      setAlive(false);
      setFailed(true);
      timer = window.setTimeout(() => void run(), RETRY_MS);
    };

    restartRef.current = (next) => {
      mode = next;
      clearTimer();
      ctrl?.abort();
      void run();
    };
    void run();

    return () => {
      stopped = true;
      clearTimer();
      ctrl?.abort();
      shotRef.current?.abort();
      restartRef.current = () => undefined;
    };
  }, [enabled]);

  const reload = () => {
    if (enabled) {
      setFailed(false);
      restartRef.current("snapshot");
      return;
    }
    shotRef.current?.abort();
    const ctrl = new AbortController();
    shotRef.current = ctrl;
    setLoading(true);
    void (async () => {
      try {
        await fetchEventStream(
          `/api/manage/system/logs/stream?lines=${INITIAL_LOG_LINES}&after=0`,
          {
            method: "GET",
            signal: ctrl.signal,
            onMessage: (data: LogBatch) => {
              if (!data || !Array.isArray(data.entries)) {
                return;
              }
              setFailed(false);
              rememberRef.current({ ...data, reset: true });
              ctrl.abort();
            },
          },
        );
      } catch {
        if (!ctrl.signal.aborted) {
          setFailed(true);
        }
      } finally {
        if (shotRef.current === ctrl) {
          shotRef.current = null;
        }
        setLoading(false);
      }
    })();
  };

  return { cursor, loading, alive, failed, reload };
}
