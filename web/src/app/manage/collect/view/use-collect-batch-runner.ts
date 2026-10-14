import React, { useState, useCallback } from "react";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { COLLECT_BATCH_FAILED_EVENT } from "@/app/manage/components/manage-tour";
import { isActiveCollectStatus, makeStartingProgress, type BatchOption, type FilmSource } from "./types";

export function useCollectBatchRunner(
  siteList: FilmSource[],
  setSiteList: React.Dispatch<React.SetStateAction<FilmSource[]>>,
  selectedSourceIds: React.Key[],
  reloadList: () => Promise<void>,
) {
  const { message } = useAppMessage();
  const [batchOpen, setBatchOpen] = useState(false);
  const [batchIds, setBatchIds] = useState<string[]>([]);
  const [batchTime, setBatchTime] = useState(24);
  const [batchOptions, setBatchOptions] = useState<BatchOption[]>([]);

  const openBatchCollect = useCallback(async () => {
    const resp = await ApiGet("/manage/collect/options");
    if (resp.code === 0) {
      const allOptions = Array.isArray(resp.data)
        ? resp.data.map((item: BatchOption) => ({
            ...item,
            sort: siteList.find((site) => site.id === item.id)?.sort ?? 0,
            state: siteList.find((site) => site.id === item.id)?.state ?? false,
          }))
        : [];
      const enabledIds = new Set(allOptions.map((item) => item.id));
      const selectedEnabledIds = selectedSourceIds
        .map(String)
        .filter((id) => enabledIds.has(id));
      if (selectedSourceIds.length === 0) {
        message.warning("请先选择采集站");
        return;
      }
      if (selectedEnabledIds.length === 0) {
        message.warning("选中的采集站均未启用");
        return;
      }
      const readyIds = selectedEnabledIds.filter(
        (id) => siteList.find((item) => item.id === id)?.categoryReady !== false,
      );
      if (readyIds.length === 0) {
        message.warning("选中的采集站缺少分类配置");
        return;
      }
      const options = allOptions.filter((item) => readyIds.includes(item.id));
      setBatchOptions(options);
      setBatchIds(readyIds);
      setBatchOpen(true);
      return;
    }
    message.error(resp.msg || "加载批量采集列表失败");
  }, [message, selectedSourceIds, siteList]);

  const startBatchCollect = useCallback(async () => {
    if (batchIds.length === 0) {
      message.warning("请选择采集站");
      return;
    }
    const claimedIds = batchIds.filter(
      (id) => !isActiveCollectStatus(siteList.find((item) => item.id === id)?.progress?.status),
    );
    if (claimedIds.length === 0) {
      message.warning("选中采集站均已在采集中");
      setBatchOpen(false);
      return;
    }
    setBatchOpen(false);
    const idSet = new Set(claimedIds);
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id)
          ? { ...item, progress: makeStartingProgress(item.id, item.name) }
          : item,
      ),
    );
    const resp = await ApiPost("/manage/spider/start", {
      ids: batchIds,
      time: batchTime,
      batch: true,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      return;
    }
    message.error(resp.msg || "批量采集启动失败");
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id) && item.progress?.status === "starting"
          ? { ...item, progress: null }
          : item,
      ),
    );
    window.dispatchEvent(new Event(COLLECT_BATCH_FAILED_EVENT));
    await reloadList();
  }, [batchIds, batchTime, message, reloadList, setSiteList, siteList]);

  return {
    batchOpen,
    setBatchOpen,
    batchIds,
    batchTime,
    setBatchTime,
    batchOptions,
    openBatchCollect,
    startBatchCollect,
  };
}
