"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Empty, Space } from "antd";
import { PlusOutlined, ApiOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission, ManagePermissionProvider } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectDrawer from "./batch-collect-drawer";
import CategoryChangeModal from "./category-change-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectQueueBars from "./collect-queue-bars";
import DeleteQueueBar from "./delete-queue-bar";
import CollectSourceGrid from "./collect-source-grid";
import CollectToolbar from "./collect-toolbar";
import SourceFormDrawer from "./source-form-drawer";
import { useCollectProgressStream } from "./use-collect-progress-stream";
import { useCollectStream, type CategoryTreeChange, type SearchIndexProgress } from "./use-collect-stream";
import { useCollectSourceEvents } from "./use-collect-source-events";
import { useCategoryChangeConfirm } from "./use-category-change-confirm";
import { useCollectCleanup } from "./use-collect-cleanup";
import { useCollectBatchActions } from "./use-collect-batch-actions";
import { useCollectBatchRunner } from "./use-collect-batch-runner";
import { useCollectQueueBars } from "./use-collect-queue-bars";
import { useCollectDeleteQueue } from "./use-collect-delete-queue";
import { useCollectSort } from "./use-collect-sort";
import { useCollectSourceForm } from "./use-collect-source-form";
import { useCollectCardActions } from "./use-collect-card-actions";
import { isTerminalCollectStatus, newClientCollectQueueId } from "./collect-queue";
import {
  isActiveCollectStatus,
  type CollectListItemResponse,
  type FilmSource,
  normalizeSource,
} from "./types";
import styles from "./index.module.less";

export default function CollectManagePageView() {
  const router = useRouter();
  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const canOperate = canWrite;
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [loading, setLoading] = useState(false);
  const mountedRef = useRef(false);
  const [globalSpiderProxyReady, setGlobalSpiderProxyReady] = useState(false);
  const [searchIndexProgress, setSearchIndexProgress] = useState<SearchIndexProgress>();
  const searchIndexRef = useRef(searchIndexProgress);
  searchIndexRef.current = searchIndexProgress;

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
          if (stoppingSiteIdsRef.current.size > 0) {
            for (const id of stoppingSiteIdsRef.current) {
              const item = rawData.find((s: CollectListItemResponse) => s.id === id);
              if (!item || !item.progress || isTerminalCollectStatus(item.progress.status)) {
                stoppingSiteIdsRef.current.delete(id);
                message.info({ key: `collect-stop-site-${id}`, content: "采集已停止", duration: 2 });
              }
            }
          }
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
  const stoppingSiteIdsRef = useRef<Set<string>>(new Set());

  const handleTerminalStatus = useCallback(
    (id: string, status: string) => {
      if (stoppingSiteIdsRef.current.has(id)) {
        stoppingSiteIdsRef.current.delete(id);
        const text = status === "stopped" ? "采集已停止" : status === "failed" ? "采集失败" : "采集完成";
        message.info({
          key: `collect-stop-site-${id}`,
          content: text,
          duration: 2,
        });
      }
    },
    [message],
  );

  const handleProgressStream = useCollectProgressStream(
    setSiteList,
    resetTimersRef,
    handleTerminalStatus,
    searchIndexRef,
  );

  const {
    deleteQueueItems,
    enqueueDeletingSources,
    handleSourceDeleteEvent,
    handleDeleteSnapshot: handleDeleteSnapshotQueue,
    hydrateSourceNames: hydrateDeleteQueueNames,
    dismissDeleteQueue,
  } = useCollectDeleteQueue();

  useEffect(() => {
    hydrateDeleteQueueNames(siteList);
  }, [siteList, hydrateDeleteQueueNames]);

  useCollectStream({
    onProgress: handleProgressStream,
    onCategorySnapshot: handleCategorySnapshot,
    onCategoryStatus: handleCategoryStatus,
    onSearchIndexProgress: (p) => {
      setSearchIndexProgress(p);
    },
    onSourceDelete: (evt) => {
      handleSourceDelete(evt);
      handleSourceDeleteEvent(evt);
    },
    onDeleteSnapshot: (ids) => {
      handleDeleteSnapshot(ids);
      setSiteList((current) => {
        handleDeleteSnapshotQueue(ids, current);
        return current;
      });
    },
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
  } = useCollectCleanup(getCollectList, setSiteList, enqueueDeletingSources);

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
  } = useCollectBatchActions(siteList, setSiteList, getCollectList, enqueueDeletingSources);

  const {
    batchOpen,
    setBatchOpen,
    batchTime,
    setBatchTime,
    batchRows,
    openBatchCollect,
    startBatchCollect,
  } = useCollectBatchRunner(siteList, setSiteList, selectedSourceIds, getCollectList, clearSelection);

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

  const { queueBars, stoppingQueueId, stopQueue } = useCollectQueueBars(siteList, setSiteList, getCollectList, searchIndexProgress);

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
    togglingStateSourceId,
    deletingSourceId,
    changeSourceState,
    startTask,
    stopTask,
    delSource,
  } = useCollectCardActions({
    siteList,
    setSiteList,
    updateSiteListItem,
    getCollectList,
    stoppingSiteIdsRef,
    onDeleteSubmitted: enqueueDeletingSources,
  });

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
          sitePageTotal?: number;
        }>("/manage/collect/category/sync", { id });
        const ok = resp.code === 0;
        const pageTotal = resp.data?.sitePageTotal;

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
            s.id === id
              ? {
                  ...s,
                  categorySyncing: false,
                  categoryReady: ok ? true : false,
                  sitePageTotal:
                    pageTotal && pageTotal > 0 ? pageTotal : s.sitePageTotal,
                }
              : s,
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

  return (
    <ManagePermissionProvider canWrite={canOperate} isAdmin={isAdmin}>
      <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集中心"
        description={<>统一管理采集站与采集任务<span className={styles.headerMeta}>· {siteList.length} 个</span></>}
        actions={
          <Space size={8}>
            {isAdmin && (
              <Button icon={<ApiOutlined />} onClick={() => router.push("/manage/system/proxy")}>网络代理</Button>
            )}
            <Button danger loading={cleanupScanning} disabled={!canOperate || siteList.length === 0} onClick={() => void startCleanupScan()}>清理失效源</Button>
          </Space>
        }
      />

      <div className={styles.cardPanel}>
        <CollectToolbar
          totalCount={siteList.length}
          selectedCount={selectedSourceIds.length}
          canWrite={canOperate}
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

        {deleteQueueItems.length > 0 ? (
          <DeleteQueueBar
            items={deleteQueueItems}
            onDismiss={dismissDeleteQueue}
          />
        ) : null}

        {queueBars.length > 0 ? (
          <CollectQueueBars
            items={queueBars}
            stoppingQueueId={stoppingQueueId}
            canWrite={canOperate}
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
              canWrite={canOperate}
              canAddSource={canOperate}
              globalSpiderProxyReady={globalSpiderProxyReady}
              onSelect={handleSelectSource}
              onChangeCollectDuration={changeCollectDuration}
              onChangeSourceState={changeSourceState}
              togglingSourceId={togglingStateSourceId}
              onStartTask={(record) => void startTask(record)}
              onTerminateTask={(id) => void stopTask(id)}
              onEditSource={(id) => void openEditDialog(id)}
              onDeleteSource={(id) => void delSource(id)}
              deletingSourceId={deletingSourceId}
              onOpenAddDialog={openAddDialog}
              onSortList={handleSortList}
              onSyncCategory={handleSyncCategory}
              onUpdateSite={updateSiteListItem}
            />
          </div>
        ) : (
          <div className={styles.emptyCard}>
            <Empty description={loading ? "采集站加载中…" : "暂无采集站"}>
              {!loading && canOperate ? (
                <Button type="primary" icon={<PlusOutlined />} onClick={openAddDialog}>新增采集站</Button>
              ) : null}
            </Empty>
          </div>
        )}
      </div>

      <SourceFormDrawer
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

      <BatchCollectDrawer
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
    </ManagePermissionProvider>
  );
}
