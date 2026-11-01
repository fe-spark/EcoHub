import { useCallback, useState } from "react";
import { ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import type { CategoryTreeChange } from "./use-collect-stream";

export interface CategorySyncResponse {
  needConfirm?: boolean;
  id?: string;
  name?: string;
  changes?: CategoryTreeChange[];
  dataCleared?: boolean;
}

export function useCategoryChangeConfirm(onSuccess?: () => Promise<void> | void) {
  const { message } = useAppMessage();
  const [open, setOpen] = useState(false);
  const [sourceId, setSourceId] = useState("");
  const [sourceName, setSourceName] = useState("");
  const [changes, setChanges] = useState<CategoryTreeChange[]>([]);
  const [loading, setLoading] = useState(false);

  const openConfirm = useCallback((id: string, name: string, changeList: CategoryTreeChange[]) => {
    setSourceId(id);
    setSourceName(name);
    setChanges(changeList || []);
    setOpen(true);
  }, []);

  const closeConfirm = useCallback(() => {
    if (loading) return;
    setOpen(false);
    setSourceId("");
    setSourceName("");
    setChanges([]);
  }, [loading]);

  const handleConfirmAction = useCallback(
    async (action: "keep" | "clear") => {
      if (!sourceId) return;
      setLoading(true);
      try {
        const resp = await ApiPostLong<CategorySyncResponse>("/manage/collect/category/sync", {
          id: sourceId,
          confirm: action,
        });
        if (resp.code === 0) {
          message.success(resp.msg || "分类获取成功");
          setOpen(false);
          setSourceId("");
          setSourceName("");
          setChanges([]);
          await onSuccess?.();
          return;
        }
        message.error(resp.msg || "获取分类失败");
      } catch {
        message.error("获取分类失败");
      } finally {
        setLoading(false);
      }
    },
    [message, onSuccess, sourceId],
  );

  return {
    open,
    sourceId,
    sourceName,
    changes,
    loading,
    openConfirm,
    closeConfirm,
    handleKeep: () => handleConfirmAction("keep"),
    handleClear: () => handleConfirmAction("clear"),
  };
}
