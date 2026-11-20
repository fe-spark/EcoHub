import React, { useCallback, useMemo, useRef, useState } from "react";
import { useAppMessage } from "@/lib/useAppMessage";
import { API_PRIMARY_MISMATCH } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { getFilmClassTree, saveFilmClassTree } from "./api";
import {
  cloneTree,
  collectStats,
  moveCategoryWithinSameParent,
  serializeTree,
  updateTreeNodeVisibility,
  type FilmClassNode,
} from "./types";

export function useCategoryTreeState() {
  const { message } = useAppMessage();
  const { primary, loading: primaryLoading, refresh: refreshPrimary } = usePrimarySource();
  const primaryId = primary?.id || "";
  const [sources, setSources] = useState<{ id: string; name: string; state?: boolean }[]>([]);
  const [sourceId, setSourceId] = useState("");
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loadingTree, setLoadingTree] = useState(false);
  const [savingTree, setSavingTree] = useState(false);
  const sourceIdRef = useRef("");
  const previousPrimaryIdRef = useRef("");
  const pendingChangesRef = useRef(false);

  sourceIdRef.current = sourceId;

  const stats = useMemo(() => collectStats(classTree), [classTree]);
  const hasPendingChanges = useMemo(
    () => JSON.stringify(serializeTree(classTree)) !== JSON.stringify(serializeTree(originalTree)),
    [classTree, originalTree],
  );
  pendingChangesRef.current = hasPendingChanges;

  const fetchFilmClassTree = useCallback(async (keepExpanded = false, overrideSourceId?: string) => {
    if (primaryLoading) return;
    const previousPrimaryId = previousPrimaryIdRef.current;
    const primaryChanged = Boolean(previousPrimaryId && previousPrimaryId !== primaryId);
    const currentSelected = sourceIdRef.current;
    let targetSourceId = overrideSourceId !== undefined ? overrideSourceId : currentSelected || primaryId;

    if (overrideSourceId === undefined && primaryChanged && (!currentSelected || currentSelected === previousPrimaryId)) {
      if (pendingChangesRef.current) {
        message.warning("主站已变更，未保存的分类调整已丢弃");
      }
      targetSourceId = primaryId;
      setClassTree([]);
      setOriginalTree([]);
      setExpandedKeys([]);
    }

    setLoadingTree(true);
    try {
      const {
        resp,
        tree,
        sources: nextSources,
        currentSourceId,
        primarySourceId,
      } = await getFilmClassTree(targetSourceId);
      if (resp.code !== 0) {
        message.error(resp.msg || "分类数据加载失败");
        return;
      }
      if (primarySourceId && primarySourceId !== primaryId) {
        void refreshPrimary();
      }
      const resolvedSourceId = currentSourceId || targetSourceId;
      previousPrimaryIdRef.current = primaryId;
      setSources(nextSources);
      setSourceId(resolvedSourceId);
      setClassTree(tree);
      setOriginalTree(cloneTree(tree));
      if (!keepExpanded || resolvedSourceId !== currentSelected) {
        setExpandedKeys([]);
      }
    } finally {
      setLoadingTree(false);
    }
  }, [message, primaryId, primaryLoading, refreshPrimary]);

  const selectSource = useCallback(async (nextSourceId: string) => {
    if (!nextSourceId || nextSourceId === sourceIdRef.current) {
      return;
    }
    await fetchFilmClassTree(false, nextSourceId);
  }, [fetchFilmClassTree]);

  const saveTree = useCallback(async () => {
    const targetSourceId = sourceIdRef.current || primaryId;
    if (!targetSourceId) {
      message.warning("暂无可用采集站");
      return;
    }
    setSavingTree(true);
    try {
      const resp = await saveFilmClassTree(classTree, targetSourceId);
      if (resp.code !== 0) {
        if (resp.code === API_PRIMARY_MISMATCH) {
          message.warning(resp.msg || "主站已变更，请刷新后重试");
          void refreshPrimary();
          return;
        }
        message.error(resp.msg || "保存分类变更失败");
        return;
      }
      message.success(resp.msg || "分类变更已保存");
      await fetchFilmClassTree(true, targetSourceId);
    } finally {
      setSavingTree(false);
    }
  }, [classTree, fetchFilmClassTree, message, primaryId, refreshPrimary]);

  const discardTree = useCallback(() => {
    setClassTree(cloneTree(originalTree));
  }, [originalTree]);

  const moveClassWithinSameParent = useCallback((dragId: number, dropId: number) => {
    if (savingTree) {
      return;
    }
    setClassTree((prev) => moveCategoryWithinSameParent(prev, dragId, dropId));
  }, [savingTree]);

  const updateClassVisibility = useCallback((id: number, show: boolean) => {
    if (savingTree) {
      return;
    }
    setClassTree((prev) => updateTreeNodeVisibility(prev, id, show));
  }, [savingTree]);

  return {
    sources,
    sourceId,
    primaryId,
    classTree,
    expandedKeys,
    loadingTree,
    savingTree,
    stats,
    hasPendingChanges,
    fetchFilmClassTree,
    selectSource,
    saveTree,
    discardTree,
    setExpandedKeys,
    moveClassWithinSameParent,
    updateClassVisibility,
  };
}
