import React, { useCallback, useMemo, useState } from "react";
import { useAppMessage } from "@/lib/useAppMessage";
import { getFilmClassTree, resetFilmClassTree, saveFilmClassTree } from "./api";
import {
  cloneTree,
  collectStats,
  moveCategoryWithinSameParent,
  normalizeTree,
  removeTreeNode,
  serializeTree,
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

  const queueDeleteClass = useCallback(
    (id: number) => {
      setClassTree((prev) => normalizeTree(removeTreeNode(prev, id)));
      message.success("删除操作已加入待保存变更");
    },
    [message],
  );

  const moveClassWithinSameParent = useCallback((dragId: number, dropId: number) => {
    setClassTree((prev) => moveCategoryWithinSameParent(prev, dragId, dropId));
  }, []);

  return {
    classTree,
    expandedKeys,
    loadingTree,
    savingTree,
    resettingTree,
    stats,
    hasPendingChanges,
    fetchFilmClassTree,
    resetTree,
    saveTree,
    setExpandedKeys,
    moveClassWithinSameParent,
    queueDeleteClass,
  };
}
