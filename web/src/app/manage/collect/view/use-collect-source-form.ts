"use client";

import { useState } from "react";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import {
  COLLECT_SOURCE_WARN_COUNT,
  type FilmSource,
  SOURCE_FORM_DEFAULTS,
  type SourceFormValues,
} from "./types";

interface UseCollectSourceFormProps {
  siteList: FilmSource[];
  getCollectList: (createdId?: string) => Promise<void>;
  globalSpiderProxyReady: boolean;
  fetchProxyStatus: () => Promise<void>;
}

export function useCollectSourceForm({
  siteList,
  getCollectList,
  globalSpiderProxyReady,
  fetchProxyStatus,
}: UseCollectSourceFormProps) {
  const { message, modal } = useAppMessage();
  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [sourceInitialValues, setSourceInitialValues] =
    useState<SourceFormValues>(SOURCE_FORM_DEFAULTS);
  const [sourceFormNonce, setSourceFormNonce] = useState(0);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);

  const openAddForm = () => {
    void fetchProxyStatus();
    setSourceModalMode("add");
    setEditingId(null);
    setSourceInitialValues(SOURCE_FORM_DEFAULTS);
    setSourceFormNonce((n) => n + 1);
    setSourceModalOpen(true);
  };

  const openAddDialog = () => {
    if (siteList.length >= COLLECT_SOURCE_WARN_COUNT) {
      modal.confirm({
        title: "采集站过多",
        content: `已有 ${siteList.length} 个，继续添加？`,
        okText: "继续",
        cancelText: "取消",
        onOk: openAddForm,
      });
      return;
    }
    openAddForm();
  };

  const openEditDialog = async (id: string) => {
    void fetchProxyStatus();
    setSourceModalMode("edit");
    setEditingId(id);
    const resp = await ApiGet("/manage/collect/find", { id });
    if (resp.code === 0 && resp.data) {
      setSourceInitialValues({
        name: String(resp.data.name ?? ""),
        uri: String(resp.data.uri ?? ""),
        state: Boolean(resp.data.state),
        isPosterSource: Boolean(resp.data.isPosterSource),
        interval: Number(resp.data.interval ?? 0),
        cd: Number(resp.data.cd > 0 ? resp.data.cd : 24),
        format: (resp.data.format as "json" | "xml") || "json",
        domainReplaceRules: String(resp.data.domainReplaceRules ?? ""),
        proxyCollect: Boolean(resp.data.proxyCollect),
      });
      setSourceFormNonce((n) => n + 1);
      setSourceModalOpen(true);
      return;
    }
    message.error(resp.msg || "获取采集站信息失败");
  };

  const handleSubmitSource = async (values: SourceFormValues) => {
    setSubmitting(true);
    try {
      const resp = await ApiPost(
        sourceModalMode === "add" ? "/manage/collect/add" : "/manage/collect/update",
        sourceModalMode === "add" ? values : { ...values, id: editingId },
      );
      if (resp.code === 0) {
        message.success(resp.msg);
        setSourceModalOpen(false);
        const createdId = sourceModalMode === "add" ? (resp.data?.id as string | undefined) : undefined;
        await getCollectList(createdId);
        return;
      }
      message.error(resp.msg || "保存采集站失败");
    } finally {
      setSubmitting(false);
    }
  };

  const testApi = async (values: SourceFormValues) => {
    const shouldUseProxy = Boolean(globalSpiderProxyReady && values.proxyCollect);
    try {
      setTesting(true);
      message.loading({
        key: "collect-test",
        content: shouldUseProxy ? "正在通过代理测试接口..." : "正在直接测试接口...",
      });
      const resp = await ApiPost("/manage/collect/test", {
        ...values,
        ...(editingId ? { id: editingId } : {}),
        useProxy: shouldUseProxy,
      });
      if (resp.code === 0) {
        message.success({ key: "collect-test", content: resp.msg });
        return;
      }
      message.error({ key: "collect-test", content: resp.msg || "接口测试失败" });
    } catch {
      message.error({ key: "collect-test", content: "接口测试失败" });
    } finally {
      setTesting(false);
    }
  };

  return {
    sourceModalMode,
    sourceModalOpen,
    setSourceModalOpen,
    sourceInitialValues,
    sourceFormNonce,
    editingId,
    submitting,
    testing,
    openAddDialog,
    openEditDialog,
    handleSubmitSource,
    testApi,
  };
}
