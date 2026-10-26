import { useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { TOUR_HOLD_PROGRESS_ATTR } from "@/app/manage/components/manage-tour";
import type { CollectQueueBarItem } from "./collect-queue-bars";
import {
  computeCollectQueueProgress,
  finishedQueueViews,
  groupCollectQueues,
  isClientCollectQueueId,
  matchPrevCollectQueueBar,
  queueEndNotice,
  type CollectQueueProgressView,
} from "./collect-queue";
import { progressAsStopping, type FilmSource } from "./types";
import type { SearchIndexProgress } from "./use-collect-stream";


interface QueueBarState {
  queueId: string;
  sourceIds: string[];
  seq: number;
  staleQueueId?: string;
  view: CollectQueueProgressView;
}

export function useCollectQueueBars(
  sites: FilmSource[],
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
  reloadList: () => Promise<void>,
  searchIndex?: SearchIndexProgress,
) {
  const { message } = useAppMessage();
  const [bars, setBars] = useState<QueueBarState[]>([]);
  const barsRef = useRef<QueueBarState[]>([]);
  const [stoppingQueueId, setStoppingQueueId] = useState<string | null>(null);
  const [tourHoldProgress, setTourHoldProgress] = useState(false);

  useEffect(() => {
    const sync = () => {
      setTourHoldProgress(document.documentElement.hasAttribute(TOUR_HOLD_PROGRESS_ATTR));
    };
    sync();
    const observer = new MutationObserver(sync);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: [TOUR_HOLD_PROGRESS_ATTR],
    });
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    const prev = barsRef.current;
    const groups = groupCollectQueues(sites);
    const used = new Set<string>();
    let seqCursor = prev.reduce((max, bar) => Math.max(max, bar.seq), 0);
    const next: QueueBarState[] = [];
    for (const group of groups) {
      const view = computeCollectQueueProgress(group.sourceIds, sites, searchIndex);
      if (!view) {
        continue;
      }
      const matched = matchPrevCollectQueueBar(group, prev, used);
      if (matched) {
        used.add(matched.queueId);
      }
      const seq = matched?.seq ?? ++seqCursor;
      const staleQueueId = isClientCollectQueueId(group.queueId)
        ? sites.find((site) => group.sourceIds.includes(site.id))?.progress?.staleQueueId
        : undefined;
      next.push({
        queueId: group.queueId,
        sourceIds: group.sourceIds,
        seq,
        staleQueueId,
        view,
      });
    }
    let visible = next;
    if (tourHoldProgress) {
      for (const bar of prev) {
        if (!next.some((item) => item.seq === bar.seq)) {
          visible.push(bar);
        }
      }
    } else {
      for (const view of finishedQueueViews(prev, next)) {
        const notice = queueEndNotice(view);
        const msgKey = view.queueId ? `collect-stop-queue-${view.queueId}` : undefined;
        if (msgKey) {
          message[notice.level]({
            key: msgKey,
            content: notice.text,
            duration: 2,
          });
        } else {
          message[notice.level](notice.text);
        }
      }
      visible = next.filter((item) => item.view.running);
    }
    barsRef.current = visible;
    setBars(visible);
  }, [sites, tourHoldProgress, message, searchIndex]);

  const stopQueue = async (queueId: string, sourceIds: string[]) => {
    setStoppingQueueId(queueId);
    const msgKey = `collect-stop-queue-${queueId}`;
    message.loading({
      key: msgKey,
      content: "正在停止...",
      duration: 0,
    });
    const idSet = new Set(sourceIds);
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id)
          ? { ...item, progress: progressAsStopping(item.progress, item.id, item.name) }
          : item,
      ),
    );
    try {
      const resp = await ApiPost("/manage/spider/stop", { ids: sourceIds });
      if (resp.code !== 0) {
        message.error({
          key: msgKey,
          content: resp.msg || "终止任务失败",
          duration: 2,
        });
        void reloadList();
        return;
      }
    } catch {
      message.error({
        key: msgKey,
        content: "终止任务失败",
        duration: 2,
      });
      void reloadList();
    } finally {
      setStoppingQueueId(null);
    }
  };

  const items = useMemo<CollectQueueBarItem[]>(
    () =>
      bars.map((bar) => ({
        queueId: bar.queueId,
        sourceIds: bar.sourceIds,
        seq: bar.seq,
        view: bar.view,
        canStop: bar.view.fetchingCount > 0,
      })),
    [bars],
  );

  return { queueBars: items, stoppingQueueId, stopQueue };
}
