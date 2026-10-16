import React, { useRef, useCallback } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { isCollectBusyStatus, type FilmSource } from "./types";

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
      const prevPrimary = prevList.find((s) => s.state) ?? prevList[0];
      const nextPrimary = nextList.find((s) => s.state) ?? nextList[0];
      const isPrimaryChanged = Boolean(prevPrimary && nextPrimary && prevPrimary.id !== nextPrimary.id);
      const collecting = prevList.some((item) => isCollectBusyStatus(item.progress?.status));
      if (isPrimaryChanged && collecting) {
        message.warning({ content: "采集中不能更换主站", key: "collect-sort" });
        return;
      }
      let assignedPrimary = false;
      const optimisticList = nextList.map((item) => {
        if (item.state && !assignedPrimary) {
          assignedPrimary = true;
          return { ...item, isPrimary: true };
        }
        return { ...item, isPrimary: false };
      });
      if (!assignedPrimary && optimisticList.length > 0) {
        optimisticList[0] = { ...optimisticList[0], isPrimary: true };
      }
      setSiteList(optimisticList);
      pendingSortRef.current = { prev: prevList, next: optimisticList };

      sortConfirmRef.current?.destroy();
      sortConfirmRef.current = modal.confirm({
        title: "保存顺序？",
        content: isPrimaryChanged && nextPrimary
          ? `主站改为「${nextPrimary.name}」。将清空轮播和排片配置，并关闭定时排片`
          : undefined,
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
                content: isPrimaryChanged && nextPrimary ? `主站：${nextPrimary.name}` : "顺序已保存",
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
