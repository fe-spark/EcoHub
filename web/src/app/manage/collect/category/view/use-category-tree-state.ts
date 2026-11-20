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
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loadingTree, setLoadingTree] = useState(false);
  const [savingTree, setSavingTree] = useState(false);
  const previousPrimaryIdRef = useRef("");
  const pendingChangesRef = useRef(false);

  const stats = useMemo(() => collectStats(classTree), [classTree]);
  const hasPendingChanges = useMemo(
    () => JSON.stringify(serializeTree(classTree)) !== JSON.stringify(serializeTree(originalTree)),
    [classTree, originalTree],
  );
  pendingChangesRef.current = hasPendingChanges;

  const fetchFilmClassTree = useCallback(async (keepExpanded = false) => {
    if (primaryLoading) return;
    const previousPrimaryId = previousPrimaryIdRef.current;
    if (previousPrimaryId && previousPrimaryId !== primaryId) {
      if (pendingChangesRef.current) {
        message.warning("主站已变更，未保存的分类调整已丢弃");
      }
      setClassTree([]);
      setOriginalTree([]);
      setExpandedKeys([]);
    }
    setLoadingTree(true);
    try {
      if (!primaryId) {
        previousPrimaryIdRef.current = "";
        setClassTree([]);
        setOriginalTree([]);
        setExpandedKeys([]);
        return;
      }
      const { resp, tree, primarySourceId } = await getFilmClassTree();
      if (resp.code !== 0) {
        message.error(resp.msg || "分类数据加载失败");
        return;
      }
      if (typeof primarySourceId === "string" && primarySourceId !== primaryId) {
        setClassTree([]);
        setOriginalTree([]);
        setExpandedKeys([]);
        void refreshPrimary();
        return;
      }
      previousPrimaryIdRef.current = primaryId;
      setClassTree(tree);
      setOriginalTree(cloneTree(tree));
      if (!keepExpanded || previousPrimaryId !== primaryId) {
        setExpandedKeys([]);
      }
    } finally {
      setLoadingTree(false);
    }
  }, [message, primaryId, primaryLoading, refreshPrimary]);

  const saveTree = useCallback(async () => {
    setSavingTree(true);
    try {
      const resp = await saveFilmClassTree(classTree, primaryId);
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
      await fetchFilmClassTree(true);
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
    classTree,
    expandedKeys,
    loadingTree,
    savingTree,
    stats,
    hasPendingChanges,
    fetchFilmClassTree,
    saveTree,
    discardTree,
    setExpandedKeys,
    moveClassWithinSameParent,
    updateClassVisibility,
  };
}
