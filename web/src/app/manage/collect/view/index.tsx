"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Empty, Space } from "antd";
import { PlusOutlined, ApiOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission, ManagePermissionProvider } from "@/lib/manage-permission";
import { usePrimarySource } from "@/lib/manage-primary";
import ManagePageHeader from "@/app/manage/components/page-header";
import CollectModals from "./collect-modals";
import CollectQueueBars from "./collect-queue-bars";
import DeleteQueueBar from "./delete-queue-bar";
import CollectSourceGrid from "./collect-source-grid";
import CollectToolbar from "./collect-toolbar";
import SourceFormDrawer from "./source-form-drawer";
import { useCollectProgressStream } from "./use-collect-progress-stream";
import { useCollectStream, type CategoryTreeChange, type SearchIndexProgress } from "./use-collect-stream";
import { useCollectSourceEvents } from "./use-collect-source-events";
import { useCategoryChangeConfirm } from "./use-category-change-confirm";
import { usePrimaryHandover } from "./use-primary-handover";
import { useCollectCleanup } from "./use-collect-cleanup";
import { useCollectBatchActions } from "./use-collect-batch-actions";
import { useCollectBatchRunner } from "./use-collect-batch-runner";
import { useCollectQueueBars } from "./use-collect-queue-bars";
import { useCollectDeleteQueue } from "./use-collect-delete-queue";
import { useCollectSort } from "./use-collect-sort";
import { useCollectSourceForm } from "./use-collect-source-form";
import { useCollectCardActions } from "./use-collect-card-actions";
import {
  isCollectBusyStatus,
  type CollectListItemResponse,
  type CollectProgress,
  type FilmSource,
  normalizeSource,
} from "./types";
import styles from "./index.module.less";

export default function CollectManagePageView() {
  const router = useRouter();
  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const { primary, loading: primaryLoading, refresh: refreshPrimary } = usePrimarySource();
  const canOperate = canWrite;
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const siteListRef = useRef<FilmSource[]>([]);
  siteListRef.current = siteList;
  const prevPrimaryIdRef = useRef<string | undefined>(undefined);

  useEffect(() => {
    if (primaryLoading) return;
    const nextPrimaryId = primary?.id ?? "";
    if (prevPrimaryIdRef.current === undefined) {
      prevPrimaryIdRef.current = nextPrimaryId;
      return;
    }
    if (prevPrimaryIdRef.current === nextPrimaryId) return;
    prevPrimaryIdRef.current = nextPrimaryId;
    setSiteList((current) => {
      let changed = false;
      const next = current.map((item) => {
        if (item.deleting && !nextPrimaryId) return item;
        const shouldBePrimary = Boolean(nextPrimaryId && item.id === nextPrimaryId);
        if (item.isPrimary === shouldBePrimary) return item;
        changed = true;
        return { ...item, isPrimary: shouldBePrimary };
      });
      return changed ? next : current;
    });
  }, [primary?.id, primaryLoading]);
  const [loading, setLoading] = useState(false);
  const mountedRef = useRef(false);
  const collectListRequestRef = useRef(0);
  const [globalSpiderProxyReady, setGlobalSpiderProxyReady] = useState(false);
  const [searchIndexProgress, setSearchIndexProgress] = useState<SearchIndexProgress>();

  const { pendingSortRef, handleSortList, cleanupSort } = useCollectSort(siteList, setSiteList);
  const {
    pendingCategoryStatusRef,
    syncingSourcesRef,
    handleCategorySnapshot,
    handleCategoryStatus,
    handleSourceDelete,
    handleDeleteSnapshot,
  } = useCollectSourceEvents({ setSiteList, message, reloadList: () => void getCollectList() });

  const getCollectList = useCallback(
    async (createdId?: string) => {
      const requestId = ++collectListRequestRef.current;
      setLoading(true);
      try {
        const resp = await ApiGet("/manage/collect/list");
        if (!mountedRef.current || requestId !== collectListRequestRef.current) return;
        if (resp.code === 0) {
          const rawData = Array.isArray(resp.data) ? resp.data : [];
          setSiteList((current) => {
            const cdMap = new Map(current.map((item) => [item.id, item.cd]));
            const progressMap = new Map(current.map((item) => [item.id, item.progress ?? null]));
            const normalizedList = rawData.map((item: CollectListItemResponse) => {
              const normalized = normalizeSource(item);
              if (progressFromStreamRef.current) {
                // 进度流是唯一数据源；列表里的进度可能比 SSE 旧。
                normalized.progress = progressMap.get(item.id) ?? null;
              }
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
        if (mountedRef.current && requestId === collectListRequestRef.current) setLoading(false);
      }
    },
    [message, pendingCategoryStatusRef, pendingSortRef],
  );

  const progressFromStreamRef = useRef(false);
  const progressStatusRef = useRef(new Map<string, string>());

  const applyProgressSnapshot = useCollectProgressStream(setSiteList);
  const handleProgressStream = useCallback(
    (list: CollectProgress[]) => {
      progressFromStreamRef.current = true;
      const live = new Set<string>();
      let settled = false;
      for (const item of list) {
        const id = String(item.id);
        live.add(id);
        const prev = progressStatusRef.current.get(id);
        if (
          (item.status === "done" || item.status === "failed" || item.status === "stopped") &&
          prev !== item.status
        ) {
          settled = true;
        }
        progressStatusRef.current.set(id, item.status);
      }
      for (const id of progressStatusRef.current.keys()) {
        if (!live.has(id)) progressStatusRef.current.delete(id);
      }
      applyProgressSnapshot(list);
      if (settled) void getCollectList();
    },
    [applyProgressSnapshot, getCollectList],
  );

  const {
    deleteQueueItems,
    stoppingDelete,
    stopDeleteQueue,
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
      if (
        (evt.status === "deleted" || evt.status === "canceled" || evt.status === "failed") &&
        siteListRef.current.some((s) => s.id === evt.sourceId && s.isPrimary)
      ) {
        void refreshPrimary();
      }
      handleSourceDelete(evt);
      handleSourceDeleteEvent(evt);
    },
    onDeleteSnapshot: (ids) => {
      handleDeleteSnapshot(ids);
      handleDeleteSnapshotQueue(ids, siteListRef.current);
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

  const primaryHandover = usePrimaryHandover();

  const {
    selectedSourceIds,
    batchStateUpdating,
    batchDeleting,
    hasBusySelected,
    deleteConfirmTitle,
    deleteConfirmDescription,
    handleSelectSource,
    selectAllSources,
    invertSelection,
    clearSelection,
    batchChangeSourceState,
    batchDeleteSources,
  } = useCollectBatchActions(
    siteList, setSiteList, getCollectList, enqueueDeletingSources, primaryHandover.ask, isAdmin, refreshPrimary,
  );

  const {
    batchOpen,
    setBatchOpen,
    batchTime,
    setBatchTime,
    batchRows,
    openBatchCollect,
    startBatchCollect,
  } = useCollectBatchRunner(
    siteList,
    selectedSourceIds,
    clearSelection,
    searchIndexProgress,
  );

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
    void getCollectList();
    void fetchProxyStatus();
    return () => {
      mountedRef.current = false;
      cleanupSort();
    };
  }, [cleanupSort, fetchProxyStatus, getCollectList]);

  const activeCollectIds = useMemo(
    () => siteList.filter((item) => isCollectBusyStatus(item.progress?.status)).map((item) => item.id),
    [siteList],
  );

  const { queueBars, sealBar, stoppingQueueId, stopQueue } = useCollectQueueBars(
    siteList,
    searchIndexProgress,
  );

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
    onDeleteSubmitted: enqueueDeletingSources,
    searchIndex: searchIndexProgress,
    isAdmin,
    refreshPrimary,
    askPrimaryHandover: primaryHandover.ask,
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
        description={<>管理采集站，开始或停止采集<span className={styles.headerMeta}>· {siteList.length} 个</span></>}
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
          hasBusySelected={hasBusySelected}
          isIndexing={searchIndexProgress?.status === "indexing"}
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
            onStop={() => void stopDeleteQueue()}
            stopping={stoppingDelete}
          />
        ) : null}

        {queueBars.length > 0 || sealBar ? (
          <CollectQueueBars
            items={queueBars}
            sealBar={sealBar}
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
              searchIndex={searchIndexProgress}
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

      <CollectModals
        batchOpen={batchOpen} batchRows={batchRows} batchTime={batchTime}
        isIndexing={searchIndexProgress?.status === "indexing"}
        onCancelBatch={() => setBatchOpen(false)} onSubmitBatch={() => void startBatchCollect()}
        onBatchTimeChange={setBatchTime}
        cleanupOpen={cleanupOpen} cleanupScanning={cleanupScanning} cleanupDeleting={cleanupDeleting}
        invalidSources={invalidSources} cleanupSkipped={cleanupSkipped}
        onCancelCleanup={cancelCleanup} onConfirmCleanup={() => void confirmCleanup()}
        categoryConfirm={categoryConfirm} primaryHandoverNode={primaryHandover.modalNode}
      />
      </div>
    </ManagePermissionProvider>
  );
}
