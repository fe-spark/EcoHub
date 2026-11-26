import { useCallback, useEffect, useRef, useState } from "react";
import { usePrimarySource } from "@/lib/manage-primary";
import { useAppMessage } from "@/lib/useAppMessage";
import { shouldFollowPrimary } from "./banner-source-follow";
import { BannerSourceOption } from "./types";

interface UseHomeSourceOptions {
  activeDirty: boolean;
  activeBusy: boolean;
  onPrimaryFollow?: (hadPending: boolean) => void;
}

/** 首页管理统一采集源状态：放在 Tabs 外层共享，并响应全局主站变更。 */
export function useHomeSource({
  activeDirty,
  activeBusy,
  onPrimaryFollow,
}: UseHomeSourceOptions) {
  const { primary, loading: primaryLoading, refresh: refreshPrimary } = usePrimarySource();
  const primaryId = primary?.id || "";
  const { modal } = useAppMessage();

  const [sources, setSources] = useState<BannerSourceOption[]>([]);
  const [sourceId, setSourceId] = useState("");

  const sourceIdRef = useRef(sourceId);
  const previousPrimaryIdRef = useRef("");
  const initializedRef = useRef(false);
  const dirtyRef = useRef(activeDirty);
  const onPrimaryFollowRef = useRef(onPrimaryFollow);

  useEffect(() => {
    sourceIdRef.current = sourceId;
    dirtyRef.current = activeDirty;
    onPrimaryFollowRef.current = onPrimaryFollow;
  }, [activeDirty, onPrimaryFollow, sourceId]);

  const syncSources = useCallback(
    (nextSources: BannerSourceOption[] | undefined, resolvedId: string, nextPrimaryId?: string) => {
      if (Array.isArray(nextSources)) {
        setSources(nextSources);
      }
      if (!sourceIdRef.current && resolvedId) {
        sourceIdRef.current = resolvedId;
        setSourceId(resolvedId);
      }
      if (typeof nextPrimaryId === "string" && nextPrimaryId !== primaryId) {
        void refreshPrimary();
      }
    },
    [primaryId, refreshPrimary],
  );

  useEffect(() => {
    if (primaryLoading) return;
    if (!initializedRef.current) {
      initializedRef.current = true;
      previousPrimaryIdRef.current = primaryId;
      if (!sourceIdRef.current && primaryId) {
        sourceIdRef.current = primaryId;
        setSourceId(primaryId);
      }
      return;
    }
    const previousPrimaryId = previousPrimaryIdRef.current;
    if (previousPrimaryId === primaryId) return;
    previousPrimaryIdRef.current = primaryId;
    if (!shouldFollowPrimary(sourceIdRef.current, previousPrimaryId)) {
      return;
    }
    onPrimaryFollowRef.current?.(dirtyRef.current);
    sourceIdRef.current = primaryId;
    setSourceId(primaryId);
  }, [primaryId, primaryLoading]);

  const handleSourceChange = useCallback(
    (nextSourceId: string) => {
      if (!nextSourceId || nextSourceId === sourceIdRef.current || activeBusy) return;
      if (!dirtyRef.current) {
        sourceIdRef.current = nextSourceId;
        setSourceId(nextSourceId);
        return;
      }
      modal.confirm({
        title: "切换采集源？",
        content: "当前调整还没保存，继续后会丢掉这次修改。",
        okText: "切换",
        cancelText: "留下",
        onOk: () => {
          sourceIdRef.current = nextSourceId;
          setSourceId(nextSourceId);
        },
      });
    },
    [activeBusy, modal],
  );

  return {
    sources,
    sourceId,
    primaryId,
    primaryLoading,
    refreshPrimary,
    syncSources,
    handleSourceChange,
  };
}
