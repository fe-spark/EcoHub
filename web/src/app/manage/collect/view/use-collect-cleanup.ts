import { useState, useRef, useCallback, type Dispatch, type SetStateAction } from "react";
import { ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import type { CheckAllResult, CleanupSkippedItem, DelBatchResult, FilmSource, InvalidSourceItem } from "./types";

export function useCollectCleanup(
  _reloadList: () => Promise<void>,
  setSiteList: Dispatch<SetStateAction<FilmSource[]>>,
  onDeleteSubmitted?: (sources: { id: string; name: string }[]) => void,
) {
  const { message } = useAppMessage();
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleanupScanning, setCleanupScanning] = useState(false);
  const [cleanupDeleting, setCleanupDeleting] = useState(false);
  const [invalidSources, setInvalidSources] = useState<InvalidSourceItem[]>([]);
  const [cleanupSkipped, setCleanupSkipped] = useState<CleanupSkippedItem[]>([]);
  const cleanupScanCanceledRef = useRef(false);

  const startCleanupScan = useCallback(async () => {
    cleanupScanCanceledRef.current = false;
    setCleanupScanning(true);
    setCleanupOpen(true);
    try {
      const resp = await ApiPostLong<CheckAllResult>("/manage/collect/check/all", {});
      if (resp.code === 0) {
        const data = resp.data ?? { checked: 0, ok: 0, failed: [], skipped: [] };
        const failed = Array.isArray(data.failed) ? data.failed : [];
        const skipped = Array.isArray(data.skipped) ? data.skipped : [];
        setInvalidSources(failed);
        setCleanupSkipped(skipped);
        if (failed.length === 0) {
          setCleanupOpen(false);
          const skipText =
            skipped.length > 0
              ? `，跳过 ${skipped.length} 个（${skipped
                  .map((item) => item.name || item.id)
                  .join("、")}）`
              : "";
          message.success(`接口正常：${data.checked ?? 0} 个${skipText}`);
          return;
        }
        if (!cleanupScanCanceledRef.current) {
          setCleanupOpen(true);
        }
        return;
      }
      setCleanupOpen(false);
      message.error(resp.msg || "失效源检测失败");
    } catch {
      setCleanupOpen(false);
    } finally {
      setCleanupScanning(false);
    }
  }, [message]);

  const cancelCleanup = useCallback(() => {
    cleanupScanCanceledRef.current = true;
    setCleanupOpen(false);
  }, []);

  const confirmCleanup = useCallback(async () => {
    if (invalidSources.length === 0) return;
    const targetSources = [...invalidSources];
    const targetIds = targetSources.map((item) => item.id);
    const targetIdSet = new Set(targetIds);

    // 1. 立即乐观将卡片置为“删除中”
    setSiteList((prev) =>
      prev.map((s) => (targetIdSet.has(s.id) ? { ...s, deleting: true } : s)),
    );

    setCleanupDeleting(true);
    try {
      const resp = await ApiPostLong<DelBatchResult>("/manage/collect/delete", {
        ids: targetIds,
      });
      if (resp.code === 0) {
        onDeleteSubmitted?.(
          targetSources.map((s) => ({ id: s.id, name: s.name || s.id })),
        );
        message.success(`已提交 ${targetIds.length} 个失效采集站后台级联删除任务`);
        setCleanupOpen(false);
        setInvalidSources([]);
        setCleanupSkipped([]);
        return;
      }
      setSiteList((prev) =>
        prev.map((s) => (targetIdSet.has(s.id) ? { ...s, deleting: false } : s)),
      );
      message.error(resp.msg || "清理失败");
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "清理失败";
      setSiteList((prev) =>
        prev.map((s) => (targetIdSet.has(s.id) ? { ...s, deleting: false } : s)),
      );
      message.error(msg);
    } finally {
      setCleanupDeleting(false);
    }
  }, [invalidSources, message, onDeleteSubmitted, setSiteList]);

  return {
    cleanupOpen,
    cleanupScanning,
    cleanupDeleting,
    invalidSources,
    cleanupSkipped,
    startCleanupScan,
    cancelCleanup,
    confirmCleanup,
  };
}
