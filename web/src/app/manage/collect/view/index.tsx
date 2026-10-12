"use client";

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Button,
  Card,
  Empty,
  Popconfirm,
  Space,
  Typography,
} from "antd";
import { PlusOutlined, ApiOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import { COLLECT_BATCH_FAILED_EVENT } from "@/app/manage/components/manage-tour";
import BatchCollectModal from "./batch-collect-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectSourceGrid from "./collect-source-grid";
import SourceFormModal from "./source-form-modal";
import {
  isActiveCollectStatus,
  COLLECT_SOURCE_WARN_COUNT,
  type BatchOption,
  type CheckAllResult,
  type CleanupSkippedItem,
  type CollectProgress,
  type DelBatchResult,
  type FilmSource,
  type InvalidSourceItem,
  SOURCE_FORM_DEFAULTS,
  type SourceFormValues,
} from "./types";
import styles from "./index.module.less";

interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
}

/** 启动瞬间本地进度：0%，避免等轮询才出现进度 */
function makeStartingProgress(id: string, name: string): CollectProgress {
  return {
    id,
    name,
    total: 0,
    current: 0,
    success: 0,
    failed: 0,
    status: "starting",
  };
}

const POLL_INTERVAL = 4000;
const MAX_POLL_FAILURES = 10;

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    state: Boolean(item.state),
    sort: Number(item.sort ?? 0),
    isPosterSource: Boolean(item.isPosterSource),
    isPrimary: Boolean(item.isPrimary),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd > 0 ? item.cd : 24),
    format: (item.format as "json" | "xml") || "json",
    lastCollectTime: item.lastCollectTime,
    progress: item.progress ?? null,
    proxyCollect: Boolean(item.proxyCollect),
    createdAt: item.createdAt,
  };
}

export default function CollectManagePageView() {
  const router = useRouter();
  const { message, modal } = useAppMessage();
  const { canWrite } = useManagePermission();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [selectedSourceIds, setSelectedSourceIds] = useState<React.Key[]>([]);
  const [batchStateUpdating, setBatchStateUpdating] = useState(false);
  const [batchDeleting, setBatchDeleting] = useState(false);
  const [loading, setLoading] = useState(false);
  const timerRef = useRef<NodeJS.Timeout | null>(null);
  const mountedRef = useRef(false);
  const pollFailuresRef = useRef(0);
  const requestRef = useRef<((silent?: boolean) => Promise<void>) | null>(null);
  const pendingSortRef = useRef<{ prev: FilmSource[]; next: FilmSource[] } | null>(null);
  const sortConfirmRef = useRef<{ destroy: () => void } | null>(null);

  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [sourceInitialValues, setSourceInitialValues] =
    useState<SourceFormValues>(SOURCE_FORM_DEFAULTS);
  const [sourceFormNonce, setSourceFormNonce] = useState(0);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);
  const [globalSpiderProxyReady, setGlobalSpiderProxyReady] = useState(false);

  const fetchProxyStatus = useCallback(async () => {
    try {
      const resp = await ApiGet("/manage/proxy/config");
      if (resp.code === 0 && resp.data) {
        const ready = Boolean(
          resp.data.enabled &&
            String(resp.data.proxyUrl || "").trim() &&
            resp.data.modules?.spider,
        );
        setGlobalSpiderProxyReady(ready);
      } else {
        setGlobalSpiderProxyReady(false);
      }
    } catch {
      setGlobalSpiderProxyReady(false);
    }
  }, []);

  const [batchOpen, setBatchOpen] = useState(false);
  const [batchIds, setBatchIds] = useState<string[]>([]);
  const [batchTime, setBatchTime] = useState(24);
  const [batchOptions, setBatchOptions] = useState<BatchOption[]>([]);

  // 失效源检测与清理
  const [cleanupOpen, setCleanupOpen] = useState(false);
  const [cleanupScanning, setCleanupScanning] = useState(false);
  const [cleanupDeleting, setCleanupDeleting] = useState(false);
  const [invalidSources, setInvalidSources] = useState<InvalidSourceItem[]>([]);
  const [cleanupSkipped, setCleanupSkipped] = useState<CleanupSkippedItem[]>([]);
  const cleanupScanCanceledRef = useRef(false);

  // 仅「仍在生命周期内」的任务禁用操作；done/failed 短暂展示进度但不锁按钮。
  const activeCollectIds = useMemo(
    () =>
      siteList
        .filter((item) => isActiveCollectStatus(item.progress?.status))
        .map((item) => item.id),
    [siteList],
  );

  const tourProgressPhase = useMemo(() => {
    if (activeCollectIds.length > 0) {
      return "running";
    }
    const statuses = siteList.map((item) => item.progress?.status).filter(Boolean);
    if (statuses.length === 0) {
      return undefined;
    }
    if (statuses.includes("failed")) {
      return "failed";
    }
    if (statuses.includes("stopped")) {
      return "stopped";
    }
    return "done";
  }, [activeCollectIds.length, siteList]);

  const canAddSource = canWrite;

  const clearPollTimer = useCallback(() => {
    if (timerRef.current) {
      clearTimeout(timerRef.current);
      timerRef.current = null;
    }
  }, []);

  const schedulePoll = useCallback(() => {
    if (!mountedRef.current) {
      return;
    }
    clearPollTimer();
    timerRef.current = setTimeout(() => {
      if (pollFailuresRef.current >= MAX_POLL_FAILURES) {
        return;
      }
      void requestRef.current?.(true);
    }, POLL_INTERVAL);
  }, [clearPollTimer]);

  const getCollectList = useCallback(async (silent = false) => {
    if (!silent) {
      setLoading(true);
    }
    try {
      const resp = await ApiGet("/manage/collect/list");
      if (!mountedRef.current) {
        return;
      }
      if (resp.code === 0) {
        pollFailuresRef.current = 0;
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
          if (!pending) {
            return normalizedList;
          }
          const freshMap = new Map(normalizedList.map((item) => [item.id, item]));
          return current.map((item) => {
            const fresh = freshMap.get(item.id);
            if (!fresh) {
              return item;
            }
            return { ...fresh, isPrimary: item.isPrimary };
          });
        });
        setSelectedSourceIds((current) =>
          current.filter((id) => rawData.some((item: CollectListItemResponse) => item.id === id)),
        );
      } else {
        pollFailuresRef.current += 1;
        message.error(resp.msg || "采集站列表加载失败");
      }
    } catch {
      pollFailuresRef.current += 1;
      // 拦截器已统一提示，避免重复弹窗
    } finally {
      if (!mountedRef.current) {
        return;
      }
      if (!silent) {
        setLoading(false);
      }
      if (pollFailuresRef.current < MAX_POLL_FAILURES) {
        schedulePoll();
      }
    }
  }, [message, schedulePoll]);

  useEffect(() => {
    requestRef.current = getCollectList;
  }, [getCollectList]);

  useEffect(() => {
    mountedRef.current = true;
    void getCollectList();
    void fetchProxyStatus();
    return () => {
      mountedRef.current = false;
      clearPollTimer();
      sortConfirmRef.current?.destroy();
      sortConfirmRef.current = null;
      pendingSortRef.current = null;
    };
  }, [clearPollTimer, fetchProxyStatus, getCollectList]);

  const updateSiteListItem = useCallback(
    (id: string, updater: (record: FilmSource) => FilmSource) => {
      setSiteList((current) =>
        current.map((item) => (item.id === id ? updater(item) : item)),
      );
    },
    [],
  );

  const changeCollectDuration = useCallback(
    (id: string, value: number) => {
      updateSiteListItem(id, (item) => ({ ...item, cd: value }));
    },
    [updateSiteListItem],
  );

  const handleSelectSource = useCallback((id: string, checked: boolean) => {
    setSelectedSourceIds((current) =>
      checked ? [...current, id] : current.filter((item) => item !== id),
    );
  }, []);

  const selectAllSources = useCallback(() => {
    setSelectedSourceIds(siteList.map((item) => item.id));
  }, [siteList]);

  const invertSelection = useCallback(() => {
    setSelectedSourceIds((current) =>
      siteList.filter((item) => !current.includes(item.id)).map((item) => item.id),
    );
  }, [siteList]);

  const clearSelection = useCallback(() => {
    setSelectedSourceIds([]);
  }, []);

  // 批量检测所有采集站接口连通性，收集采集不通的源
  const startCleanupScan = async () => {
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
      // 拦截器已统一提示，避免重复弹窗
    } finally {
      setCleanupScanning(false);
    }
  };

  const cancelCleanup = () => {
    cleanupScanCanceledRef.current = true;
    setCleanupOpen(false);
  };

  // 确认清理：批量删除失效源
  const confirmCleanup = async () => {
    if (invalidSources.length === 0) {
      return;
    }
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
        await getCollectList();
        return;
      }
      message.error(resp.msg || "清理失败");
    } catch {
      // 拦截器已统一提示，避免重复弹窗
    } finally {
      setCleanupDeleting(false);
    }
  };

  const batchChangeSourceState = async (state: boolean) => {
    const selectedSources = siteList.filter((item) => selectedSourceIds.includes(item.id));
    if (selectedSources.length === 0) {
      message.warning("请先选择采集站");
      return;
    }

    const sourceIdsToUpdate = selectedSources.filter((item) => item.state !== state).map((item) => item.id);
    if (sourceIdsToUpdate.length === 0) {
      message.info(state ? "选中采集站已全部启用" : "选中采集站已全部禁用");
      return;
    }

    setBatchStateUpdating(true);
    try {
      const resp = await ApiPost("/manage/collect/change/batch", {
        ids: sourceIdsToUpdate,
        state,
      });
      if (resp.code !== 0) {
        message.error(resp.msg || `批量${state ? "启用" : "禁用"}失败`);
      } else {
        message.success(`已${state ? "启用" : "禁用"} ${sourceIdsToUpdate.length} 个采集站`);
      }
      await getCollectList();
    } finally {
      setBatchStateUpdating(false);
    }
  };

  /** 批量删除选中采集站（采集中的会被后端跳过并提示） */
  const batchDeleteSources = async () => {
    const ids = selectedSourceIds.map(String).filter(Boolean);
    if (ids.length === 0) {
      message.warning("请先选择采集站");
      return;
    }
    setBatchDeleting(true);
    try {
      const resp = await ApiPost<DelBatchResult>("/manage/collect/del/batch", { ids });
      if (resp.code === 0) {
        const data = resp.data ?? { deleted: [], skipped: [] };
        const deleted = Array.isArray(data.deleted) ? data.deleted : [];
        const skipped = Array.isArray(data.skipped) ? data.skipped : [];
        if (deleted.length > 0) {
          message.success(`已删除 ${deleted.length} 个采集站`);
        } else {
          message.warning("没有可删除的采集站");
        }
        if (skipped.length > 0) {
          message.warning(
            `${skipped.length} 个未删除：${skipped
              .map((item) => `${item.name || item.id}（${item.reason}）`)
              .join("；")}`,
          );
        }
        setSelectedSourceIds((current) =>
          current.filter((id) => !deleted.includes(String(id))),
        );
        await getCollectList();
        return;
      }
      message.error(resp.msg || "批量删除失败");
    } catch {
      // 拦截器已统一提示，避免重复弹窗
    } finally {
      setBatchDeleting(false);
    }
  };

  const startTask = async (record: FilmSource) => {
    if (!record.state) {
      message.warning("该采集站已被禁用，无法发起采集");
      return;
    }
    if (record.categoryReady === false) {
      message.warning("该采集站还没有分类，请重新保存后再采集");
      return;
    }
    if (isActiveCollectStatus(record.progress?.status)) {
      message.warning("该采集站已在采集中");
      return;
    }
    // 点击后立即展示 0% 进度，再等接口与列表校准
    updateSiteListItem(record.id, (item) => ({
      ...item,
      progress: makeStartingProgress(record.id, record.name),
    }));
    const collectTime = record.cd ?? 24;
    const resp = await ApiPost("/manage/spider/start", {
      id: record.id,
      time: collectTime,
      batch: false,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      void getCollectList(true);
      return;
    }
    message.error(resp.msg || "启动采集失败");
    await getCollectList();
  };

  const stopTask = async (id: string) => {
    const resp = await ApiPost("/manage/spider/stop", { id });
    if (resp.code === 0) {
      message.success("已停止采集");
      await getCollectList();
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

  const handleSortList = (nextList: FilmSource[]) => {
    const prevList = pendingSortRef.current?.prev ?? siteList;
    let assignedPrimary = false;
    const optimisticList = nextList.map((item) => {
      if (item.state && !assignedPrimary) {
        assignedPrimary = true;
        return { ...item, isPrimary: true };
      }
      return { ...item, isPrimary: false };
    });
    if (!assignedPrimary && optimisticList.length > 0) {
      optimisticList[0] = { ...optimisticList[0], isPrimary: true };
    }
    setSiteList(optimisticList);
    pendingSortRef.current = { prev: prevList, next: optimisticList };

    const prevPrimary = prevList.find((s) => s.state) ?? prevList[0];
    const nextPrimary = optimisticList.find((s) => s.isPrimary);
    const isPrimaryChanged = Boolean(
      prevPrimary && nextPrimary && prevPrimary.id !== nextPrimary.id,
    );

    sortConfirmRef.current?.destroy();
    sortConfirmRef.current = modal.confirm({
      title: "保存顺序？",
      content: isPrimaryChanged && nextPrimary ? `首选站改为「${nextPrimary.name}」` : undefined,
      okText: "保存",
      cancelText: "取消",
      centered: true,
      onOk: async () => {
        const pending = pendingSortRef.current;
        if (!pending) {
          return;
        }
        try {
          const resp = await ApiPost("/manage/collect/sort", {
            ids: pending.next.map((item) => item.id),
          });
          if (resp.code === 0) {
            pendingSortRef.current = null;
            if (isPrimaryChanged && nextPrimary) {
              message.success({
                content: `首选站：${nextPrimary.name}`,
                key: "collect-sort",
              });
            } else {
              message.success({
                content: "顺序已保存",
                key: "collect-sort",
              });
            }
            return;
          }
          message.error({
            content: resp.msg || "保存排序失败",
            key: "collect-sort",
          });
          pendingSortRef.current = null;
          setSiteList(pending.prev);
        } catch (e: any) {
          message.error({
            content: e?.message || "保存排序失败",
            key: "collect-sort",
          });
          pendingSortRef.current = null;
          setSiteList(pending.prev);
        }
      },
      onCancel: () => {
        const pending = pendingSortRef.current;
        pendingSortRef.current = null;
        if (pending) {
          setSiteList(pending.prev);
        }
      },
    });
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
        sourceModalMode === "add"
          ? "/manage/collect/add"
          : "/manage/collect/update",
        sourceModalMode === "add"
          ? values
          : { ...values, id: editingId },
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

  const runSourceTest = async (values: SourceFormValues, useProxy: boolean) => {
    try {
      setTesting(true);
      message.loading({
        key: "collect-test",
        content: useProxy ? "正在通过代理测试接口..." : "正在直接测试接口...",
      });
      const resp = await ApiPost("/manage/collect/test", {
        ...values,
        ...(editingId ? { id: editingId } : {}),
        useProxy,
      });
      if (resp.code === 0) {
        message.success({ key: "collect-test", content: resp.msg });
        return;
      }
      message.error({
        key: "collect-test",
        content: resp.msg || "接口测试失败",
      });
    } catch {
      message.error({ key: "collect-test", content: "接口测试失败，请稍后重试" });
    } finally {
      setTesting(false);
    }
  };

  const testApi = async (values: SourceFormValues) => {
    const shouldUseProxy = Boolean(globalSpiderProxyReady && values.proxyCollect);
    await runSourceTest(values, shouldUseProxy);
  };

  const openBatchCollect = async () => {
    const resp = await ApiGet("/manage/collect/options");
    if (resp.code === 0) {
      const allOptions = Array.isArray(resp.data)
        ? resp.data.map((item: BatchOption) => ({
            ...item,
            sort: siteList.find((site) => site.id === item.id)?.sort ?? 0,
            state: siteList.find((site) => site.id === item.id)?.state ?? false,
          }))
        : [];
      const enabledIds = new Set(allOptions.map((item) => item.id));
      const selectedEnabledIds = selectedSourceIds
        .map(String)
        .filter((id) => enabledIds.has(id));
      if (selectedSourceIds.length === 0) {
        message.warning("请先选择要采集的采集站");
        return;
      }
      if (selectedEnabledIds.length === 0) {
        message.warning("选中的采集站均未启用，无法批量采集");
        return;
      }
      const readyIds = selectedEnabledIds.filter((id) => {
        const site = siteList.find((item) => item.id === id);
        return site?.categoryReady !== false;
      });
      if (readyIds.length === 0) {
        message.warning("选中的采集站还没有分类，请重新保存后再采集");
        return;
      }
      if (readyIds.length < selectedEnabledIds.length) {
        message.warning(
          `${selectedEnabledIds.length - readyIds.length} 个采集站还没有分类，已从本次采集中排除`,
        );
      }
      const options = allOptions.filter((item) => readyIds.includes(item.id));
      setBatchOptions(options);
      setBatchIds(readyIds);
      setBatchOpen(true);
      return;
    }
    message.error(resp.msg || "加载批量采集列表失败");
  };

  const startBatchCollect = async () => {
    if (batchIds.length === 0) {
      message.warning("请至少选择一个采集站");
      return;
    }
    const claimedIds = batchIds.filter((id) => {
      const site = siteList.find((item) => item.id === id);
      return !isActiveCollectStatus(site?.progress?.status);
    });
    if (claimedIds.length === 0) {
      message.warning("选中采集站均已在采集中，已跳过");
      setBatchOpen(false);
      return;
    }
    setBatchOpen(false);
    const idSet = new Set(claimedIds);
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id)
          ? { ...item, progress: makeStartingProgress(item.id, item.name) }
          : item,
      ),
    );
    const resp = await ApiPost("/manage/spider/start", {
      ids: batchIds,
      time: batchTime,
      batch: true,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      void getCollectList(true);
      return;
    }
    message.error(resp.msg || "批量采集启动失败");
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id) && item.progress?.status === "starting"
          ? { ...item, progress: null }
          : item,
      ),
    );
    window.dispatchEvent(new Event(COLLECT_BATCH_FAILED_EVENT));
    await getCollectList();
  };

  const selectedCount = selectedSourceIds.length;

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集中心"
        description={
          <>
            统一管理采集站与采集任务
            <span className={styles.headerMeta}>
              · {siteList.length} 个
            </span>
          </>
        }
        actions={
          <Space size={8}>
            <Button
              icon={<ApiOutlined />}
              onClick={() => router.push("/manage/system?tab=proxy")}
            >
              网络代理
            </Button>
            <Button
              danger
              loading={cleanupScanning}
              disabled={!canWrite || siteList.length === 0}
              onClick={() => void startCleanupScan()}
            >
              清理失效源
            </Button>
          </Space>
        }
      />

      <div className={styles.cardPanel}>
        <Card size="small" className={styles.toolbarCard} styles={{ body: { padding: 12 } }}>
          <div className={styles.toolbar}>
            <Space size={[8, 8]} wrap>
              <span className={styles.toolbarHint}>
                共 {siteList.length} 个
              </span>
              <Button size="small" data-tour="collect-select-all" onClick={selectAllSources}>
                全选
              </Button>
              <Button size="small" onClick={invertSelection}>
                反选
              </Button>
              <Button size="small" disabled={selectedCount === 0} onClick={clearSelection}>
                清空
              </Button>
            </Space>
            <Space size={8} wrap className={styles.toolbarActions}>
              <Button
                type="primary"
                disabled={!canWrite || selectedCount === 0}
                data-tour="collect-batch"
                onClick={() => void openBatchCollect()}
              >
                批量采集{selectedCount > 0 ? ` (${selectedCount})` : ""}
              </Button>
              <Button
                loading={batchStateUpdating}
                disabled={!canWrite || selectedCount === 0}
                data-tour="collect-batch-enable"
                onClick={() => void batchChangeSourceState(true)}
              >
                批量启用{selectedCount > 0 ? ` (${selectedCount})` : ""}
              </Button>
              <Popconfirm
                title="禁用选中采集站？"
                okText="禁用"
                cancelText="取消"
                okButtonProps={{ danger: true }}
                disabled={selectedCount === 0}
                onConfirm={() => void batchChangeSourceState(false)}
              >
                <Button
                  danger
                  loading={batchStateUpdating}
                  disabled={!canWrite || selectedCount === 0}
                >
                  批量禁用{selectedCount > 0 ? ` (${selectedCount})` : ""}
                </Button>
              </Popconfirm>
              <Popconfirm
                title={`删除 ${selectedCount} 个采集站？`}
                description="不可恢复"
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true, loading: batchDeleting }}
                disabled={selectedCount === 0}
                onConfirm={() => void batchDeleteSources()}
              >
                <Button
                  danger
                  loading={batchDeleting}
                  disabled={!canWrite || selectedCount === 0}
                >
                  批量删除{selectedCount > 0 ? ` (${selectedCount})` : ""}
                </Button>
              </Popconfirm>
            </Space>
          </div>
        </Card>

        {siteList.length > 0 ? (
          <div
            className={styles.sourceGroups}
            data-tour={tourProgressPhase ? "collect-progress" : undefined}
            data-tour-progress={tourProgressPhase}
          >
            <CollectSourceGrid
              siteList={siteList}
              selectedSourceIds={selectedSourceIds.map(String)}
              activeCollectIds={activeCollectIds}
              hiddenDoneIds={[]}
              canWrite={canWrite}
              canAddSource={canAddSource}
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
            <Empty
              description={loading ? "采集站加载中…" : "暂无采集站"}
            >
              {!loading && canAddSource && canWrite ? (
                <Button type="primary" icon={<PlusOutlined />} onClick={openAddDialog}>
                  新增采集站
                </Button>
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
