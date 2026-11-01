import { useCallback, useState } from "react";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { newClientCollectQueueId } from "./collect-queue";
import {
  isActiveCollectStatus,
  makeStartingProgress,
  progressAsStopping,
  type FilmSource,
} from "./types";

interface UseCollectCardActionsParams {
  siteList: FilmSource[];
  setSiteList: React.Dispatch<React.SetStateAction<FilmSource[]>>;
  updateSiteListItem: (id: string, updater: (record: FilmSource) => FilmSource) => void;
  getCollectList: (createdId?: string) => Promise<void>;
  stoppingSiteIdsRef: React.MutableRefObject<Set<string>>;
  onDeleteSubmitted?: (sources: { id: string; name: string }[]) => void;
}

export function useCollectCardActions({
  siteList,
  setSiteList,
  updateSiteListItem,
  getCollectList,
  stoppingSiteIdsRef,
  onDeleteSubmitted,
}: UseCollectCardActionsParams) {
  const { message } = useAppMessage();
  const [togglingStateSourceId, setTogglingStateSourceId] = useState<string | null>(null);
  const [deletingSourceId, setDeletingSourceId] = useState("");

  const changeSourceState = useCallback(
    async (id: string, nextState: boolean) => {
      setTogglingStateSourceId(id);
      updateSiteListItem(id, (item) => ({ ...item, state: nextState }));
      try {
        const resp = await ApiPost("/manage/collect/state", { ids: [id], state: nextState });
        if (resp.code === 0) {
          message.success(nextState ? "采集站已启用" : "采集站已禁用");
          await getCollectList();
        } else {
          message.error(resp.msg || "状态切换失败");
          updateSiteListItem(id, (item) => ({ ...item, state: !nextState }));
        }
      } catch {
        message.error("状态切换失败");
        updateSiteListItem(id, (item) => ({ ...item, state: !nextState }));
      } finally {
        setTogglingStateSourceId(null);
      }
    },
    [getCollectList, message, updateSiteListItem],
  );

  const startTask = useCallback(
    async (record: FilmSource) => {
      if (deletingSourceId === record.id) {
        return;
      }
      if (!record.state) {
        message.warning("采集站已停用，请先启用后再采集");
        return;
      }
      if (record.categoryReady === false) {
        message.warning("缺分类");
        return;
      }
      if (record.categorySyncing) {
        message.warning("分类采集中");
        return;
      }
      if (record.progress?.status === "stopping") {
        message.warning("停止中");
        return;
      }
      if (isActiveCollectStatus(record.progress?.status)) {
        message.warning("采集站已在采集中");
        return;
      }
      updateSiteListItem(record.id, (item) => ({
        ...item,
        progress: makeStartingProgress(
          record.id,
          record.name,
          newClientCollectQueueId(),
          item.progress?.queueId ?? "",
        ),
      }));
      const resp = await ApiPost("/manage/spider/start", {
        ids: [record.id],
        time: record.cd ?? 24,
      });
      if (resp.code === 0) {
        message.success(resp.msg);
        return;
      }
      message.error(resp.msg || "启动采集失败");
      updateSiteListItem(record.id, (item) => ({ ...item, progress: null }));
      await getCollectList();
    },
    [deletingSourceId, getCollectList, message, updateSiteListItem],
  );

  const stopTask = useCallback(
    async (id: string) => {
      stoppingSiteIdsRef.current.add(id);
      updateSiteListItem(id, (item) => ({
        ...item,
        progress: progressAsStopping(item.progress, item.id, item.name),
      }));
      message.loading({
        key: `collect-stop-site-${id}`,
        content: "正在停止...",
        duration: 0,
      });
      try {
        const resp = await ApiPost("/manage/spider/stop", { ids: [id] });
        if (resp.code !== 0) {
          stoppingSiteIdsRef.current.delete(id);
          message.error({
            key: `collect-stop-site-${id}`,
            content: resp.msg || "终止任务失败",
            duration: 2,
          });
          void getCollectList();
        }
      } catch {
        stoppingSiteIdsRef.current.delete(id);
        message.error({
          key: `collect-stop-site-${id}`,
          content: "终止任务失败",
          duration: 2,
        });
        void getCollectList();
      }
    },
    [getCollectList, message, stoppingSiteIdsRef, updateSiteListItem],
  );

  const delSource = useCallback(
    async (id: string) => {
      const src = siteList.find((s) => s.id === id);
      setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, deleting: true } : s)));
      setDeletingSourceId(id);
      try {
        const resp = await ApiPost("/manage/collect/delete", { ids: [id] });
        if (resp.code === 0) {
          onDeleteSubmitted?.([{ id, name: src?.name || id }]);
          message.success("已提交删除");
          return;
        }
        setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, deleting: false } : s)));
        message.error(resp.msg || "删除失败");
        void getCollectList();
      } catch {
        setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, deleting: false } : s)));
        message.error("删除失败");
        void getCollectList();
      } finally {
        setDeletingSourceId("");
      }
    },
    [getCollectList, message, onDeleteSubmitted, setSiteList, siteList],
  );

  return {
    togglingStateSourceId,
    deletingSourceId,
    changeSourceState,
    startTask,
    stopTask,
    delSource,
  };
}
