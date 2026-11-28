"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert, Button } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { isFormValuesEqual } from "@/lib/deep-equal";

export interface UseSiteConfigCardOptions<T> {
  loadUrl: string;
  saveUrl: string;
  defaultValue: T;
  normalize: (raw?: Partial<T> | null) => T;
  loadErrorText: string;
  saveErrorText: string;
  canWrite: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}

/**
 * 统一封装网站配置子卡片的基线加载、加载失败锁死防覆写、草稿脏检查与异常安全保存。
 */
export function useSiteConfigCard<T>({
  loadUrl,
  saveUrl,
  defaultValue,
  normalize,
  loadErrorText,
  saveErrorText,
  canWrite,
  onDirtyChange,
}: UseSiteConfigCardOptions<T>) {
  const [data, setData] = useState<T>(defaultValue);
  const [draft, setDraft] = useState<T>(defaultValue);
  const [isTouched, setIsTouched] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [loadError, setLoadError] = useState(false);

  const { message } = useAppMessage();
  const normalizeRef = useRef(normalize);
  normalizeRef.current = normalize;

  const loadData = useCallback(async () => {
    setFetching(true);
    setLoadError(false);
    try {
      const resp = await ApiGet<Partial<T>>(loadUrl);
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeRef.current(resp.data);
        setData(normalized);
        setDraft(normalized);
        setIsTouched(false);
        return;
      }
      setLoadError(true);
      message.error(resp.msg || loadErrorText);
    } catch {
      setLoadError(true);
    } finally {
      setFetching(false);
    }
  }, [loadUrl, loadErrorText, message]);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const hasDirty = useMemo(() => {
    if (loadError) return false;
    return isTouched && !isFormValuesEqual(draft, data);
  }, [loadError, isTouched, draft, data]);

  useEffect(() => {
    onDirtyChange?.(hasDirty);
  }, [hasDirty, onDirtyChange]);

  const save = useCallback(
    async (preparePayload?: (cur: T) => T): Promise<boolean> => {
      if (!canWrite || loadError) return false;
      const normalized = preparePayload
        ? preparePayload(draft)
        : normalizeRef.current(draft);
      try {
        const resp = await ApiPost(saveUrl, normalized);
        if (resp.code === 0) {
          setData(normalized);
          setDraft(normalized);
          setIsTouched(false);
          return true;
        }
        message.error(resp.msg || saveErrorText);
        return false;
      } catch {
        message.error(saveErrorText);
        return false;
      }
    },
    [canWrite, loadError, draft, saveUrl, saveErrorText, message],
  );

  const discard = useCallback(() => {
    setDraft(data);
    setIsTouched(false);
  }, [data]);

  return {
    data,
    draft,
    setDraft,
    isTouched,
    setIsTouched,
    fetching,
    loadError,
    reload: loadData,
    hasDirty,
    save,
    discard,
    editable: canWrite && !loadError,
    message,
  };
}

export function ConfigLoadErrorAlert({
  visible,
  text,
  loading,
  onRetry,
}: {
  visible: boolean;
  text: string;
  loading: boolean;
  onRetry: () => void;
}) {
  if (!visible) return null;
  return (
    <Alert
      type="error"
      showIcon
      message={text}
      description="为避免用默认值误覆盖线上配置，加载成功前已锁定编辑，请重试。"
      action={
        <Button size="small" loading={loading} onClick={onRetry}>
          重试
        </Button>
      }
      style={{ marginBottom: 16 }}
    />
  );
}
