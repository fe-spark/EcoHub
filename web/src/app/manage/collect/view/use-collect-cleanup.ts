import { useState, useRef, useCallback } from "react";
import { ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import type { CheckAllResult, CleanupSkippedItem, DelBatchResult, InvalidSourceItem } from "./types";

export function useCollectCleanup(reloadList: () => Promise<void>) {
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
    setCleanupDeleting(true);
    try {
      const resp = await ApiPost<DelBatchResult>("/manage/collect/del/batch", {
        ids: invalidSources.map((item) => item.id),
      });
      if (resp.code === 0) {
        const data = resp.data ?? { deleted: [], skipped: [] };
        const deleted = Array.isArray(data.deleted) ? data.deleted : [];
        const skipped = Array.isArray(data.skipped) ? data.skipped : [];
        message.success(`已删除 ${deleted.length} 个失效采集站`);
        if (skipped.length > 0) {
          message.warning(
            `${skipped.length} 个采集站未删除：${skipped
              .map((item) => `${item.name || item.id}（${item.reason}）`)
              .join("；")}`,
          );
        }
        setCleanupOpen(false);
        setInvalidSources([]);
        setCleanupSkipped([]);
        await reloadList();
        return;
      }
      message.error(resp.msg || "清理失败");
    } finally {
      setCleanupDeleting(false);
    }
  }, [invalidSources, message, reloadList]);

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
