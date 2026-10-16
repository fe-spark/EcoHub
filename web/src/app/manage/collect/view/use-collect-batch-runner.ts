import React, { useCallback, useMemo, useState } from "react";
import { ApiPost, type ApiResponse } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { COLLECT_BATCH_FAILED_EVENT } from "@/app/manage/components/manage-tour";
import { collectBlockReason, formatSkipSummary } from "./collect-selection";
import { newClientCollectQueueId } from "./collect-queue";
import type { BatchCollectRow } from "./batch-collect-modal";
import { makeStartingProgress, type CollectBatchResult, type FilmSource } from "./types";

export function useCollectBatchRunner(
  siteList: FilmSource[],
  setSiteList: React.Dispatch<React.SetStateAction<FilmSource[]>>,
  selectedSourceIds: React.Key[],
  reloadList: () => Promise<void>,
) {
  const { message } = useAppMessage();
  const [batchOpen, setBatchOpen] = useState(false);
  const [batchScopeIds, setBatchScopeIds] = useState<string[]>([]);
  const [batchTime, setBatchTime] = useState(24);

  const batchRows = useMemo<BatchCollectRow[]>(
    () =>
      batchScopeIds.flatMap((id) => {
        const site = siteList.find((item) => item.id === id);
        if (!site) return [];
        return [{ id: site.id, name: site.name, reason: collectBlockReason(site) }];
      }),
    [batchScopeIds, siteList],
  );

  const openBatchCollect = useCallback(() => {
    const selected = siteList.filter((item) => selectedSourceIds.includes(item.id));
    if (selected.length === 0) {
      message.warning("请先选择采集站");
      return;
    }
    const blocked = selected
      .map((item) => ({ name: item.name, reason: collectBlockReason(item) }))
      .filter((item) => item.reason);
    if (blocked.length === selected.length) {
      message.warning(`没有可采集的站点：${formatSkipSummary(blocked)}`);
      return;
    }
    setBatchScopeIds(selected.map((item) => item.id));
    setBatchOpen(true);
  }, [message, selectedSourceIds, siteList]);

  const clearStarting = useCallback(
    (ids: Set<string>) => {
      if (ids.size === 0) return;
      setSiteList((current) =>
        current.map((item) =>
          ids.has(item.id) && item.progress?.status === "starting" ? { ...item, progress: null } : item,
        ),
      );
    },
    [setSiteList],
  );

  const startBatchCollect = useCallback(async () => {
    const ids = batchRows.filter((item) => !item.reason).map((item) => item.id);
    if (ids.length === 0) {
      message.warning("没有可采集的站点");
      return;
    }
    setBatchOpen(false);
    const idSet = new Set(ids);
    const queueId = newClientCollectQueueId();
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id) ? { ...item, progress: makeStartingProgress(item.id, item.name, queueId) } : item,
      ),
    );
    let resp: ApiResponse<CollectBatchResult>;
    try {
      resp = await ApiPost<CollectBatchResult>("/manage/spider/start", {
        ids,
        time: batchTime,
        batch: true,
      });
    } catch {
      clearStarting(idSet);
      window.dispatchEvent(new Event(COLLECT_BATCH_FAILED_EVENT));
      return;
    }
    const started = new Set(
      (Array.isArray(resp.data?.started) ? resp.data.started : []).map((item) => item.id),
    );
    const skipped = Array.isArray(resp.data?.skipped) ? resp.data.skipped : [];
    const skippedIds = new Set(skipped.map((item) => item.id));
    if (resp.code === 0) {
      message.success(resp.msg || `已开启 ${started.size} 个`);
      if (skipped.length > 0) {
        message.warning(`跳过：${formatSkipSummary(skipped.map((item) => ({ name: item.name || item.id, reason: item.reason })))}`);
        clearStarting(skippedIds);
      }
      return;
    }
    message.warning(
      skipped.length > 0
        ? `没有可采集的站点：${formatSkipSummary(skipped.map((item) => ({ name: item.name || item.id, reason: item.reason })))}`
        : resp.msg || "批量采集启动失败",
    );
    clearStarting(new Set(ids.filter((id) => !started.has(id))));
    window.dispatchEvent(new Event(COLLECT_BATCH_FAILED_EVENT));
    await reloadList();
  }, [batchRows, batchTime, clearStarting, message, reloadList, setSiteList]);

  return {
    batchOpen,
    setBatchOpen,
    batchTime,
    setBatchTime,
    batchRows,
    openBatchCollect,
    startBatchCollect,
  };
}
