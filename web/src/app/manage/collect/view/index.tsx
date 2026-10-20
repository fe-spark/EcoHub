"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Empty, Space } from "antd";
import { PlusOutlined, ApiOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectModal from "./batch-collect-modal";
import CategoryChangeModal from "./category-change-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectQueueBars from "./collect-queue-bars";
import CollectSourceGrid from "./collect-source-grid";
import CollectToolbar from "./collect-toolbar";
import SourceFormModal from "./source-form-modal";
import { useCollectProgressStream } from "./use-collect-progress-stream";
import { useCollectStream, type CategoryTreeChange } from "./use-collect-stream";
import { useCollectSourceEvents } from "./use-collect-source-events";
import { useCategoryChangeConfirm } from "./use-category-change-confirm";
import { useCollectCleanup } from "./use-collect-cleanup";
import { useCollectBatchActions } from "./use-collect-batch-actions";
import { useCollectBatchRunner } from "./use-collect-batch-runner";
import { useCollectQueueBars } from "./use-collect-queue-bars";
import { useCollectSort } from "./use-collect-sort";
import { useCollectSourceForm } from "./use-collect-source-form";
import { newClientCollectQueueId } from "./collect-queue";
import {
  isActiveCollectStatus,
  progressAsStopping,
  type CollectListItemResponse,
  type FilmSource,
  makeStartingProgress,
  normalizeSource,
} from "./types";
import styles from "./index.module.less";

export default function CollectManagePageView() {
  const router = useRouter();
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [loading, setLoading] = useState(false);
  const mountedRef = useRef(false);
  const [globalSpiderProxyReady, setGlobalSpiderProxyReady] = useState(false);
  const [deletingSourceId, setDeletingSourceId] = useState("");

  const { pendingSortRef, handleSortList, cleanupSort } = useCollectSort(siteList, setSiteList);
  const {
    pendingCategoryStatusRef,
    syncingSourcesRef,
    handleCategorySnapshot,
    handleCategoryStatus,
    handleSourceDelete,
    handleDeleteSnapshot,
  } = useCollectSourceEvents({ setSiteList, message });

  const getCollectList = useCallback(
    async (createdId?: string) => {
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
              const pendingStatus = pendingCategoryStatusRef.current.get(item.id);
              if (pendingStatus) {
                normalized.categorySyncing = pendingStatus === "syncing";
                if (pendingStatus === "ready") normalized.categoryReady = true;
                if (pendingStatus === "failed") normalized.categoryReady = false;
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
          if (createdId) {
            setTimeout(() => {
              document.getElementById(`source-card-${createdId}`)?.scrollIntoView({
                behavior: "smooth",
                block: "center",
              });
            }, 100);
          }
        } else {
          message.error(resp.msg || "采集站列表加载失败");
        }
      } catch {
        // 拦截器已处理
      } finally {
        if (mountedRef.current) setLoading(false);
      }
    },
    [message, pendingCategoryStatusRef, pendingSortRef],
  );

  const resetTimersRef = useRef<Map<string, NodeJS.Timeout>>(new Map());
  const handleProgressStream = useCollectProgressStream(setSiteList, resetTimersRef);

  useCollectStream({
    onProgress: handleProgressStream,
    onCategorySnapshot: handleCategorySnapshot,
    onCategoryStatus: handleCategoryStatus,
    onSourceDelete: handleSourceDelete,
    onDeleteSnapshot: handleDeleteSnapshot,
    onReconnect: () => {
      void getCollectList();
    },
  });

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

  const categoryConfirm = useCategoryChangeConfirm(getCollectList);

  const {
    selectedSourceIds,
    batchStateUpdating,
    batchDeleting,
    deleteConfirmTitle,
    deleteConfirmDescription,
    handleSelectSource,
    selectAllSources,
    invertSelection,
    clearSelection,
    batchChangeSourceState,
    batchDeleteSources,
  } = useCollectBatchActions(siteList, setSiteList, getCollectList);

  const {
    batchOpen,
    setBatchOpen,
    batchTime,
    setBatchTime,
    batchRows,
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

  const { queueBars, stoppingQueueId, stopQueue } = useCollectQueueBars(siteList, setSiteList, getCollectList);

  const updateSiteListItem = useCallback((id: string, updater: (record: FilmSource) => FilmSource) => {
    setSiteList((current) => current.map((item) => (item.id === id ? updater(item) : item)));
  }, []);

  const changeCollectDuration = useCallback(
    (id: string, value: number) => {
      updateSiteListItem(id, (item) => ({ ...item, cd: value }));
    },
    [updateSiteListItem],
  );

  const {
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
  } = useCollectSourceForm({
    siteList,
    getCollectList,
    globalSpiderProxyReady,
    fetchProxyStatus,
  });

  const handleSyncCategory = useCallback(
    async (id: string) => {
      syncingSourcesRef.current.add(id);
      setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, categorySyncing: true } : s)));
      try {
        const resp = await ApiPost<{
          needConfirm?: boolean;
          name?: string;
          changes?: CategoryTreeChange[];
        }>("/manage/collect/category/sync", { id });
        const ok = resp.code === 0;

        if (resp.data?.needConfirm) {
          syncingSourcesRef.current.delete(id);
          const target = siteList.find((s) => s.id === id);
          categoryConfirm.openConfirm(id, resp.data.name || target?.name || id, resp.data.changes || []);
          setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, categorySyncing: false } : s)));
          return;
        }

        if (syncingSourcesRef.current.has(id)) {
          syncingSourcesRef.current.delete(id);
          if (ok) {
            message.success({ key: `category-sync-${id}`, content: resp.msg || "分类获取成功" });
          } else {
            message.error({ key: `category-sync-${id}`, content: resp.msg || "分类获取失败" });
          }
        }
        setSiteList((prev) =>
          prev.map((s) =>
            s.id === id ? { ...s, categorySyncing: false, categoryReady: ok ? true : false } : s,
          ),
        );
      } catch {
        syncingSourcesRef.current.delete(id);
        message.error({ key: `category-sync-${id}`, content: "分类获取失败" });
        setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, categorySyncing: false } : s)));
      }
    },
    [categoryConfirm, message, siteList, syncingSourcesRef],
  );

  const startTask = async (record: FilmSource) => {
    if (deletingSourceId === record.id) {
      return;
    }
    if (!record.state) {
      message.warning("已禁用");
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
      progress: makeStartingProgress(record.id, record.name, newClientCollectQueueId(), item.progress?.queueId ?? ""),
    }));
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
    updateSiteListItem(id, (item) => ({ ...item, progress: progressAsStopping(item.progress, item.id, item.name) }));
    const resp = await ApiPost("/manage/spider/stop", { id });
    if (resp.code === 0) {
      message.success("正在停止");
      return;
    }
    message.error(resp.msg || "终止任务失败");
    void getCollectList();
  };

  const delSource = async (id: string) => {
    setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, deleting: true } : s)));
    setDeletingSourceId(id);
    try {
      const resp = await ApiPost("/manage/collect/del", { id });
      if (resp.code === 0) {
        message.success("已提交删除");
        return;
      }
      message.error(resp.msg || "删除失败");
      void getCollectList();
    } catch {
      message.error("删除失败");
      void getCollectList();
    } finally {
      setDeletingSourceId("");
    }
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
          deleteConfirmTitle={deleteConfirmTitle}
          deleteConfirmDescription={deleteConfirmDescription}
          onSelectAll={selectAllSources}
          onInvertSelection={invertSelection}
          onClearSelection={clearSelection}
          onOpenBatchCollect={openBatchCollect}
          onBatchEnable={() => void batchChangeSourceState(true)}
          onBatchDisable={() => void batchChangeSourceState(false)}
          onBatchDelete={() => void batchDeleteSources()}
        />

        {queueBars.length > 0 ? (
          <CollectQueueBars
            items={queueBars}
            stoppingQueueId={stoppingQueueId}
            canWrite={canWrite}
            onStopQueue={(queueId, sourceIds) => void stopQueue(queueId, sourceIds)}
          />
        ) : null}

        {siteList.length > 0 ? (
          <div className={styles.sourceGroups}>
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
              deletingSourceId={deletingSourceId}
              onOpenAddDialog={openAddDialog}
              onSortList={handleSortList}
              onSyncCategory={handleSyncCategory}
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
        rows={batchRows}
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

      <CategoryChangeModal
        open={categoryConfirm.open}
        sourceName={categoryConfirm.sourceName}
        changes={categoryConfirm.changes}
        loading={categoryConfirm.loading}
        onCancel={categoryConfirm.closeConfirm}
        onKeep={() => void categoryConfirm.handleKeep()}
        onClear={() => void categoryConfirm.handleClear()}
      />
    </div>
  );
}
