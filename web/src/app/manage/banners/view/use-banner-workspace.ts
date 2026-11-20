import { useCallback, useEffect, useRef, useState } from "react";
import axios from "axios";
import { API_CONFLICT, ApiDelete, ApiGet, ApiPut } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { shouldFollowPrimary } from "./banner-source-follow";
import { useBannerDeck } from "./use-banner-deck";
import { BannerListView, BannerRecord, BannerSourceOption } from "./types";

interface UseBannerWorkspaceOptions {
  hasPendingModal: boolean;
  onResetModals: (warnMessage?: string) => void;
}

/** 首页轮播工作区状态：支持按采集站切换与排序保存，同时响应全局主站变化。 */
export function useBannerWorkspace({
  hasPendingModal,
  onResetModals,
}: UseBannerWorkspaceOptions) {
  const { primary, loading: primaryLoading, refresh: refreshPrimary } = usePrimarySource();
  const primaryId = primary?.id || "";
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

  const [sources, setSources] = useState<BannerSourceOption[]>([]);
  const [sourceId, setSourceId] = useState("");
  const [loading, setLoading] = useState(false);
  const [orderSaving, setOrderSaving] = useState(false);

  const revisionRef = useRef("");
  const sourceIdRef = useRef(sourceId);
  const primaryIdRef = useRef(primaryId);
  const primaryLoadingRef = useRef(primaryLoading);
  const previousPrimaryIdRef = useRef("");
  const initializedRef = useRef(false);
  const requestSeqRef = useRef(0);
  const listAbortRef = useRef<AbortController | null>(null);
  // 切站请求还在飞时，保存/删除的刷新不能再发一次，否则会取消这次切换。
  const switchDepthRef = useRef(0);

  const messageRef = useRef(message);
  messageRef.current = message;
  const onResetModalsRef = useRef(onResetModals);
  onResetModalsRef.current = onResetModals;
  const pendingEditRef = useRef(false);
  pendingEditRef.current = orderDirty || hasPendingModal;

  sourceIdRef.current = sourceId;
  primaryIdRef.current = primaryId;
  primaryLoadingRef.current = primaryLoading;

  const fetchBanners = useCallback(async (overrideSourceId?: string) => {
    if (primaryLoadingRef.current && overrideSourceId === undefined) {
      return;
    }
    const currentSelected = sourceIdRef.current;
    const targetSourceId =
      overrideSourceId !== undefined ? overrideSourceId : currentSelected || primaryIdRef.current;

    listAbortRef.current?.abort();
    const controller = new AbortController();
    listAbortRef.current = controller;
    const requestSeq = ++requestSeqRef.current;
    setLoading(true);
    try {
      const resp = await ApiGet<BannerListView>(
        "/manage/banners",
        targetSourceId ? { sourceId: targetSourceId } : undefined,
        undefined,
        { signal: controller.signal },
      );
      if (controller.signal.aborted || requestSeq !== requestSeqRef.current) return;
      if (resp.code === 0 && resp.data) {
        if (Array.isArray(resp.data.sources)) {
          setSources(resp.data.sources);
        }
        if (
          typeof resp.data.primarySourceId === "string" &&
          resp.data.primarySourceId !== primaryIdRef.current
        ) {
          void refreshPrimary();
        }
        const resolvedSourceId = resp.data.sourceId || targetSourceId;
        sourceIdRef.current = resolvedSourceId;
        setSourceId(resolvedSourceId);
        revisionRef.current = resp.data.revision || "";
        if (resolvedSourceId !== currentSelected) {
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
  }, [absorbServerList, refreshPrimary, replaceList]);

  // 失败时不改列表和站点，旧数据还在，同一个源可以再选一次。
  const fetchSwitchedSource = useCallback(async (nextSourceId: string) => {
    switchDepthRef.current += 1;
    try {
      await fetchBanners(nextSourceId);
    } finally {
      switchDepthRef.current -= 1;
    }
  }, [fetchBanners]);

  const refreshAfterWrite = useCallback((sourceAtWrite: string, primaryAtWrite: string) => {
    if (switchDepthRef.current > 0) return;
    if (!sourceAtWrite || sourceIdRef.current !== sourceAtWrite) return;
    if (primaryIdRef.current !== primaryAtWrite) return;
    void fetchBanners(sourceAtWrite);
  }, [fetchBanners]);

  useEffect(() => {
    if (primaryLoading) {
      return;
    }
    if (!initializedRef.current) {
      initializedRef.current = true;
      previousPrimaryIdRef.current = primaryId;
      void fetchBanners();
      return;
    }
    const previousPrimaryId = previousPrimaryIdRef.current;
    if (previousPrimaryId === primaryId) {
      return;
    }
    previousPrimaryIdRef.current = primaryId;
    if (!shouldFollowPrimary(sourceIdRef.current, previousPrimaryId)) {
      return;
    }
    onResetModalsRef.current(
      pendingEditRef.current ? "主站已变更，未保存的轮播编辑已关闭" : undefined,
    );
    void fetchSwitchedSource(primaryId);
  }, [fetchBanners, fetchSwitchedSource, primaryId, primaryLoading]);

  useEffect(() => {
    return () => {
      initializedRef.current = false;
      listAbortRef.current?.abort();
    };
  }, []);

  const selectSource = useCallback(async (nextSourceId: string) => {
    if (!nextSourceId || nextSourceId === sourceIdRef.current) {
      return;
    }
    onResetModalsRef.current();
    await fetchSwitchedSource(nextSourceId);
  }, [fetchSwitchedSource]);

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
        switchDepthRef.current === 0 &&
        sourceIdRef.current === activeSourceId &&
        primaryIdRef.current === activePrimaryId;
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
        const resp = await ApiDelete(`/manage/banners/${encodeURIComponent(String(id))}${querySource}`);
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
    sources,
    sourceId,
    primaryId,
    banners,
    orderDirty,
    loading,
    orderSaving,
    canWrite,
    refreshAfterWrite,
    selectSource,
    handleSortList,
    handleMoveOrder,
    handleSaveOrder,
    handleDelete,
    discardOrder,
    applyPublishedDeck,
  };
}
