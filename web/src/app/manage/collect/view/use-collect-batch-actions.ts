import React, { useCallback, useEffect, useMemo, useState } from "react";
import { ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import {
  formatSkipSummary,
  isSourceSelectable,
} from "./collect-selection";
import type { DelBatchResult, FilmSource } from "./types";

export function useCollectBatchActions(
  siteList: FilmSource[],
  setSiteList: React.Dispatch<React.SetStateAction<FilmSource[]>>,
  reloadList: () => Promise<void>,
) {
  const { message } = useAppMessage();
  const [selectedSourceIds, setSelectedSourceIds] = useState<React.Key[]>([]);
  const [batchStateUpdating, setBatchStateUpdating] = useState(false);
  const [batchDeleting, setBatchDeleting] = useState(false);

  useEffect(() => {
    setSelectedSourceIds((current) => {
      const next = current.filter((id) => {
        const site = siteList.find((item) => item.id === String(id));
        return Boolean(site && isSourceSelectable(site));
      });
      return next.length === current.length ? current : next;
    });
  }, [siteList]);

  const selectedSources = useMemo(
    () => siteList.filter((item) => selectedSourceIds.includes(item.id)),
    [selectedSourceIds, siteList],
  );

  const handleSelectSource = useCallback((id: string, checked: boolean) => {
    setSelectedSourceIds((current) =>
      checked ? [...current, id] : current.filter((item) => item !== id),
    );
  }, []);

  const selectAllSources = useCallback(() => {
    setSelectedSourceIds(siteList.filter(isSourceSelectable).map((item) => item.id));
  }, [siteList]);

  const invertSelection = useCallback(() => {
    setSelectedSourceIds((current) =>
      siteList
        .filter((item) => isSourceSelectable(item) && !current.includes(item.id))
        .map((item) => item.id),
    );
  }, [siteList]);

  const clearSelection = useCallback(() => {
    setSelectedSourceIds([]);
  }, []);

  const batchChangeSourceState = useCallback(
    async (state: boolean) => {
      if (selectedSources.length === 0) {
        message.warning("请先选择采集站");
        return;
      }
      const sourceIdsToUpdate = selectedSources
        .filter((item) => item.state !== state)
        .map((item) => item.id);
      const already = selectedSources.length - sourceIdsToUpdate.length;
      const stateLabel = state ? "启用" : "禁用";
      if (sourceIdsToUpdate.length === 0) {
        message.info(`选中采集站已全部${stateLabel}`);
        setSelectedSourceIds([]);
        return;
      }

      setBatchStateUpdating(true);
      try {
        const resp = await ApiPost("/manage/collect/state", {
          ids: sourceIdsToUpdate,
          state,
        });
        if (resp.code !== 0) {
          message.error(resp.msg || `批量${stateLabel}失败`);
        } else {
          const skippedText = already > 0 ? `，跳过 ${already} 个（已${stateLabel}）` : "";
          message.success(`已${stateLabel} ${sourceIdsToUpdate.length} 个${skippedText}`);
          setSelectedSourceIds([]);
        }
        await reloadList();
      } finally {
        setBatchStateUpdating(false);
      }
    },
    [message, reloadList, selectedSources],
  );

  const batchDeleteSources = useCallback(async () => {
    if (selectedSources.length === 0) {
      message.warning("请先选择采集站");
      return;
    }
    const ids = selectedSources.map((item) => item.id);
    const idSet = new Set(ids);
    setSiteList((current) =>
      current.map((item) => (idSet.has(item.id) ? { ...item, deleting: true } : item)),
    );
    setSelectedSourceIds([]);
    setBatchDeleting(true);
    try {
      const resp = await ApiPost("/manage/collect/delete", { ids });
      if (resp.code === 0) {
        message.success("已提交批量删除");
        return;
      }
      message.error(resp.msg || "批量删除失败");
      void reloadList();
    } catch {
      message.error("批量删除失败");
      void reloadList();
    } finally {
      setBatchDeleting(false);
    }
  }, [message, reloadList, selectedSources, setSiteList]);

  const deleteConfirmTitle = `删除 ${selectedSources.length} 个采集站？`;
  const deleteConfirmDescription = "";

  return {
    selectedSourceIds,
    setSelectedSourceIds,
    batchStateUpdating,
    batchDeleting,
    deleteConfirmTitle,
    deleteConfirmDescription,
    handleSelectSource,
    selectAllSources,
    invertSelection,
    clearSelection,
    batchChangeSourceState,
    batchDeleteSources,
  };
}
