"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Empty, Space } from "antd";
import { PlusOutlined, ApiOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectModal from "./batch-collect-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectSourceGrid from "./collect-source-grid";
import CollectToolbar from "./collect-toolbar";
import SourceFormModal from "./source-form-modal";
import { useCollectStream } from "./use-collect-stream";
import { useCollectCleanup } from "./use-collect-cleanup";
import { useCollectBatchActions } from "./use-collect-batch-actions";
import { useCollectBatchRunner } from "./use-collect-batch-runner";
import { useCollectSort } from "./use-collect-sort";
import {
  isActiveCollectStatus,
  COLLECT_SOURCE_WARN_COUNT,
  type CollectListItemResponse,
  type CollectProgress,
  type FilmSource,
  makeStartingProgress,
  normalizeSource,
  SOURCE_FORM_DEFAULTS,
  type SourceFormValues,
} from "./types";
import styles from "./index.module.less";

export default function CollectManagePageView() {
  const router = useRouter();
  const { message, modal } = useAppMessage();
  const { canWrite } = useManagePermission();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [loading, setLoading] = useState(false);
  const mountedRef = useRef(false);

  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [sourceInitialValues, setSourceInitialValues] = useState<SourceFormValues>(SOURCE_FORM_DEFAULTS);
  const [sourceFormNonce, setSourceFormNonce] = useState(0);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [globalSpiderProxyReady, setGlobalSpiderProxyReady] = useState(false);

  const { pendingSortRef, handleSortList, cleanupSort } = useCollectSort(siteList, setSiteList);

  const getCollectList = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/collect/list");
      if (!mountedRef.current) return;
      if (resp.code === 0) {
        const rawData = Array.isArray(resp.data) ? resp.data : [];
        setSiteList((current) => {
          const cdMap = new Map(current.map((item) => [item.id, item.cd]));
          const normalizedList = rawData.map((item: CollectListItemResponse) => {
            const normalized = normalizeSource(item);
            if (cdMap.has(item.id) && cdMap.get(item.id)) {
              normalized.cd = cdMap.get(item.id);
            }
            return normalized;
          });
          const pending = pendingSortRef.current;
          if (!pending) return normalizedList;
          const freshMap = new Map(normalizedList.map((item) => [item.id, item]));
          return current.map((item) => {
            const fresh = freshMap.get(item.id);
            if (!fresh) return item;
            return { ...fresh, isPrimary: item.isPrimary };
          });
        });
      } else {
        message.error(resp.msg || "采集站列表加载失败");
      }
    } catch {
      // 拦截器已处理
    } finally {
      if (mountedRef.current) setLoading(false);
    }
  }, [message, pendingSortRef]);

  const resetTimersRef = useRef<Map<string, NodeJS.Timeout>>(new Map());

  // SSE 实时进度分发
  const handleProgressStream = useCallback((progressList: CollectProgress[]) => {
    if (!Array.isArray(progressList)) return;
    const progressMap = new Map(progressList.map((p) => [String(p.id), p]));

    for (const p of progressList) {
      const id = String(p.id);
      if (p.status === "done" || p.status === "stopped" || p.status === "failed") {
        if (!resetTimersRef.current.has(id)) {
          const timer = setTimeout(() => {
            resetTimersRef.current.delete(id);
            setSiteList((current) =>
              current.map((item) =>
                item.id === id &&
                item.progress &&
                (item.progress.status === "done" ||
                  item.progress.status === "stopped" ||
                  item.progress.status === "failed")
                  ? { ...item, progress: null }
                  : item,
              ),
            );
          }, 3000);
          resetTimersRef.current.set(id, timer);
        }
      } else {
        const existingTimer = resetTimersRef.current.get(id);
        if (existingTimer) {
          clearTimeout(existingTimer);
          resetTimersRef.current.delete(id);
        }
      }
    }

    setSiteList((current) =>
      current.map((item) => {
        const p = progressMap.get(String(item.id));
        if (p) {
          return { ...item, progress: p };
        }
        if (
          item.progress &&
          (item.progress.status === "done" ||
            item.progress.status === "stopped" ||
            item.progress.status === "failed")
        ) {
          return { ...item, progress: null };
        }
        return item;
      }),
    );
  }, []);

  useCollectStream(handleProgressStream);

  const {
    cleanupOpen,
    cleanupScanning,
    cleanupDeleting,
    invalidSources,
    cleanupSkipped,
    startCleanupScan,
    cancelCleanup,
    confirmCleanup,
  } = useCollectCleanup(getCollectList);

  const {
    selectedSourceIds,
    batchStateUpdating,
    batchDeleting,
    handleSelectSource,
    selectAllSources,
    invertSelection,
    clearSelection,
    batchChangeSourceState,
    batchDeleteSources,
  } = useCollectBatchActions(siteList, getCollectList);

  const {
    batchOpen,
    setBatchOpen,
    batchIds,
    batchTime,
    setBatchTime,
    batchOptions,
    openBatchCollect,
    startBatchCollect,
  } = useCollectBatchRunner(siteList, setSiteList, selectedSourceIds, getCollectList);

  const fetchProxyStatus = useCallback(async () => {
    try {
      const resp = await ApiGet("/manage/proxy/config");
      if (resp.code === 0 && resp.data) {
        const ready = Boolean(
          resp.data.enabled && String(resp.data.proxyUrl || "").trim() && resp.data.modules?.spider,
        );
        setGlobalSpiderProxyReady(ready);
      } else {
        setGlobalSpiderProxyReady(false);
      }
    } catch {
      setGlobalSpiderProxyReady(false);
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    const timers = resetTimersRef.current;
    void getCollectList();
    void fetchProxyStatus();
    return () => {
      mountedRef.current = false;
      cleanupSort();
      for (const timer of timers.values()) {
        clearTimeout(timer);
      }
      timers.clear();
    };
  }, [cleanupSort, fetchProxyStatus, getCollectList]);

  const activeCollectIds = useMemo(
    () => siteList.filter((item) => isActiveCollectStatus(item.progress?.status)).map((item) => item.id),
    [siteList],
  );

  const tourProgressPhase = useMemo(() => {
    if (activeCollectIds.length > 0) return "running";
    const statuses = siteList.map((item) => item.progress?.status).filter(Boolean);
    if (statuses.length === 0) return undefined;
    if (statuses.includes("failed")) return "failed";
    if (statuses.includes("stopped")) return "stopped";
    return "done";
  }, [activeCollectIds.length, siteList]);

  const updateSiteListItem = useCallback((id: string, updater: (record: FilmSource) => FilmSource) => {
    setSiteList((current) => current.map((item) => (item.id === id ? updater(item) : item)));
  }, []);

  const changeCollectDuration = useCallback(
    (id: string, value: number) => {
      updateSiteListItem(id, (item) => ({ ...item, cd: value }));
    },
    [updateSiteListItem],
  );

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
        await getCollectList();
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
      message.loading({ key: "collect-test", content: shouldUseProxy ? "正在通过代理测试接口..." : "正在直接测试接口..." });
      const resp = await ApiPost("/manage/collect/test", { ...values, ...(editingId ? { id: editingId } : {}), useProxy: shouldUseProxy });
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

  const startTask = async (record: FilmSource) => {
    if (!record.state) {
      message.warning("采集站已被禁用");
      return;
    }
    if (record.categoryReady === false) {
      message.warning("采集站缺少分类配置");
      return;
    }
    if (isActiveCollectStatus(record.progress?.status)) {
      message.warning("采集站已在采集中");
      return;
    }
    updateSiteListItem(record.id, (item) => ({ ...item, progress: makeStartingProgress(record.id, record.name) }));
    const resp = await ApiPost("/manage/spider/start", { id: record.id, time: record.cd ?? 24, batch: false });
    if (resp.code === 0) {
      message.success(resp.msg);
      return;
    }
    message.error(resp.msg || "启动采集失败");
    updateSiteListItem(record.id, (item) => ({ ...item, progress: null }));
    await getCollectList();
  };

  const stopTask = async (id: string) => {
    const resp = await ApiPost("/manage/spider/stop", { id });
    if (resp.code === 0) {
      message.success("已停止采集");
      return;
    }
    message.error(resp.msg || "终止任务失败");
  };

  const delSource = async (id: string) => {
    const resp = await ApiPost("/manage/collect/del", { id });
    if (resp.code === 0) {
      message.success(resp.msg);
      await getCollectList();
      return;
    }
    message.error(resp.msg || "删除采集站失败");
  };

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集中心"
        description={<>统一管理采集站与采集任务<span className={styles.headerMeta}>· {siteList.length} 个</span></>}
        actions={
          <Space size={8}>
            <Button icon={<ApiOutlined />} onClick={() => router.push("/manage/system?tab=proxy")}>网络代理</Button>
            <Button danger loading={cleanupScanning} disabled={!canWrite || siteList.length === 0} onClick={() => void startCleanupScan()}>清理失效源</Button>
          </Space>
        }
      />

      <div className={styles.cardPanel}>
        <CollectToolbar
          totalCount={siteList.length}
          selectedCount={selectedSourceIds.length}
          canWrite={canWrite}
          batchStateUpdating={batchStateUpdating}
          batchDeleting={batchDeleting}
          onSelectAll={selectAllSources}
          onInvertSelection={invertSelection}
          onClearSelection={clearSelection}
          onOpenBatchCollect={() => void openBatchCollect()}
          onBatchEnable={() => void batchChangeSourceState(true)}
          onBatchDisable={() => void batchChangeSourceState(false)}
          onBatchDelete={() => void batchDeleteSources()}
        />

        {siteList.length > 0 ? (
          <div className={styles.sourceGroups} data-tour={tourProgressPhase ? "collect-progress" : undefined} data-tour-progress={tourProgressPhase}>
            <CollectSourceGrid
              siteList={siteList}
              selectedSourceIds={selectedSourceIds.map(String)}
              activeCollectIds={activeCollectIds}
              hiddenDoneIds={[]}
              canWrite={canWrite}
              canAddSource={canWrite}
              globalSpiderProxyReady={globalSpiderProxyReady}
              onSelect={handleSelectSource}
              onChangeCollectDuration={changeCollectDuration}
              onStartTask={(record) => void startTask(record)}
              onTerminateTask={(id) => void stopTask(id)}
              onEditSource={(id) => void openEditDialog(id)}
              onDeleteSource={(id) => void delSource(id)}
              onOpenAddDialog={openAddDialog}
              onSortList={handleSortList}
            />
          </div>
        ) : (
          <div className={styles.emptyCard}>
            <Empty description={loading ? "采集站加载中…" : "暂无采集站"}>
              {!loading && canWrite ? (
                <Button type="primary" icon={<PlusOutlined />} onClick={openAddDialog}>新增采集站</Button>
              ) : null}
            </Empty>
          </div>
        )}
      </div>

      <SourceFormModal
        key={sourceFormNonce}
        open={sourceModalOpen}
        mode={sourceModalMode}
        loading={submitting}
        testing={testing}
        globalSpiderProxyReady={globalSpiderProxyReady}
        initialValues={sourceInitialValues}
        formNonce={sourceFormNonce}
        onCancel={() => setSourceModalOpen(false)}
        onSubmit={handleSubmitSource}
        onTest={testApi}
      />

      <BatchCollectModal
        open={batchOpen}
        options={batchOptions}
        selectedIds={batchIds}
        activeCollectIds={activeCollectIds}
        batchTime={batchTime}
        onCancel={() => setBatchOpen(false)}
        onSubmit={() => void startBatchCollect()}
        onBatchTimeChange={setBatchTime}
      />

      <CleanupInvalidModal
        open={cleanupOpen}
        scanning={cleanupScanning}
        deleting={cleanupDeleting}
        invalidSources={invalidSources}
        skipped={cleanupSkipped}
        onCancel={cancelCleanup}
        onConfirm={() => void confirmCleanup()}
      />
    </div>
  );
}
