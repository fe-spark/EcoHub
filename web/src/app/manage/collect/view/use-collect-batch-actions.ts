import React, { useState, useCallback } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import type { DelBatchResult, FilmSource } from "./types";

export function useCollectBatchActions(
  siteList: FilmSource[],
  reloadList: () => Promise<void>,
) {
  const { message } = useAppMessage();
  const [selectedSourceIds, setSelectedSourceIds] = useState<React.Key[]>([]);
  const [batchStateUpdating, setBatchStateUpdating] = useState(false);
  const [batchDeleting, setBatchDeleting] = useState(false);

  const handleSelectSource = useCallback((id: string, checked: boolean) => {
    setSelectedSourceIds((current) =>
      checked ? [...current, id] : current.filter((item) => item !== id),
    );
  }, []);

  const selectAllSources = useCallback(() => {
    setSelectedSourceIds(
      siteList
        .filter((item) => item.categoryReady !== false)
        .map((item) => item.id),
    );
  }, [siteList]);

  const invertSelection = useCallback(() => {
    setSelectedSourceIds((current) =>
      siteList
        .filter((item) => item.categoryReady !== false && !current.includes(item.id))
        .map((item) => item.id),
    );
  }, [siteList]);

  const clearSelection = useCallback(() => {
    setSelectedSourceIds([]);
  }, []);

  const batchChangeSourceState = useCallback(
    async (state: boolean) => {
      const selectedSources = siteList.filter((item) => selectedSourceIds.includes(item.id));
      if (selectedSources.length === 0) {
        message.warning("请先选择采集站");
        return;
      }

      const sourceIdsToUpdate = selectedSources
        .filter((item) => item.state !== state)
        .map((item) => item.id);

      if (sourceIdsToUpdate.length === 0) {
        message.info(state ? "选中采集站已全部启用" : "选中采集站已全部禁用");
        return;
      }

      setBatchStateUpdating(true);
      try {
        const resp = await ApiPost("/manage/collect/change/batch", {
          ids: sourceIdsToUpdate,
          state,
        });
        if (resp.code !== 0) {
          message.error(resp.msg || `批量${state ? "启用" : "禁用"}失败`);
        } else {
          message.success(`已${state ? "启用" : "禁用"} ${sourceIdsToUpdate.length} 个采集站`);
        }
        await reloadList();
      } finally {
        setBatchStateUpdating(false);
      }
    },
    [message, reloadList, selectedSourceIds, siteList],
  );

  const batchDeleteSources = useCallback(async () => {
    const ids = selectedSourceIds.map(String).filter(Boolean);
    if (ids.length === 0) {
      message.warning("请先选择采集站");
      return;
    }
    setBatchDeleting(true);
    try {
      const resp = await ApiPost<DelBatchResult>("/manage/collect/del/batch", { ids });
      if (resp.code === 0) {
        const data = resp.data ?? { deleted: [], skipped: [] };
        const deleted = Array.isArray(data.deleted) ? data.deleted : [];
        const skipped = Array.isArray(data.skipped) ? data.skipped : [];
        if (deleted.length > 0) {
          message.success(`已删除 ${deleted.length} 个采集站`);
        } else {
          message.warning("没有可删除的采集站");
        }
        if (skipped.length > 0) {
          message.warning(
            `${skipped.length} 个未删除：${skipped
              .map((item) => `${item.name || item.id}（${item.reason}）`)
              .join("；")}`,
          );
        }
        setSelectedSourceIds((current) =>
          current.filter((id) => !deleted.includes(String(id))),
        );
        await reloadList();
        return;
      }
      message.error(resp.msg || "批量删除失败");
    } finally {
      setBatchDeleting(false);
    }
  }, [message, reloadList, selectedSourceIds]);

  return {
    selectedSourceIds,
    setSelectedSourceIds,
    batchStateUpdating,
    batchDeleting,
    handleSelectSource,
    selectAllSources,
    invertSelection,
    clearSelection,
    batchChangeSourceState,
    batchDeleteSources,
  };
}
