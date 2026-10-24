import { useState, useCallback, useRef, useEffect } from "react";
import type { DeletingQueueItem } from "./delete-queue-bar";
import type { SourceDeleteEvent } from "./use-collect-stream";
import type { FilmSource } from "./types";

export function useCollectDeleteQueue() {
  const [deleteQueueItems, setDeleteQueueItems] = useState<DeletingQueueItem[]>([]);
  const autoDismissTimerRef = useRef<NodeJS.Timeout | null>(null);

  const clearAutoDismiss = useCallback(() => {
    if (autoDismissTimerRef.current) {
      clearTimeout(autoDismissTimerRef.current);
      autoDismissTimerRef.current = null;
    }
  }, []);

  const dismissDeleteQueue = useCallback(() => {
    clearAutoDismiss();
    setDeleteQueueItems([]);
  }, [clearAutoDismiss]);

  const enqueueDeletingSources = useCallback(
    (sources: { id: string; name: string }[]) => {
      clearAutoDismiss();
      setDeleteQueueItems((prev) => {
        const next = [...prev];
        for (const src of sources) {
          const idx = next.findIndex((item) => item.id === src.id);
          if (idx >= 0) {
            next[idx] = {
              ...next[idx],
              status: next[idx].status === "deleted" ? "deleted" : "pending",
            };
          } else {
            next.push({
              id: src.id,
              name: src.name,
              status: "pending",
            });
          }
        }
        return next;
      });
    },
    [clearAutoDismiss],
  );

  const handleSourceDeleteEvent = useCallback(
    (evt: SourceDeleteEvent) => {
      clearAutoDismiss();
      setDeleteQueueItems((prev) => {
        const idx = prev.findIndex((item) => item.id === evt.sourceId);
        const next = [...prev];
        const status =
          evt.status === "deleting"
            ? "deleting"
            : evt.status === "deleted"
              ? "deleted"
              : "failed";

        const deletedFilms = evt.deletedFilms ?? 0;

        if (idx >= 0) {
          next[idx] = {
            ...next[idx],
            name: evt.sourceName || next[idx].name,
            status,
            deletedFilms: deletedFilms || next[idx].deletedFilms,
          };
        } else if (evt.status === "deleting") {
          next.push({
            id: evt.sourceId,
            name: evt.sourceName || evt.sourceId,
            status: "deleting",
            deletedFilms,
          });
        }
        return next;
      });
    },
    [clearAutoDismiss],
  );

  const handleDeleteSnapshot = useCallback(
    (ids: string[], siteList: FilmSource[]) => {
      if (ids.length === 0) return;
      const siteMap = new Map(siteList.map((s) => [s.id, s.name]));
      setDeleteQueueItems((prev) => {
        const next = [...prev];
        for (const id of ids) {
          if (!next.some((item) => item.id === id)) {
            next.push({
              id,
              name: siteMap.get(id) || id,
              status: "deleting",
            });
          }
        }
        return next;
      });
    },
    [],
  );

  const hydrateSourceNames = useCallback((siteList: FilmSource[]) => {
    if (siteList.length === 0) return;
    const siteMap = new Map(siteList.map((s) => [s.id, s.name]));
    setDeleteQueueItems((prev) => {
      let changed = false;
      const next = prev.map((item) => {
        if (item.name === item.id && siteMap.has(item.id)) {
          changed = true;
          return { ...item, name: siteMap.get(item.id)! };
        }
        return item;
      });
      return changed ? next : prev;
    });
  }, []);

  useEffect(() => {
    if (deleteQueueItems.length === 0) return;
    const allTerminal = deleteQueueItems.every(
      (item) => item.status === "deleted" || item.status === "failed",
    );
    if (allTerminal) {
      autoDismissTimerRef.current = setTimeout(() => {
        setDeleteQueueItems([]);
      }, 5000);
      return () => {
        if (autoDismissTimerRef.current) {
          clearTimeout(autoDismissTimerRef.current);
          autoDismissTimerRef.current = null;
        }
      };
    }
  }, [deleteQueueItems]);

  return {
    deleteQueueItems,
    enqueueDeletingSources,
    handleSourceDeleteEvent,
    handleDeleteSnapshot,
    hydrateSourceNames,
    dismissDeleteQueue,
  };
}
