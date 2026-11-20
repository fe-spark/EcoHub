import React, { useRef, useCallback } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { type FilmSource } from "./types";

export function useCollectSort(
  siteList: FilmSource[],
  setSiteList: React.Dispatch<React.SetStateAction<FilmSource[]>>,
) {
  const { message, modal } = useAppMessage();
  const pendingSortRef = useRef<{ prev: FilmSource[]; next: FilmSource[] } | null>(null);
  const sortConfirmRef = useRef<{ destroy: () => void } | null>(null);

  const handleSortList = useCallback(
    (nextList: FilmSource[]) => {
      const prevList = pendingSortRef.current?.prev ?? siteList;
      const primary = prevList.find((item) => item.isPrimary);
      if (primary && nextList[0]?.id !== primary.id) {
        message.warning({ content: "当前主站必须保持在首位", key: "collect-sort" });
        return;
      }
      setSiteList(nextList);
      pendingSortRef.current = { prev: prevList, next: nextList };

      sortConfirmRef.current?.destroy();
      sortConfirmRef.current = modal.confirm({
        title: "保存顺序？",
        okText: "保存",
        cancelText: "取消",
        centered: true,
        onOk: async () => {
          const pending = pendingSortRef.current;
          if (!pending) return;
          try {
            const resp = await ApiPost("/manage/collect/sort", {
              ids: pending.next.map((item) => item.id),
            });
            if (resp.code === 0) {
              pendingSortRef.current = null;
              message.success({
                content: "顺序已保存",
                key: "collect-sort",
              });
              return;
            }
            message.error({ content: resp.msg || "保存排序失败", key: "collect-sort" });
            pendingSortRef.current = null;
            setSiteList(pending.prev);
          } catch (e: any) {
            message.error({ content: e?.message || "保存排序失败", key: "collect-sort" });
            pendingSortRef.current = null;
            setSiteList(pending.prev);
          }
        },
        onCancel: () => {
          const pending = pendingSortRef.current;
          pendingSortRef.current = null;
          if (pending) setSiteList(pending.prev);
        },
      });
    },
    [message, modal, setSiteList, siteList],
  );

  const cleanupSort = useCallback(() => {
    sortConfirmRef.current?.destroy();
    sortConfirmRef.current = null;
    pendingSortRef.current = null;
  }, []);

  return {
    pendingSortRef,
    handleSortList,
    cleanupSort,
  };
}
