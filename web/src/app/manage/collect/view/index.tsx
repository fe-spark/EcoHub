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
import { useCollectSourceForm } from "./use-collect-source-form";
import {
  isActiveCollectStatus,
  type CollectListItemResponse,
  type CollectProgress,
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

  const { pendingSortRef, handleSortList, cleanupSort } = useCollectSort(siteList, setSiteList);

  const pendingCategoryStatusRef = useRef<Map<string, string>>(new Map());

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
    [message, pendingSortRef],
  );

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

  // SSE 实时分类状态同步
  const handleCategorySnapshot = useCallback((snapshot: Record<string, string>) => {
    if (!snapshot || typeof snapshot !== "object") return;
    setSiteList((current) =>
      current.map((item) => {
        const status = snapshot[item.id];
        if (!status) return item;
        return {
          ...item,
          categorySyncing: status === "syncing",
          categoryReady: status === "ready" ? true : status === "failed" ? false : item.categoryReady,
        };
      }),
    );
  }, []);

  const handleCategoryStatus = useCallback(
    (evt: { sourceId: string; status: string }) => {
      if (!evt || !evt.sourceId) return;
      pendingCategoryStatusRef.current.set(evt.sourceId, evt.status);
      setSiteList((current) => {
        const target = current.find((item) => item.id === evt.sourceId);
        if (target?.categorySyncing) {
          if (evt.status === "ready") {
            message.success("分类获取成功");
          } else if (evt.status === "failed") {
            message.error("分类获取失败");
          }
        }
        return current.map((item) =>
          item.id === evt.sourceId
            ? {
                ...item,
                categorySyncing: evt.status === "syncing",
                categoryReady:
                  evt.status === "ready" ? true : evt.status === "failed" ? false : item.categoryReady,
              }
            : item,
        );
      });
    },
    [message],
  );

  useCollectStream({
    onProgress: handleProgressStream,
    onCategorySnapshot: handleCategorySnapshot,
    onCategoryStatus: handleCategoryStatus,
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
      setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, categorySyncing: true } : s)));
      try {
        const resp = await ApiPost("/manage/collect/category/sync", { id });
        const ok = resp.code === 0;
        if (ok) {
          message.success(resp.msg || "获取分类成功");
        } else {
          message.error(resp.msg || "获取分类失败");
        }
        setSiteList((prev) =>
          prev.map((s) =>
            s.id === id ? { ...s, categorySyncing: false, categoryReady: ok ? true : false } : s,
          ),
        );
      } catch {
        message.error("获取分类失败");
        setSiteList((prev) => prev.map((s) => (s.id === id ? { ...s, categorySyncing: false } : s)));
      }
    },
    [message],
  );

  const startTask = async (record: FilmSource) => {
    if (!record.state) {
      message.warning("已禁用");
      return;
    }
    if (record.categoryReady === false) {
      message.warning("不可采集");
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
