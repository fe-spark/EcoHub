import { useCallback, useEffect, useRef, useState } from "react";
import axios from "axios";
import { API_CONFLICT, ApiDelete, ApiGet, ApiPut } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useBannerDeck } from "./use-banner-deck";
import { BannerListView, BannerRecord, BannerSourceOption } from "./types";

interface UseBannerWorkspaceOptions {
  targetSourceId: string;
  primaryId: string;
  primaryLoading: boolean;
  onSourcesSync: (
    sources: BannerSourceOption[] | undefined,
    resolvedSourceId: string,
    primarySourceId?: string,
  ) => void;
  hasPendingModal: boolean;
  onResetModals: (warnMessage?: string) => void;
}

/** 首页轮播工作区状态：消费外层统一传入的采集站 ID，并维护排序暂存与保存。 */
export function useBannerWorkspace({
  targetSourceId,
  primaryId,
  primaryLoading,
  onSourcesSync,
  hasPendingModal,
  onResetModals,
}: UseBannerWorkspaceOptions) {
  const { canWrite } = useManagePermission();
  const { message } = useAppMessage();

  const {
    banners,
    bannersRef,
    orderDirty,
    setBanners,
    replaceList,
    absorbServerList,
    discardOrder,
  } = useBannerDeck();

  const [resolvedSourceId, setResolvedSourceId] = useState(targetSourceId);
  const [loading, setLoading] = useState(false);
  const [orderSaving, setOrderSaving] = useState(false);

  const revisionRef = useRef("");
  const sourceIdRef = useRef(resolvedSourceId);
  const primaryIdRef = useRef(primaryId);
  const requestSeqRef = useRef(0);
  const listAbortRef = useRef<AbortController | null>(null);
  const onSourcesSyncRef = useRef(onSourcesSync);
  const onResetModalsRef = useRef(onResetModals);
  const messageRef = useRef(message);

  sourceIdRef.current = resolvedSourceId;
  primaryIdRef.current = primaryId;
  onSourcesSyncRef.current = onSourcesSync;
  onResetModalsRef.current = onResetModals;
  messageRef.current = message;

  const fetchBanners = useCallback(
    async (sid: string) => {
      listAbortRef.current?.abort();
      const controller = new AbortController();
      listAbortRef.current = controller;
      const requestSeq = ++requestSeqRef.current;
      const prevSelected = sourceIdRef.current;
      setLoading(true);
      try {
        const resp = await ApiGet<BannerListView>(
          "/manage/banners",
          sid ? { sourceId: sid } : undefined,
          undefined,
          { signal: controller.signal },
        );
        if (controller.signal.aborted || requestSeq !== requestSeqRef.current) return;
        if (resp.code === 0 && resp.data) {
          const nextId = resp.data.sourceId || sid;
          onSourcesSyncRef.current(resp.data.sources, nextId, resp.data.primarySourceId);
          sourceIdRef.current = nextId;
          setResolvedSourceId(nextId);
          revisionRef.current = resp.data.revision || "";
          if (nextId !== prevSelected) {
            replaceList(resp.data.items || []);
          } else {
            absorbServerList(resp.data.items || []);
          }
        } else {
          messageRef.current.error(resp.msg || "获取轮播列表失败");
        }
      } catch (err) {
        if (controller.signal.aborted || axios.isCancel(err)) return;
        messageRef.current.error("网络异常，获取轮播列表失败");
      } finally {
        if (listAbortRef.current === controller) {
          listAbortRef.current = null;
        }
        if (requestSeq === requestSeqRef.current) {
          setLoading(false);
        }
      }
    },
    [absorbServerList, replaceList],
  );

  useEffect(() => {
    if (primaryLoading) return;
    if (hasPendingModal) {
      onResetModalsRef.current();
    }
    void fetchBanners(targetSourceId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fetchBanners, primaryLoading, targetSourceId]);

  useEffect(() => {
    return () => {
      listAbortRef.current?.abort();
    };
  }, []);

  const refreshAfterWrite = useCallback(
    (sourceAtWrite: string, primaryAtWrite: string) => {
      if (!sourceAtWrite || sourceIdRef.current !== sourceAtWrite) return;
      if (primaryIdRef.current !== primaryAtWrite) return;
      void fetchBanners(sourceAtWrite);
    },
    [fetchBanners],
  );

  const handleSortList = useCallback(
    (nextList: BannerRecord[]) => {
      setBanners(nextList);
    },
    [setBanners],
  );

  const handleMoveOrder = useCallback(
    (index: number, direction: "up" | "down") => {
      const current = bannersRef.current;
      const targetIndex = direction === "up" ? index - 1 : index + 1;
      if (targetIndex < 0 || targetIndex >= current.length) return;
      const updated = [...current];
      const temp = updated[index];
      updated[index] = updated[targetIndex];
      updated[targetIndex] = temp;
      setBanners(updated);
    },
    [bannersRef, setBanners],
  );

  const handleSaveOrder = useCallback(async () => {
    const activeSourceId = sourceIdRef.current;
    const activePrimaryId = primaryIdRef.current;
    if (!canWrite || !activeSourceId) return;
    const draft = bannersRef.current;
    setOrderSaving(true);
    try {
      const resp = await ApiPut<BannerListView>("/manage/banners/order", {
        sourceId: activeSourceId,
        revision: revisionRef.current,
        ids: draft.map((item) => item.id),
      });
      const stillHere =
        sourceIdRef.current === activeSourceId && primaryIdRef.current === activePrimaryId;
      if (resp.code === 0 && resp.data) {
        if (stillHere) {
          revisionRef.current = resp.data.revision;
          replaceList(resp.data.items || []);
        }
        message.success(resp.msg || "排片顺序已保存");
        return;
      }
      if (resp.code === API_CONFLICT) {
        message.warning(
          stillHere
            ? "轮播已被修改，已刷新为最新数据，请确认顺序后再保存"
            : "轮播已被修改，当前站点已切换，请回到原站点后再保存",
        );
        if (stillHere) refreshAfterWrite(activeSourceId, activePrimaryId);
        return;
      }
      message.error(resp.msg || "排序保存失败");
    } catch {
      message.error("网络异常，排序保存失败");
    } finally {
      setOrderSaving(false);
    }
  }, [bannersRef, canWrite, message, refreshAfterWrite, replaceList]);

  const handleDelete = useCallback(
    async (id: string) => {
      const activeSourceId = sourceIdRef.current;
      const activePrimaryId = primaryIdRef.current;
      if (!activeSourceId) return;
      try {
        const querySource = `?sourceId=${encodeURIComponent(activeSourceId)}`;
        const resp = await ApiDelete(
          `/manage/banners/${encodeURIComponent(String(id))}${querySource}`,
        );
        if (resp.code === 0) {
          message.success("删除成功");
          refreshAfterWrite(activeSourceId, activePrimaryId);
        } else {
          message.error(resp.msg || "删除失败");
        }
      } catch {
        message.error("网络异常，删除失败");
      }
    },
    [message, refreshAfterWrite],
  );

  const applyPublishedDeck = useCallback(
    (view: BannerListView) => {
      revisionRef.current = view.revision;
      replaceList(view.items || []);
    },
    [replaceList],
  );

  return {
    sourceId: resolvedSourceId || targetSourceId,
    banners,
    orderDirty,
    loading,
    orderSaving,
    canWrite,
    refreshAfterWrite,
    handleSortList,
    handleMoveOrder,
    handleSaveOrder,
    handleDelete,
    discardOrder,
    applyPublishedDeck,
  };
}
