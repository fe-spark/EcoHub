import React, { useCallback, useMemo, useState } from "react";
import { ApiPost, type ApiResponse } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { COLLECT_BATCH_FAILED_EVENT } from "@/app/manage/components/manage-tour";
import { collectBlockReason, collectIndexUpdatingReason, formatSkipSummary } from "./collect-selection";
import type { BatchCollectRow } from "./batch-collect-drawer";
import { type CollectBatchResult, type FilmSource } from "./types";
import type { SearchIndexProgress } from "./use-collect-stream";

export function useCollectBatchRunner(
  siteList: FilmSource[],
  selectedSourceIds: React.Key[],
  clearSelection?: () => void,
  searchIndex?: SearchIndexProgress,
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
        return [{ id: site.id, name: site.name, reason: collectBlockReason(site, searchIndex) }];
      }),
    [batchScopeIds, searchIndex, siteList],
  );

  const openBatchCollect = useCallback(() => {
    if (searchIndex?.status === "indexing") {
      message.warning(collectIndexUpdatingReason);
      return;
    }
    const selected = siteList.filter((item) => selectedSourceIds.includes(item.id));
    if (selected.length === 0) {
      message.warning("请先选择采集站");
      return;
    }
    const blocked = selected
      .map((item) => ({ name: item.name, reason: collectBlockReason(item, searchIndex) }))
      .filter((item) => item.reason);
    if (blocked.length === selected.length) {
      message.warning(`没有可采集的站点：${formatSkipSummary(blocked)}`);
      return;
    }
    setBatchScopeIds(selected.map((item) => item.id));
    setBatchOpen(true);
  }, [message, searchIndex, selectedSourceIds, siteList]);

  const startBatchCollect = useCallback(async () => {
    if (searchIndex?.status === "indexing") {
      message.warning(collectIndexUpdatingReason);
      return;
    }
    const ids = batchRows.filter((item) => !item.reason).map((item) => item.id);
    if (ids.length === 0) {
      message.warning("没有可采集的站点");
      return;
    }
    setBatchOpen(false);
    clearSelection?.();
    let resp: ApiResponse<CollectBatchResult>;
    try {
      resp = await ApiPost<CollectBatchResult>("/manage/spider/start", {
        ids,
        time: batchTime,
      });
    } catch {
      window.dispatchEvent(new Event(COLLECT_BATCH_FAILED_EVENT));
      return;
    }
    const started = new Set(
      (Array.isArray(resp.data?.started) ? resp.data.started : []).map((item) => item.id),
    );
    const skipped = Array.isArray(resp.data?.skipped) ? resp.data.skipped : [];
    if (resp.code === 0) {
      message.success(resp.msg || `已开启 ${started.size} 个`);
      if (skipped.length > 0) {
        message.warning(
          `跳过：${formatSkipSummary(
            skipped.map((item) => ({ name: item.name || item.id, reason: item.reason })),
          )}`,
        );
      }
      return;
    }
    message.warning(
      skipped.length > 0
        ? `没有可采集的站点：${formatSkipSummary(
            skipped.map((item) => ({ name: item.name || item.id, reason: item.reason })),
          )}`
        : resp.msg || "批量采集启动失败",
    );
    window.dispatchEvent(new Event(COLLECT_BATCH_FAILED_EVENT));
  }, [batchRows, batchTime, clearSelection, message, searchIndex]);

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
