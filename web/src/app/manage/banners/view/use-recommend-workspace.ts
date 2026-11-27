import { useCallback, useEffect, useRef, useState } from "react";
import { type DragEndEvent } from "@dnd-kit/core";
import { arrayMove } from "@dnd-kit/sortable";
import { ApiDelete, ApiGet, ApiPatch, ApiPost, ApiPut } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerSourceOption, FilmOption } from "./types";
import {
  ChartOption,
  RecommendAdminGroup,
  RecommendCategoryOption,
} from "./recommend-types";

interface RecommendSyncResult {
  groupId: string;
  title: string;
  chartKey: string;
  candidateCount: number;
  matchedCount: number;
  error?: string;
}

interface RecommendBatchSyncResult {
  syncedGroups: number;
  totalMatched: number;
  failedGroups: number;
  results?: RecommendSyncResult[];
}

interface UseRecommendWorkspaceOptions {
  sourceId: string;
  primaryLoading: boolean;
  onSourcesSync?: (
    sources: BannerSourceOption[] | undefined,
    resolvedSourceId: string,
    primarySourceId?: string,
  ) => void;
  onDirtyChange?: (dirty: boolean) => void;
  onBusyChange?: (busy: boolean) => void;
}

export function useRecommendWorkspace({
  sourceId,
  primaryLoading,
  onSourcesSync,
  onDirtyChange,
  onBusyChange,
}: UseRecommendWorkspaceOptions) {
  const { message, modal } = useAppMessage();
  const messageRef = useRef(message);
  const onSourcesSyncRef = useRef(onSourcesSync);
  useEffect(() => {
    messageRef.current = message;
    onSourcesSyncRef.current = onSourcesSync;
  }, [message, onSourcesSync]);

  const [loading, setLoading] = useState(true);
  const [resolvedSourceId, setResolvedSourceId] = useState(sourceId);
  const [chartOptions, setChartOptions] = useState<ChartOption[]>([]);
  const [categoryOptions, setCategoryOptions] = useState<RecommendCategoryOption[]>([]);
  const [serverGroups, setServerGroups] = useState<RecommendAdminGroup[]>([]);
  const [groups, setGroups] = useState<RecommendAdminGroup[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState("");
  const [savingChanges, setSavingChanges] = useState(false);
  const [presetting, setPresetting] = useState(false);
  const [importingCategories, setImportingCategories] = useState(false);
  const [syncingGroupId, setSyncingGroupId] = useState<string | null>(null);
  const requestSeqRef = useRef(0);
  const abortControllerRef = useRef<AbortController | null>(null);

  const loadData = useCallback(async (targetSourceId: string) => {
    const seq = ++requestSeqRef.current;
    abortControllerRef.current?.abort();
    const controller = new AbortController();
    abortControllerRef.current = controller;
    setLoading(true);
    try {
      const recRes = await ApiGet<{
        sourceId: string;
        primarySourceId: string;
        sources?: BannerSourceOption[];
        chartOptions: ChartOption[];
        categoryOptions?: RecommendCategoryOption[];
        groups: RecommendAdminGroup[];
      }>(
        targetSourceId
          ? `/manage/recommends?sourceId=${encodeURIComponent(targetSourceId)}`
          : "/manage/recommends",
        undefined,
        undefined,
        { signal: controller.signal },
      );
      if (seq !== requestSeqRef.current) return;
      if (recRes.code === 0 && recRes.data) {
        const nextGroups = recRes.data.groups || [];
        const nextResolved = recRes.data.sourceId || targetSourceId;
        onSourcesSyncRef.current?.(recRes.data.sources, nextResolved, recRes.data.primarySourceId);
        setResolvedSourceId(nextResolved);
        setChartOptions(recRes.data.chartOptions || []);
        setCategoryOptions(recRes.data.categoryOptions || []);
        setServerGroups(nextGroups);
        setGroups(nextGroups);
        setSelectedGroupId((prev) =>
          nextGroups.some((g) => g.id === prev) ? prev : nextGroups[0]?.id || "",
        );
      } else {
        messageRef.current.error(recRes.msg || "加载首页推荐失败");
      }
    } catch {
      if (seq !== requestSeqRef.current || controller.signal.aborted) return;
      messageRef.current.error("加载首页推荐失败");
    } finally {
      if (seq === requestSeqRef.current) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    if (primaryLoading) return;
    void loadData(sourceId);
    return () => {
      abortControllerRef.current?.abort();
    };
  }, [loadData, primaryLoading, sourceId]);

  const activeSourceId = sourceId || resolvedSourceId;
  const selectedGroup = groups.find((g) => g.id === selectedGroupId) || groups[0] || null;

  const groupOrderDirty =
    groups.length === serverGroups.length &&
    groups.some((g, idx) => g.id !== serverGroups[idx]?.id);

  const serverById = new Map(serverGroups.map((g) => [g.id, g]));
  const toggledGroups = groups.filter((g) => {
    const orig = serverById.get(g.id);
    return orig && orig.enabled !== g.enabled;
  });
  const itemOrderDirtyGroups = groups.filter((g) => {
    if (g.categoryId > 0) return false;
    const orig = serverById.get(g.id);
    if (!orig || orig.items.length !== g.items.length) return false;
    return g.items.some((it, idx) => it.itemId !== orig.items[idx]?.itemId);
  });

  const hasUnsavedChanges =
    groupOrderDirty || toggledGroups.length > 0 || itemOrderDirtyGroups.length > 0;

  useEffect(() => {
    onDirtyChange?.(hasUnsavedChanges);
  }, [hasUnsavedChanges, onDirtyChange]);

  useEffect(() => {
    onBusyChange?.(
      loading || savingChanges || presetting || importingCategories || Boolean(syncingGroupId),
    );
  }, [importingCategories, loading, onBusyChange, presetting, savingChanges, syncingGroupId]);

  useEffect(() => {
    if (!hasUnsavedChanges) return;
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => window.removeEventListener("beforeunload", handleBeforeUnload);
  }, [hasUnsavedChanges]);

  const handleGroupDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = groups.findIndex((g) => g.id === active.id);
    const newIndex = groups.findIndex((g) => g.id === over.id);
    if (oldIndex === -1 || newIndex === -1) return;
    setGroups((prev) => arrayMove(prev, oldIndex, newIndex));
  };

  const handleItemDragEnd = (event: DragEndEvent) => {
    if (!selectedGroup) return;
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = selectedGroup.items.findIndex((i) => i.itemId === active.id);
    const newIndex = selectedGroup.items.findIndex((i) => i.itemId === over.id);
    if (oldIndex === -1 || newIndex === -1) return;
    const nextItems = arrayMove(selectedGroup.items, oldIndex, newIndex);
    setGroups((prev) =>
      prev.map((g) => (g.id === selectedGroup.id ? { ...g, items: nextItems } : g)),
    );
  };

  const handleToggleEnabled = (groupId: string, checked: boolean) => {
    setGroups((prev) =>
      prev.map((item) => (item.id === groupId ? { ...item, enabled: checked } : item)),
    );
  };

  const handleSaveAllChanges = async () => {
    if (!hasUnsavedChanges || !activeSourceId) return;
    setSavingChanges(true);
    try {
      if (groupOrderDirty) {
        const res = await ApiPut("/manage/recommends/groups/order", {
          sourceId: activeSourceId,
          ids: groups.map((g) => g.id),
        });
        if (res.code !== 0) {
          message.error(res.msg || "分组排序保存失败");
          return;
        }
      }
      for (const g of toggledGroups) {
        const res = await ApiPatch(`/manage/recommends/groups/${g.id}`, {
          enabled: g.enabled,
        });
        if (res.code !== 0) {
          message.error(res.msg || `更新「${g.title}」开关失败`);
          return;
        }
      }
      for (const g of itemOrderDirtyGroups) {
        const res = await ApiPut(`/manage/recommends/groups/${g.id}/items/order`, {
          ids: g.items.map((i) => i.itemId),
        });
        if (res.code !== 0) {
          message.error(res.msg || `保存「${g.title}」影片排序失败`);
          return;
        }
      }
      message.success("已保存首页推荐配置");
      await loadData(activeSourceId);
    } finally {
      setSavingChanges(false);
    }
  };

  const notifyBatchSummary = (actionLabel: string, summary?: RecommendBatchSyncResult) => {
    if (!summary || summary.syncedGroups === 0) {
      message.success(actionLabel);
      return;
    }
    if (summary.failedGroups > 0) {
      message.warning(
        `${actionLabel}，命中 ${summary.totalMatched} 部（${summary.failedGroups} 个分组榜单拉取失败，请检查网络或代理）`,
      );
      return;
    }
    if (summary.totalMatched === 0) {
      message.warning(`${actionLabel}，已拉取外部榜单，但当前采集站片库暂无匹配的可播影片`);
      return;
    }
    message.success(`${actionLabel}，共同步 ${summary.totalMatched} 部可播影片`);
  };

  const syncGroup = async (g: RecommendAdminGroup) => {
    if (syncingGroupId) return;
    setSyncingGroupId(g.id);
    try {
      const res = await ApiPost<RecommendSyncResult>(`/manage/recommends/groups/${g.id}/sync`, {});
      if (res.code === 0) {
        const matched = res.data?.matchedCount ?? 0;
        const candidates = res.data?.candidateCount ?? 0;
        if (matched > 0) {
          message.success(`「${g.title}」已同步 ${matched} 部影片（榜单共 ${candidates} 条）`);
        } else if (candidates > 0) {
          message.warning(`「${g.title}」已拉取 ${candidates} 条榜单，但当前采集站片库暂无匹配的可播影片`);
        } else {
          message.warning(`「${g.title}」榜单暂无可用候选条目`);
        }
        await loadData(activeSourceId);
      } else {
        message.error(res.msg || "同步失败");
      }
    } catch {
      message.error("同步失败");
    } finally {
      setSyncingGroupId(null);
    }
  };

  const syncAllGroups = async () => {
    if (!activeSourceId || syncingGroupId) return;
    setSyncingGroupId("all");
    try {
      const res = await ApiPost<RecommendBatchSyncResult>("/manage/recommends/sync-all", {
        sourceId: activeSourceId,
      });
      if (res.code === 0) {
        notifyBatchSummary("榜单批量同步完成", res.data);
        await loadData(activeSourceId);
      } else {
        message.error(res.msg || "批量同步失败");
      }
    } catch {
      message.error("批量同步失败");
    } finally {
      setSyncingGroupId(null);
    }
  };

  const clearGroup = (g: RecommendAdminGroup) => {
    modal.confirm({
      title: `清空「${g.title}」下的全部影片？`,
      okText: "清空",
      okButtonProps: { danger: true },
      onOk: async () => {
        const res = await ApiDelete(`/manage/recommends/groups/${g.id}/items`);
        if (res.code === 0) {
          message.success("已清空");
          await loadData(activeSourceId);
        } else {
          message.error(res.msg || "清空失败");
        }
      },
    });
  };

  const deleteGroup = (g: RecommendAdminGroup) => {
    modal.confirm({
      title: `删除分组「${g.title}」？`,
      content: "删除后该分组及内部已排影片将一并移除。",
      okText: "删除",
      okButtonProps: { danger: true },
      onOk: async () => {
        const res = await ApiDelete(`/manage/recommends/groups/${g.id}`);
        if (res.code === 0) {
          message.success("已删除分组");
          await loadData(activeSourceId);
        } else {
          message.error(res.msg || "删除失败");
        }
      },
    });
  };

  const pickFilm = async (film: FilmOption, onAdded: () => void) => {
    if (!selectedGroup) return;
    const res = await ApiPost(`/manage/recommends/groups/${selectedGroup.id}/items`, {
      mid: film.id,
    });
    if (res.code === 0) {
      message.success(`已将「${film.name}」加入「${selectedGroup.title}」`);
      onAdded();
      await loadData(activeSourceId);
    } else {
      message.error(res.msg || "添加失败");
    }
  };

  const removeItem = async (itemId: string) => {
    const res = await ApiDelete(`/manage/recommends/items/${itemId}`);
    if (res.code === 0) {
      message.success("已移出");
      await loadData(activeSourceId);
    } else {
      message.error(res.msg || "移出失败");
    }
  };

  const initPresets = async () => {
    if (!activeSourceId || presetting) return;
    setPresetting(true);
    try {
      const res = await ApiPost<{ syncSummary?: RecommendBatchSyncResult }>(
        "/manage/recommends/presets",
        { sourceId: activeSourceId },
      );
      if (res.code === 0) {
        notifyBatchSummary("已导入热门榜单预设分组", res.data?.syncSummary);
        await loadData(activeSourceId);
      } else {
        message.error(res.msg || "导入预设分组失败");
      }
    } catch {
      message.error("导入预设分组失败");
    } finally {
      setPresetting(false);
    }
  };

  const importCategories = async () => {
    if (!activeSourceId || importingCategories) return;
    setImportingCategories(true);
    try {
      const res = await ApiPost<{ createdCount?: number }>(
        "/manage/recommends/categories/import",
        { sourceId: activeSourceId },
      );
      if (res.code === 0) {
        const count = res.data?.createdCount ?? 0;
        if (count > 0) {
          message.success(`已导入 ${count} 个站点分类分组`);
        } else {
          message.info("当前站点可见一级分类已全部导入");
        }
        await loadData(activeSourceId);
      } else {
        message.error(res.msg || "导入站点分类失败");
      }
    } catch {
      message.error("导入站点分类失败");
    } finally {
      setImportingCategories(false);
    }
  };

  return {
    loading,
    activeSourceId,
    chartOptions,
    categoryOptions,
    groups,
    selectedGroup,
    hasUnsavedChanges,
    savingChanges,
    presetting,
    importingCategories,
    syncingGroupId,
    loadData,
    setSelectedGroupId,
    discardChanges: () => setGroups(serverGroups),
    handleGroupDragEnd,
    handleItemDragEnd,
    handleToggleEnabled,
    handleSaveAllChanges,
    syncGroup,
    syncAllGroups,
    clearGroup,
    deleteGroup,
    pickFilm,
    removeItem,
    initPresets,
    importCategories,
  };
}
