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
  isTerminalCollectStatus,
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
  const notifiedQueueIdsRef = useRef<Set<string>>(new Set());
  const stoppingQueuesRef = useRef<Map<string, string[]>>(new Map());
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
      for (const item of next) {
        if (item.view.stoppingCount > 0 && !stoppingQueuesRef.current.has(item.queueId)) {
          stoppingQueuesRef.current.set(item.queueId, item.sourceIds);
        }
      }
      // 检查正在手动终止的队列：必须该队列下的所有站点均达到终态，才触发「采集已终止」
      if (stoppingQueuesRef.current.size > 0) {
        for (const [qId, sIds] of Array.from(stoppingQueuesRef.current.entries())) {
          const allStopped = sIds.every((id) => {
            const site = sites.find((s) => s.id === id);
            if (!site || !site.progress) return true;
            return isTerminalCollectStatus(site.progress.status);
          });
          if (allStopped) {
            stoppingQueuesRef.current.delete(qId);
            notifiedQueueIdsRef.current.add(qId);
            message.info({
              key: `collect-queue-notice-${qId}`,
              content: "采集已终止",
              duration: 2,
            });
            setStoppingQueueId((current) => (current === qId ? null : current));
            const stopIdSet = new Set(sIds);
            setSiteList((current) =>
              current.map((item) =>
                stopIdSet.has(item.id)
                  ? { ...item, progress: null }
                  : item,
              ),
            );
          }
        }
      }

      for (const item of next) {
        if (item.view.running) {
          notifiedQueueIdsRef.current.delete(item.queueId);
        }
      }
      for (const view of finishedQueueViews(prev, next)) {
        const qId = view.queueId;
        // 如果该队列已被 stopQueue 记录拦截或已通知过，不再重复触发
        if (qId && (notifiedQueueIdsRef.current.has(qId) || stoppingQueuesRef.current.has(qId))) {
          continue;
        }
        if (qId) {
          notifiedQueueIdsRef.current.add(qId);
        }
        const notice = queueEndNotice(view);
        const msgKey = qId ? `collect-queue-notice-${qId}` : undefined;
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
  }, [sites, tourHoldProgress, message, searchIndex, setSiteList]);

  const stopQueue = async (queueId: string, sourceIds: string[]) => {
    const runningIds = sourceIds.filter((id) => {
      const site = sites.find((s) => s.id === id);
      return site?.progress && !isTerminalCollectStatus(site.progress.status);
    });
    const msgKey = queueId ? `collect-queue-notice-${queueId}` : undefined;

    if (runningIds.length === 0) {
      if (msgKey) {
        message.info({
          key: msgKey,
          content: "采集已终止",
          duration: 2,
        });
      }
      setStoppingQueueId(null);
      return;
    }

    setStoppingQueueId(queueId);
    if (queueId) {
      stoppingQueuesRef.current.set(queueId, sourceIds);
    }
    if (msgKey) {
      message.loading({
        key: msgKey,
        content: "正在停止...",
        duration: 0,
      });
    }
    const runningIdSet = new Set(runningIds);
    setSiteList((current) =>
      current.map((item) =>
        runningIdSet.has(item.id)
          ? { ...item, progress: progressAsStopping(item.progress, item.id, item.name) }
          : item,
      ),
    );
    try {
      const resp = await ApiPost("/manage/spider/stop", { ids: sourceIds });
      if (resp.code !== 0) {
        if (queueId) {
          stoppingQueuesRef.current.delete(queueId);
        }
        setStoppingQueueId((current) => (current === queueId ? null : current));
        if (msgKey) {
          message.error({
            key: msgKey,
            content: resp.msg || "终止任务失败",
            duration: 2,
          });
        } else {
          message.error(resp.msg || "终止任务失败");
        }
        void reloadList();
        return;
      }
      void reloadList();
    } catch {
      if (queueId) {
        stoppingQueuesRef.current.delete(queueId);
      }
      setStoppingQueueId((current) => (current === queueId ? null : current));
      if (msgKey) {
        message.error({
          key: msgKey,
          content: "终止任务失败",
          duration: 2,
        });
      } else {
        message.error("终止任务失败");
      }
      void reloadList();
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
