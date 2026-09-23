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
import {
  COLLECT_BATCH_FAILED_EVENT,
  TOUR_HOLD_PROGRESS_ATTR,
} from "@/app/manage/components/manage-tour";
import BatchCollectModal from "./batch-collect-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectQueueBars, { type CollectQueueBarItem } from "./collect-queue-bars";
import CollectSourceCard from "./collect-source-card";
import SourceFormModal from "./source-form-modal";
import {
  computeCollectQueueProgress,
  groupCollectQueues,
  matchPrevCollectQueueBar,
  newClientCollectQueueId,
} from "./collect-queue";
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

type QueueBarState = {
  queueId: string;
  sourceIds: string[];
  seq: number;
  view: NonNullable<ReturnType<typeof computeCollectQueueProgress>>;
  hideAt: number | null;
};

/** 启动瞬间本地进度：0%，避免等轮询才出现进度条 */
function makeStartingProgress(id: string, name: string, queueId: string): CollectProgress {
  return {
    id,
    name,
    total: 0,
    current: 0,
    success: 0,
    failed: 0,
    status: "starting",
    queueId,
  };
}

const POLL_INTERVAL = 4000;
const MAX_POLL_FAILURES = 10;
/** 采集全部完成/失败后，顶部总进度条保留展示的时长 */
const OVERALL_DONE_KEEP_MS = 10000;

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    state: Boolean(item.state),
    grade: Number(item.grade ?? 1),
    isPosterSource: Boolean(item.isPosterSource),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd > 0 ? item.cd : 24),
    format: (item.format as "json" | "xml") || "json",
    lastCollectTime: item.lastCollectTime,
    progress: item.progress ?? null,
    proxyEnabled: Boolean(item.proxyEnabled),
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

  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [sourceInitialValues, setSourceInitialValues] =
    useState<SourceFormValues>(SOURCE_FORM_DEFAULTS);
  const [sourceFormNonce, setSourceFormNonce] = useState(0);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);
  const proxyChoiceRef = useRef<boolean | null>(null);

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

  const [stoppingQueueId, setStoppingQueueId] = useState<string | null>(null);
  const [queueBars, setQueueBars] = useState<QueueBarState[]>([]);
  const [nowMs, setNowMs] = useState(() => Date.now());
  const queueSeqRef = useRef(0);
  const seenRunningQueueIdsRef = useRef<Set<string>>(new Set());
  /** 顶部进度条已倒计时隐藏的终态任务；同步隐藏对应卡片环形进度 */
  const [hiddenDoneIds, setHiddenDoneIds] = useState<string[]>([]);
  const [tourHoldProgress, setTourHoldProgress] = useState(false);

  // 仅「仍在生命周期内」的任务禁用操作；done/failed 短暂展示进度但不锁按钮。
  const activeCollectIds = useMemo(
    () =>
      siteList
        .filter((item) => isActiveCollectStatus(item.progress?.status))
        .map((item) => item.id),
    [siteList],
  );

  /** 主站优先，其余保持列表顺序，同一网格展示 */
  const displaySites = useMemo(() => {
    const masters = siteList.filter((item) => item.grade === 0);
    const others = siteList.filter((item) => item.grade !== 0);
    return [...masters, ...others];
  }, [siteList]);

  const masterCount = useMemo(
    () => siteList.filter((item) => item.grade === 0).length,
    [siteList],
  );

  const canAddSource = canWrite;

  const liveQueueGroups = useMemo(() => groupCollectQueues(siteList), [siteList]);

  useEffect(() => {
    const root = document.documentElement;
    const sync = () => setTourHoldProgress(root.hasAttribute(TOUR_HOLD_PROGRESS_ATTR));
    sync();
    const obs = new MutationObserver(sync);
    obs.observe(root, { attributes: true, attributeFilter: [TOUR_HOLD_PROGRESS_ATTR] });
    return () => obs.disconnect();
  }, []);

  useEffect(() => {
    const liveIds = new Set(liveQueueGroups.map((group) => group.queueId));
    const expiredIds: string[] = [];
    const expiredSourceIds: string[] = [];

    setQueueBars((prev) => {
      const usedPrev = new Set<string>();
      const next: QueueBarState[] = [];

      const matchPrev = (group: { queueId: string; sourceIds: string[] }) =>
        matchPrevCollectQueueBar(group, prev, usedPrev);

      for (const group of liveQueueGroups) {
        const view = computeCollectQueueProgress(group.sourceIds, siteList, activeCollectIds);
        if (!view) {
          continue;
        }
        const existing = matchPrev(group);
        if (existing) {
          usedPrev.add(existing.queueId);
        }
        if (view.running) {
          seenRunningQueueIdsRef.current.add(group.queueId);
          next.push({
            queueId: group.queueId,
            sourceIds: group.sourceIds,
            seq: existing?.seq ?? ++queueSeqRef.current,
            view,
            hideAt: null,
          });
          continue;
        }
        if (!existing && !seenRunningQueueIdsRef.current.has(group.queueId) && !tourHoldProgress) {
          expiredSourceIds.push(...group.sourceIds);
          continue;
        }
        const hideAt = tourHoldProgress
          ? null
          : existing?.hideAt ?? nowMs + OVERALL_DONE_KEEP_MS;
        if (hideAt != null && hideAt <= nowMs) {
          expiredSourceIds.push(...group.sourceIds);
          expiredIds.push(group.queueId);
          continue;
        }
        next.push({
          queueId: group.queueId,
          sourceIds: group.sourceIds,
          seq: existing?.seq ?? ++queueSeqRef.current,
          view,
          hideAt,
        });
      }

      const coveredSources = new Set(next.flatMap((bar) => bar.sourceIds));
      for (const bar of prev) {
        if (liveIds.has(bar.queueId) || usedPrev.has(bar.queueId)) {
          continue;
        }
        if (bar.sourceIds.some((id) => coveredSources.has(id))) {
          continue;
        }
        if (tourHoldProgress) {
          next.push(bar);
          continue;
        }
        if (bar.hideAt != null && bar.hideAt > nowMs) {
          next.push({
            ...bar,
            view: {
              ...bar.view,
              running: false,
              percent: 100,
              activeCount: 0,
              fetchingCount: 0,
              wrappingCount: 0,
            },
          });
          continue;
        }
        if (bar.view.running && bar.hideAt == null) {
          next.push({
            ...bar,
            view: {
              ...bar.view,
              running: false,
              percent: 100,
              activeCount: 0,
              fetchingCount: 0,
              wrappingCount: 0,
            },
            hideAt: nowMs + OVERALL_DONE_KEEP_MS,
          });
          continue;
        }
        expiredIds.push(bar.queueId);
        expiredSourceIds.push(...bar.sourceIds);
      }

      next.sort((a, b) => a.seq - b.seq);
      return next;
    });

    if (expiredIds.length > 0) {
      for (const id of expiredIds) {
        seenRunningQueueIdsRef.current.delete(id);
      }
    }
    if (expiredSourceIds.length > 0) {
      setHiddenDoneIds((prev) => {
        const merged = [...new Set([...prev, ...expiredSourceIds])];
        return merged.length === prev.length ? prev : merged;
      });
    }
  }, [activeCollectIds, liveQueueGroups, nowMs, siteList, tourHoldProgress]);

  useEffect(() => {
    const pending = queueBars.some((bar) => bar.hideAt != null);
    if (!pending) {
      return;
    }
    const timer = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(timer);
  }, [queueBars]);

  const visibleQueueBars = useMemo<CollectQueueBarItem[]>(() => {
    const byId = new Map(siteList.map((item) => [item.id, item]));
    return queueBars
      .filter((bar) => bar.hideAt == null || bar.hideAt > nowMs)
      .map((bar) => ({
        queueId: bar.queueId,
        sourceIds: bar.sourceIds,
        seq: bar.seq,
        view: bar.view,
        countdown:
          bar.hideAt != null ? Math.max(1, Math.ceil((bar.hideAt - nowMs) / 1000)) : null,
        canStop: bar.sourceIds.some((id) => {
          const status = byId.get(id)?.progress?.status;
          return status === "starting" || status === "running";
        }),
      }));
  }, [nowMs, queueBars, siteList]);

  // 站点重新变为活跃（再次采集）时，取消卡片进度隐藏
  useEffect(() => {
    if (hiddenDoneIds.length === 0) {
      return;
    }
    const activeSet = new Set(activeCollectIds);
    setHiddenDoneIds((prev) => {
      const next = prev.filter((id) => !activeSet.has(id));
      return next.length === prev.length ? prev : next;
    });
  }, [activeCollectIds, hiddenDoneIds]);

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
          return rawData.map((item: CollectListItemResponse) => {
            const normalized = normalizeSource(item);
            if (cdMap.has(item.id) && cdMap.get(item.id)) {
              normalized.cd = cdMap.get(item.id);
            }
            return normalized;
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
    return () => {
      mountedRef.current = false;
      clearPollTimer();
    };
  }, [clearPollTimer, getCollectList]);

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
          message.success(`检测完成：全部 ${data.checked ?? 0} 个采集站接口正常，无需清理${skipText}`);
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

  /** 批量删除选中采集站（主站/采集中的会被后端跳过并提示） */
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
    if (isActiveCollectStatus(record.progress?.status)) {
      message.warning("该采集站已在采集中");
      return;
    }
    // 点击后立即展示 0% 进度条，再等接口与列表校准
    const queueId = newClientCollectQueueId();
    updateSiteListItem(record.id, (item) => ({
      ...item,
      progress: makeStartingProgress(record.id, record.name, queueId),
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
      message.success("已停止该采集任务，已抓取数据将继续处理完成");
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
    setSourceModalMode("add");
    setEditingId(null);
    setSourceInitialValues(SOURCE_FORM_DEFAULTS);
    proxyChoiceRef.current = null;
    setSourceFormNonce((n) => n + 1);
    setSourceModalOpen(true);
  };

  const openAddDialog = () => {
    if (siteList.length >= COLLECT_SOURCE_WARN_COUNT) {
      modal.confirm({
        title: "采集站数量过多",
        content: `当前已有 ${siteList.length} 个采集站。采集站越多，排队越长，写库、快照和内存占用都会上升，低配机器更容易打满。确认继续添加？`,
        okText: "继续添加",
        cancelText: "取消",
        onOk: openAddForm,
      });
      return;
    }
    openAddForm();
  };

  const openEditDialog = async (id: string) => {
    setSourceModalMode("edit");
    setEditingId(id);
    const resp = await ApiGet("/manage/collect/find", { id });
    if (resp.code === 0 && resp.data) {
      setSourceInitialValues({
        name: String(resp.data.name ?? ""),
        uri: String(resp.data.uri ?? ""),
        state: Boolean(resp.data.state),
        grade: Number(resp.data.grade ?? 1),
        isPosterSource: Boolean(resp.data.isPosterSource),
        interval: Number(resp.data.interval ?? 0),
        cd: Number(resp.data.cd > 0 ? resp.data.cd : 24),
        format: (resp.data.format as "json" | "xml") || "json",
        domainReplaceRules: String(resp.data.domainReplaceRules ?? ""),
      });
      proxyChoiceRef.current = null;
      setSourceFormNonce((n) => n + 1);
      setSourceModalOpen(true);
      return;
    }
    message.error(resp.msg || "获取采集站信息失败");
  };

  const handleSubmitSource = async (values: SourceFormValues) => {
    const useProxy = proxyChoiceRef.current ?? undefined;
    setSubmitting(true);
    try {
      const resp = await ApiPost(
        sourceModalMode === "add"
          ? "/manage/collect/add"
          : "/manage/collect/update",
        sourceModalMode === "add"
          ? { ...values, ...(useProxy !== undefined ? { useProxy } : {}) }
          : { ...values, id: editingId, ...(useProxy !== undefined ? { useProxy } : {}) },
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

  const askProxyChoice = (): Promise<boolean | null> => {
    return new Promise((resolve) => {
      void (async () => {
        let enabled = false;
        let proxyUrl = "";
        try {
          const cfg = await ApiGet("/manage/proxy/config");
          if (cfg.code !== 0) {
            message.error(cfg.msg || "获取代理配置失败");
            resolve(null);
            return;
          }
          enabled = Boolean(cfg.data?.enabled);
          proxyUrl = String(cfg.data?.proxyUrl || "").trim();
        } catch {
          message.error("获取代理配置失败");
          resolve(null);
          return;
        }
        if (!enabled || !proxyUrl) {
          proxyChoiceRef.current = false;
          resolve(false);
          return;
        }
        const dialogRef: { current?: { destroy: () => void } } = {};
        let settled = false;
        const finish = (choice: boolean | null) => {
          if (settled) return;
          settled = true;
          if (choice !== null) {
            proxyChoiceRef.current = choice;
          }
          dialogRef.current?.destroy();
          resolve(choice);
        };
        dialogRef.current = modal.confirm({
          title: "是否使用代理测试？",
          content: `系统已开启网络代理（${proxyUrl}）。本次可以选择走代理，或直接连接采集站。`,
          okText: "使用代理",
          cancelText: "直接测试",
          zIndex: 2000,
          onOk: () => finish(true),
          onCancel: () => finish(null),
          cancelButtonProps: {
            onClick: () => finish(false),
          },
        });
      })();
    });
  };

  const testApi = async (values: SourceFormValues) => {
    const useProxy = await askProxyChoice();
    if (useProxy === null) {
      return;
    }
    await runSourceTest(values, useProxy);
  };

  const openBatchCollect = async () => {
    const resp = await ApiGet("/manage/collect/options");
    if (resp.code === 0) {
      const allOptions = Array.isArray(resp.data)
        ? resp.data.map((item: BatchOption) => ({
            ...item,
            grade: siteList.find((site) => site.id === item.id)?.grade ?? 1,
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
      const options = allOptions.filter((item) => selectedEnabledIds.includes(item.id));
      setBatchOptions(options);
      setBatchIds(selectedEnabledIds);
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
    const queueId = newClientCollectQueueId();
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id)
          ? { ...item, progress: makeStartingProgress(item.id, item.name, queueId) }
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

  const stopQueue = async (queueId: string, sourceIds: string[]) => {
    const stoppable = sourceIds.filter((id) => {
      const status = siteList.find((item) => item.id === id)?.progress?.status;
      return status === "starting" || status === "running";
    });
    if (stoppable.length === 0) {
      return;
    }
    setStoppingQueueId(queueId);
    try {
      const results = await Promise.all(
        stoppable.map((id) => ApiPost("/manage/spider/stop", { id })),
      );
      if (results.some((resp) => resp.code === 0)) {
        message.success("已停止该采集队列中仍在抓取的任务，已抓取数据将继续处理完成");
      } else {
        message.error(results[0]?.msg || "终止任务失败");
      }
      await getCollectList();
    } finally {
      setStoppingQueueId(null);
    }
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
                title="批量禁用采集站？"
                description="禁用后会停止选中采集站的后续请求，已抓取数据会继续处理完成，并阻止后续批量/自动采集调度。"
                okText="确认禁用"
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
                title={`批量删除 ${selectedCount} 个采集站？`}
                description="删除后不可恢复。主采集站与正在采集的站点会自动跳过。"
                okText="确认删除"
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

        <CollectQueueBars
          items={visibleQueueBars}
          tourHoldProgress={tourHoldProgress}
          stoppingQueueId={stoppingQueueId}
          canWrite={canWrite}
          onStopQueue={(queueId, sourceIds) => void stopQueue(queueId, sourceIds)}
        />

        {siteList.length > 0 ? (
          <div className={styles.sourceGroups}>
            {masterCount === 0 ? (
              <div className={styles.masterTip}>
                尚未配置主采集站
                {canAddSource && canWrite ? (
                  <>
                    ，
                    <Typography.Link onClick={openAddDialog}>新增</Typography.Link>
                    时将类型设为「主采集站」
                  </>
                ) : null}
              </div>
            ) : null}
            {masterCount > 1 ? (
              <div className={styles.masterTipWarn}>
                当前有 {masterCount} 个主采集站，业务上应只保留一个
              </div>
            ) : null}
            <div className={styles.cardGrid}>
              {displaySites.map((site) => {
                const hiddenDone =
                  hiddenDoneIds.includes(site.id) &&
                  site.progress != null &&
                  !isActiveCollectStatus(site.progress.status);
                return (
                  <CollectSourceCard
                    key={site.id}
                    record={hiddenDone ? { ...site, progress: null } : site}
                    selected={selectedSourceIds.includes(site.id)}
                    active={activeCollectIds.includes(site.id)}
                    onSelect={handleSelectSource}
                    onChangeCollectDuration={changeCollectDuration}
                    onStartTask={(record) => void startTask(record)}
                    onTerminateTask={(id) => void stopTask(id)}
                    onEditSource={(id) => void openEditDialog(id)}
                    onDeleteSource={(id) => void delSource(id)}
                  />
                );
              })}
              {canAddSource && canWrite ? (
                <button
                  type="button"
                  className={styles.addSourceTile}
                  onClick={openAddDialog}
                >
                  <PlusOutlined className={styles.addSourceIcon} />
                  <span className={styles.addSourceLabel}>新增采集站</span>
                  <span className={styles.addSourceHint}>
                    {siteList.length >= COLLECT_SOURCE_WARN_COUNT
                      ? `已超过建议数量（${COLLECT_SOURCE_WARN_COUNT}）`
                      : "添加新的采集源"}
                  </span>
                </button>
              ) : null}
            </div>
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
