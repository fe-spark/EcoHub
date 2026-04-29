import React, { useCallback, useMemo, useState } from "react";
import { useAppMessage } from "@/lib/useAppMessage";
import { getFilmClassTree, resetFilmClassTree, saveFilmClassTree, updateFilmClassShow } from "./api";
import {
  cloneTree,
  collectStats,
  moveCategoryWithinSameParent,
  normalizeTree,
  serializeTree,
  updateTreeNodeVisibility,
  type FilmClassNode,
} from "./types";

export function useCategoryTreeState() {
  const { message } = useAppMessage();
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loadingTree, setLoadingTree] = useState(false);
  const [savingTree, setSavingTree] = useState(false);
  const [resettingTree, setResettingTree] = useState(false);
  const [updatingShowIds, setUpdatingShowIds] = useState<number[]>([]);

  const stats = useMemo(() => collectStats(classTree), [classTree]);
  const hasPendingChanges = useMemo(
    () => JSON.stringify(serializeTree(classTree)) !== JSON.stringify(serializeTree(originalTree)),
    [classTree, originalTree],
  );

  const fetchFilmClassTree = useCallback(async () => {
    setLoadingTree(true);
    try {
      const { resp, tree } = await getFilmClassTree();
      if (resp.code !== 0) {
        message.error(resp.msg || "分类数据加载失败");
        return;
      }
      setClassTree(tree);
      setOriginalTree(cloneTree(tree));
      setExpandedKeys([]);
    } finally {
      setLoadingTree(false);
    }
  }, [message]);

  const resetTree = useCallback(async () => {
    setResettingTree(true);
    try {
      const resp = await resetFilmClassTree();
      if (resp.code !== 0) {
        message.error(resp.msg || "重置分类失败");
        return false;
      }
      message.success(resp.msg || "分类已重置");
      await fetchFilmClassTree();
      return true;
    } finally {
      setResettingTree(false);
    }
  }, [fetchFilmClassTree, message]);

  const saveTree = useCallback(async () => {
    setSavingTree(true);
    try {
      const resp = await saveFilmClassTree(classTree);
      if (resp.code !== 0) {
        message.error(resp.msg || "保存分类变更失败");
        return;
      }
      message.success(resp.msg || "分类变更已保存");
      await fetchFilmClassTree();
    } finally {
      setSavingTree(false);
    }
  }, [classTree, fetchFilmClassTree, message]);

  const moveClassWithinSameParent = useCallback((dragId: number, dropId: number) => {
    setClassTree((prev) => moveCategoryWithinSameParent(prev, dragId, dropId));
  }, []);

  const updateClassVisibility = useCallback(
    async (id: number, show: boolean) => {
      setUpdatingShowIds((prev) => [...prev, id]);
      try {
        const resp = await updateFilmClassShow(id, show);
        if (resp.code !== 0) {
          message.error(resp.msg || "更新分类显示状态失败");
          return;
        }
        setClassTree((prev) => updateTreeNodeVisibility(prev, id, show));
        setOriginalTree((prev) => updateTreeNodeVisibility(prev, id, show));
        message.success(show ? "分类已显示" : "分类已隐藏");
      } finally {
        setUpdatingShowIds((prev) => prev.filter((item) => item !== id));
      }
    },
    [message],
  );

  return {
    classTree,
    expandedKeys,
    loadingTree,
    savingTree,
    resettingTree,
    updatingShowIds,
    stats,
    hasPendingChanges,
    fetchFilmClassTree,
    resetTree,
    saveTree,
    setExpandedKeys,
    moveClassWithinSameParent,
    updateClassVisibility,
  };
}
