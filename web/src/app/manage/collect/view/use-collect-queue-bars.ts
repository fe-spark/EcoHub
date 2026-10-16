import { useEffect, useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { TOUR_HOLD_PROGRESS_ATTR } from "@/app/manage/components/manage-tour";
import type { CollectQueueBarItem } from "./collect-queue-bars";
import {
  computeCollectQueueProgress,
  groupCollectQueues,
  matchPrevCollectQueueBar,
  type CollectQueueProgressView,
} from "./collect-queue";
import { progressAsStopping, type FilmSource } from "./types";

const CLOSE_SECONDS = 8;

interface QueueBarState {
  queueId: string;
  sourceIds: string[];
  seq: number;
  view: CollectQueueProgressView;
  countdown: number | null;
}

function finishedView(view: CollectQueueProgressView): CollectQueueProgressView {
  return { ...view, running: false, percent: 100, activeCount: 0, fetchingCount: 0 };
}

export function useCollectQueueBars(
  sites: FilmSource[],
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
  reloadList: () => Promise<void>,
) {
  const { message } = useAppMessage();
  const [bars, setBars] = useState<QueueBarState[]>([]);
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
    const groups = groupCollectQueues(sites);
    setBars((prev) => {
      const used = new Set<string>();
      let seqCursor = prev.reduce((max, bar) => Math.max(max, bar.seq), 0);
      const next: QueueBarState[] = [];
      for (const group of groups) {
        const view = computeCollectQueueProgress(group.sourceIds, sites, []);
        if (!view) {
          continue;
        }
        const matched = matchPrevCollectQueueBar(group, prev, used);
        if (matched) {
          used.add(matched.queueId);
        }
        const seq = matched?.seq ?? ++seqCursor;
        next.push({
          queueId: group.queueId,
          sourceIds: group.sourceIds,
          seq,
          view,
          countdown: view.running ? null : matched?.countdown ?? CLOSE_SECONDS,
        });
      }
      for (const bar of prev) {
        if (next.some((item) => item.seq === bar.seq)) {
          continue;
        }
        if (bar.countdown === 0) {
          continue;
        }
        next.push({
          ...bar,
          view: bar.view.running ? finishedView(bar.view) : bar.view,
          countdown: bar.countdown ?? CLOSE_SECONDS,
        });
      }
      return next;
    });
  }, [sites]);

  const countdownActive = bars.some((bar) => !bar.view.running && bar.countdown != null);
  useEffect(() => {
    if (tourHoldProgress || !countdownActive) {
      return;
    }
    const timer = window.setInterval(() => {
      setBars((current) =>
        current.flatMap((bar) => {
          if (bar.view.running || bar.countdown == null) {
            return [bar];
          }
          const countdown = bar.countdown - 1;
          if (countdown <= 0) {
            return [];
          }
          return [{ ...bar, countdown }];
        }),
      );
    }, 1000);
    return () => window.clearInterval(timer);
  }, [countdownActive, tourHoldProgress]);

  const stopQueue = async (queueId: string, sourceIds: string[]) => {
    setStoppingQueueId(queueId);
    const idSet = new Set(sourceIds);
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id)
          ? { ...item, progress: progressAsStopping(item.progress, item.id, item.name) }
          : item,
      ),
    );
    try {
      const results = await Promise.all(
        sourceIds.map((id) => ApiPost("/manage/spider/stop", { id })),
      );
      if (results.every((item) => item.code === 0)) {
        message.success("正在停止");
        return;
      }
      message.error(results.find((item) => item.code !== 0)?.msg || "终止任务失败");
      void reloadList();
    } catch {
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
        countdown: bar.view.running || tourHoldProgress ? null : bar.countdown,
        canStop: bar.view.fetchingCount > 0,
      })),
    [bars, tourHoldProgress],
  );

  return { queueBars: items, stoppingQueueId, tourHoldProgress, stopQueue };
}
