import React, { useCallback, useMemo, useRef, useState } from "react";
import { useAppMessage } from "@/lib/useAppMessage";
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
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [expandedKeys, setExpandedKeys] = useState<React.Key[]>([]);
  const [loadingTree, setLoadingTree] = useState(false);
  const [savingTree, setSavingTree] = useState(false);
  const [sources, setSources] = useState<{ id: string; name: string }[]>([]);
  const [sourceId, setSourceId] = useState("");
  const sourceIdRef = useRef("");

  const stats = useMemo(() => collectStats(classTree), [classTree]);
  const hasPendingChanges = useMemo(
    () => JSON.stringify(serializeTree(classTree)) !== JSON.stringify(serializeTree(originalTree)),
    [classTree, originalTree],
  );

  const fetchFilmClassTree = useCallback(async (nextSourceId?: string, keepExpanded = false) => {
    const previousSourceId = sourceIdRef.current;
    setLoadingTree(true);
    try {
      const requested = nextSourceId ?? sourceIdRef.current;
      const { resp, tree, sources: nextSources, defaultSourceId: nextDefault, currentSourceId } =
        await getFilmClassTree(requested);
      if (resp.code !== 0) {
        message.error(resp.msg || "分类数据加载失败");
        return;
      }
      const activeSourceId = currentSourceId || nextDefault || requested;
      sourceIdRef.current = activeSourceId;
      setSourceId(activeSourceId);
      setSources(nextSources);
      setClassTree(tree);
      setOriginalTree(cloneTree(tree));
      if (!keepExpanded || previousSourceId !== activeSourceId) {
        setExpandedKeys([]);
      }
    } finally {
      setLoadingTree(false);
    }
  }, [message]);

  const saveTree = useCallback(async () => {
    setSavingTree(true);
    try {
      const resp = await saveFilmClassTree(classTree, sourceIdRef.current);
      if (resp.code !== 0) {
        message.error(resp.msg || "保存分类变更失败");
        return;
      }
      message.success(resp.msg || "分类变更已保存");
      await fetchFilmClassTree(sourceIdRef.current, true);
    } finally {
      setSavingTree(false);
    }
  }, [classTree, fetchFilmClassTree, message]);

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
    sources,
    sourceId,
    fetchFilmClassTree,
    saveTree,
    discardTree,
    setExpandedKeys,
    moveClassWithinSameParent,
    updateClassVisibility,
  };
}
