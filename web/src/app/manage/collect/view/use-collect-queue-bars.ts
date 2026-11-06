import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import type { CollectQueueBarItem } from "./collect-queue-bars";
import {
  computeCollectQueueProgress,
  groupCollectQueues,
  isActiveQueueView,
  isCollectQueueBar,
  queueEndNotice,
  resolveSearchSealBar,
} from "./collect-queue";
import type { SearchIndexProgress } from "./use-collect-stream";
import type { FilmSource } from "./types";

export function useCollectQueueBars(sites: FilmSource[], searchIndex?: SearchIndexProgress) {
  const { message } = useAppMessage();
  const [stoppingQueueId, setStoppingQueueId] = useState<string | null>(null);
  const seqRef = useRef(new Map<string, number>());
  const nextSeqRef = useRef(1);

  const allBars = useMemo<CollectQueueBarItem[]>(
    () =>
      groupCollectQueues(sites).flatMap((group) => {
        const view = computeCollectQueueProgress(group.sourceIds, sites);
        if (!view) return [];
        let seq = seqRef.current.get(group.queueId);
        if (seq == null) {
          seq = nextSeqRef.current++;
          seqRef.current.set(group.queueId, seq);
        }
        return [
          {
            queueId: group.queueId,
            sourceIds: group.sourceIds,
            seq,
            view,
            canStop: view.running,
          },
        ];
      }),
    [sites],
  );
  const collectQueuesRunning = allBars.some((bar) => bar.view.running);
  const sealBar = useMemo(
    () => resolveSearchSealBar(
      sites.filter((site) => site.progress?.status === "sealing").length,
      searchIndex,
      collectQueuesRunning,
    ),
    [collectQueuesRunning, searchIndex, sites],
  );
  const queueBars = useMemo(
    () => allBars.filter((bar) => isCollectQueueBar(bar.view, sealBar != null)),
    [allBars, sealBar],
  );

  // 队列从进行中变成结束时提示一次，完成态由服务端快照继续保留展示。
  const activeRef = useRef<Set<string>>(new Set());
  const notifiedRef = useRef<Set<string>>(new Set());
  useEffect(() => {
    const active = new Set(
      allBars.filter((bar) => isActiveQueueView(bar.view)).map((bar) => bar.queueId),
    );
    for (const queueId of activeRef.current) {
      if (active.has(queueId) || notifiedRef.current.has(queueId)) continue;
      notifiedRef.current.add(queueId);
      const bar = allBars.find((item) => item.queueId === queueId);
      if (!bar) continue;
      const notice = queueEndNotice(bar.view);
      message[notice.level]({
        key: `collect-queue-notice-${queueId}`,
        content: notice.text,
        duration: 2,
      });
    }
    activeRef.current = active;
  }, [allBars, message]);

  useEffect(() => {
    if (!stoppingQueueId) return;
    const bar = queueBars.find((item) => item.queueId === stoppingQueueId);
    if (!bar || !bar.view.running) {
      setStoppingQueueId(null);
    }
  }, [queueBars, stoppingQueueId]);

  const stopQueue = useCallback(
    async (queueId: string, sourceIds: string[]) => {
      const bar = queueBars.find((item) => item.queueId === queueId);
      if (!bar?.view.running) return;
      setStoppingQueueId(queueId);
      const msgKey = `collect-queue-notice-${queueId}`;
      message.loading({ key: msgKey, content: "正在停止...", duration: 0 });
      try {
        const resp = await ApiPost("/manage/spider/stop", { ids: sourceIds });
        if (resp.code !== 0) {
          setStoppingQueueId(null);
          message.error({ key: msgKey, content: resp.msg || "终止任务失败", duration: 2 });
        }
      } catch {
        setStoppingQueueId(null);
        message.error({ key: msgKey, content: "终止任务失败", duration: 2 });
      }
    },
    [message, queueBars],
  );

  return { queueBars, sealBar, stoppingQueueId, stopQueue };
}
