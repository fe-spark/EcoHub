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
  Form,
  Popconfirm,
  Progress,
  Space,
  Typography,
} from "antd";
import { PlusOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectModal from "./batch-collect-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectSourceCard from "./collect-source-card";
import SourceFormModal from "./source-form-modal";
import {
  isActiveCollectStatus,
  MAX_COLLECT_SOURCES,
  stationProgressPercent,
  type BatchOption,
  type CheckAllResult,
  type CleanupSkippedItem,
  type CollectProgress,
  type DelBatchResult,
  type FilmSource,
  type InvalidSourceItem,
  type SourceFormValues,
} from "./types";
import styles from "./index.module.less";

interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
}

/** 顶部总进度条会话快照 */
interface OverallProgressView {
  total: number;
  activeCount: number;
  doneCount: number;
  failedCount: number;
  fetchingCount: number;
  wrappingCount: number;
  percent: number;
  success: number;
  failed: number;
  running: boolean;
  statsText: string;
}

/** 启动瞬间本地进度：0%，避免等轮询才出现进度条 */
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
/** 采集全部完成/失败后，顶部总进度条保留展示的时长 */
const OVERALL_DONE_KEEP_MS = 5000;
/** 结束倒计时的初始秒数（与 OVERALL_DONE_KEEP_MS 对应） */
const OVERALL_DONE_KEEP_SECONDS = OVERALL_DONE_KEEP_MS / 1000;

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    state: Boolean(item.state),
    grade: Number(item.grade ?? 1),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd > 0 ? item.cd : 24),
    lastCollectTime: item.lastCollectTime,
    progress: item.progress ?? null,
  };
}

export default function CollectManagePageView() {
  const { message } = useAppMessage();
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

  const [sourceForm] = Form.useForm<SourceFormValues>();
  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [testing, setTesting] = useState(false);

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

  /** 本页发起的批量采集会话 ID；用于展示总进度条，全部结束后自动收起 */
  const [batchRunIds, setBatchRunIds] = useState<string[]>([]);
  const [stoppingAll, setStoppingAll] = useState(false);
  /** 顶部总进度：进行中或结束倒计时内的最近一次会话快照 */
  const [overallSession, setOverallSession] = useState<OverallProgressView | null>(null);
  const overallDoneTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  /** 结束倒计时剩余秒数（页面上可见） */
  const [overallCountdown, setOverallCountdown] = useState<number | null>(null);
  const overallCountdownRef = useRef<ReturnType<typeof setInterval> | null>(null);
  /** 结束态已展示并倒计时隐藏后，残留期内不再闪回 */
  const overallHiddenRef = useRef(false);
  /** 启动结束倒计时时的任务 ID 快照，供到期隐藏卡片进度使用 */
  const overallDoneIdsRef = useRef<string[]>([]);
  /** 本次挂载是否见过运行中任务：挂载即结束态说明是上次会话残留，不展示 */
  const hasSeenRunningRef = useRef(false);
  /** 顶部进度条已倒计时隐藏的终态任务；同步隐藏对应卡片环形进度 */
  const [hiddenDoneIds, setHiddenDoneIds] = useState<string[]>([]);

  const clearOverallCountdown = useCallback(() => {
    if (overallCountdownRef.current) {
      clearInterval(overallCountdownRef.current);
      overallCountdownRef.current = null;
    }
    setOverallCountdown(null);
  }, []);

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

  const canAddSource = siteList.length < MAX_COLLECT_SOURCES;

  /**
   * 总进度条覆盖的任务 ID：
   * - 本页批量启动后优先用 batchRunIds（含已完成站，进度可到 100%）
   * - 单个/批量采集均展示：活跃任务 + 仍有终态进度残留（完成/失败后保留期内）都计入，
   *   避免结束后顶部进度条瞬间消失
   */
  const overallTaskIds = useMemo(() => {
    if (batchRunIds.length > 0) {
      return batchRunIds;
    }
    const withProgress = siteList
      .filter((item) => item.progress != null)
      .map((item) => item.id);
    const ids = [...new Set([...activeCollectIds, ...withProgress])];
    if (ids.length >= 1) {
      return ids;
    }
    return [] as string[];
  }, [batchRunIds, activeCollectIds, siteList]);

  const overallProgress = useMemo<OverallProgressView | null>(() => {
    if (overallTaskIds.length === 0) {
      return null;
    }
    const byId = new Map(siteList.map((item) => [item.id, item]));
    let percentSum = 0;
    let success = 0;
    let failed = 0;
    /** 拉取中（starting/running） */
    let fetchingCount = 0;
    /** 收尾中（page_done/waiting_publish/finalizing） */
    let wrappingCount = 0;
    /** 终态完成（done/stopped/进度已清） */
    let doneCount = 0;
    /** 终态失败 */
    let failedCount = 0;
    let hasAnyProgress = false;

    for (const id of overallTaskIds) {
      const item = byId.get(id);
      const progress = item?.progress ?? null;
      if (progress) {
        hasAnyProgress = true;
        success += progress.success;
        failed += progress.failed;
        percentSum += stationProgressPercent(progress);
        const status = progress.status;
        if (status === "starting" || status === "running") {
          fetchingCount += 1;
        } else if (
          status === "page_done" ||
          status === "waiting_publish" ||
          status === "finalizing"
        ) {
          wrappingCount += 1;
        } else if (status === "failed") {
          failedCount += 1;
        } else {
          // done / stopped / 其它终态
          doneCount += 1;
        }
      } else if (activeCollectIds.includes(id)) {
        // 刚启动、列表尚未带回 progress
        fetchingCount += 1;
        hasAnyProgress = true;
      } else {
        // 进度已清理：按完成计
        doneCount += 1;
        percentSum += 100;
      }
    }

    const total = overallTaskIds.length;
    const activeCount = fetchingCount + wrappingCount;
    const percent = total > 0 ? Math.floor(percentSum / total) : 0;
    const running = activeCount > 0;

    // 无活跃、也无任何进度残留时不展示
    if (!running && !hasAnyProgress && batchRunIds.length === 0) {
      return null;
    }

    // 文案：避免「0/9 站」这种全程无信息量的分数
    const phaseParts: string[] = [`共 ${total} 站`];
    if (running) {
      if (fetchingCount > 0) {
        phaseParts.push(`采集中 ${fetchingCount}`);
      }
      if (wrappingCount > 0) {
        phaseParts.push(`收尾 ${wrappingCount}`);
      }
      if (doneCount > 0) {
        phaseParts.push(`完成 ${doneCount}`);
      }
      if (failedCount > 0) {
        phaseParts.push(`异常 ${failedCount}`);
      }
    } else {
      if (failedCount > 0) {
        phaseParts.push(`异常 ${failedCount}`);
      }
      if (doneCount > 0 || failedCount === 0) {
        phaseParts.push("已结束");
      }
    }
    if (success > 0 || failed > 0) {
      phaseParts.push(`入库 ${success}`);
    }
    if (failed > 0) {
      phaseParts.push(`失败 ${failed}`);
    }

    return {
      total,
      activeCount,
      doneCount,
      failedCount,
      fetchingCount,
      wrappingCount,
      percent: running ? Math.min(percent, 99) : Math.min(percent, 100),
      success,
      failed,
      running,
      statsText: phaseParts.join(" · "),
    };
  }, [overallTaskIds, siteList, activeCollectIds, batchRunIds.length]);

  // 批量会话全部结束后，进度条保留展示直至服务端清掉终态进度，再收起
  useEffect(() => {
    if (batchRunIds.length === 0) {
      return;
    }
    const anyActive = batchRunIds.some((id) => activeCollectIds.includes(id));
    const anyProgress = batchRunIds.some((id) =>
      siteList.some((item) => item.id === id && item.progress != null),
    );
    if (!anyActive && !anyProgress) {
      setBatchRunIds([]);
    }
  }, [batchRunIds, activeCollectIds, siteList]);

  // 顶部总进度：进行中实时更新；全部结束（完成/失败/停止）后保留 OVERALL_DONE_KEEP_MS 再隐藏
  useEffect(() => {
    if (overallProgress) {
      if (overallProgress.running) {
        hasSeenRunningRef.current = true;
        overallHiddenRef.current = false;
        setOverallSession(overallProgress);
        clearOverallCountdown();
        if (overallDoneTimerRef.current) {
          clearTimeout(overallDoneTimerRef.current);
          overallDoneTimerRef.current = null;
        }
      } else if (!hasSeenRunningRef.current && !overallDoneTimerRef.current) {
        // 挂载即结束态：上次会话的终态残留，不展示
        setOverallSession(null);
        setHiddenDoneIds((prev) => [...new Set([...prev, ...overallTaskIds])]);
      } else if (!overallHiddenRef.current && !overallDoneTimerRef.current) {
        // 本次会话刚结束：启动结束倒计时
        setOverallSession(overallProgress);
        overallDoneIdsRef.current = overallTaskIds;
        setOverallCountdown(OVERALL_DONE_KEEP_SECONDS);
        overallCountdownRef.current = setInterval(() => {
          setOverallCountdown((prev) => (prev == null || prev <= 1 ? prev : prev - 1));
        }, 1000);
        overallDoneTimerRef.current = setTimeout(() => {
          overallDoneTimerRef.current = null;
          overallHiddenRef.current = true;
          clearOverallCountdown();
          setOverallSession(null);
          setHiddenDoneIds((prev) => [...new Set([...prev, ...overallDoneIdsRef.current])]);
        }, OVERALL_DONE_KEEP_MS);
      }
    } else if (overallTaskIds.length === 0) {
      overallHiddenRef.current = false;
      clearOverallCountdown();
      if (overallDoneTimerRef.current) {
        clearTimeout(overallDoneTimerRef.current);
        overallDoneTimerRef.current = null;
      }
      setOverallSession(null);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- overallTaskIds 仅用于启动结束倒计时时的任务快照
  }, [clearOverallCountdown, overallProgress, overallTaskIds.length]);

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
        const list = Array.isArray(resp.data)
          ? resp.data.map((item: CollectListItemResponse) =>
              normalizeSource(item),
            )
          : [];
        setSiteList(list);
        setSelectedSourceIds((current) =>
          current.filter((id) => list.some((item) => item.id === id)),
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
      clearOverallCountdown();
      if (overallDoneTimerRef.current) {
        clearTimeout(overallDoneTimerRef.current);
        overallDoneTimerRef.current = null;
      }
    };
  }, [clearOverallCountdown, clearPollTimer, getCollectList]);

  const updateSiteListItem = useCallback(
    (id: string, updater: (record: FilmSource) => FilmSource) => {
      setSiteList((current) =>
        current.map((item) => (item.id === id ? updater(item) : item)),
      );
    },
    [],
  );

  const changeCollectDuration = useCallback(
    async (id: string, value: number) => {
      const record = siteList.find((item) => item.id === id);
      if (!record) {
        return;
      }
      updateSiteListItem(id, (item) => ({ ...item, cd: value }));
      const resp = await ApiPost("/manage/collect/update", {
        id,
        name: record.name,
        uri: record.uri,
        grade: record.grade,
        state: record.state,
        interval: record.interval,
        cd: value,
      });
      if (resp.code !== 0) {
        message.error(resp.msg || "保存采集时长失败");
        updateSiteListItem(id, (item) => ({ ...item, cd: record.cd }));
      }
    },
    [message, siteList, updateSiteListItem],
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
      message.success("已停止该采集任务，已请求数据将继续入库");
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

  const openAddDialog = () => {
    if (siteList.length >= MAX_COLLECT_SOURCES) {
      message.warning(`采集站数量已达上限（${MAX_COLLECT_SOURCES} 个）`);
      return;
    }
    setSourceModalMode("add");
    setEditingId(null);
    sourceForm.resetFields();
    sourceForm.setFieldsValue({
      grade: 1,
      state: false,
      interval: 0,
      cd: 24,
      name: "",
      uri: "",
    });
    setSourceModalOpen(true);
  };

  const openEditDialog = async (id: string) => {
    setSourceModalMode("edit");
    setEditingId(id);
    const resp = await ApiGet("/manage/collect/find", { id });
    if (resp.code === 0 && resp.data) {
      sourceForm.setFieldsValue({
        name: String(resp.data.name ?? ""),
        uri: String(resp.data.uri ?? ""),
        state: Boolean(resp.data.state),
        grade: Number(resp.data.grade ?? 1),
        interval: Number(resp.data.interval ?? 0),
        cd: Number(resp.data.cd > 0 ? resp.data.cd : 24),
      });
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

  const testApi = async () => {
    try {
      const values = await sourceForm.validateFields();
      setTesting(true);
      message.loading({
        key: "collect-test",
        content: "正在测试接口，请稍候...",
      });
      const resp = await ApiPost("/manage/collect/test", values);
      if (resp.code === 0) {
        message.success({ key: "collect-test", content: resp.msg });
        return;
      }
      message.error({
        key: "collect-test",
        content: resp.msg || "接口测试失败",
      });
    } catch {
      // 表单校验失败时不额外提示。
    } finally {
      setTesting(false);
    }
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
    const idSet = new Set(batchIds);
    // 批量启动：先本地全部置为 starting 0%，关闭弹窗即可看到进度
    setSiteList((current) =>
      current.map((item) =>
        idSet.has(item.id) && !isActiveCollectStatus(item.progress?.status)
          ? { ...item, progress: makeStartingProgress(item.id, item.name) }
          : item,
      ),
    );
    setBatchRunIds(batchIds);
    const resp = await ApiPost("/manage/spider/start", {
      ids: batchIds,
      time: batchTime,
      batch: true,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setBatchOpen(false);
      void getCollectList(true);
      return;
    }
    message.error(resp.msg || "批量采集启动失败");
    setBatchRunIds([]);
    setBatchOpen(false);
    await getCollectList();
  };

  const submitStopAllTasks = async () => {
    setStoppingAll(true);
    try {
      const resp = await ApiPost("/manage/spider/stopAll", {});
      if (resp.code === 0) {
        message.success(resp.msg);
        await getCollectList();
        return;
      }
      message.error(resp.msg || "终止任务失败");
    } finally {
      setStoppingAll(false);
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
              · {siteList.length}/{MAX_COLLECT_SOURCES}
            </span>
          </>
        }
        actions={
          <Button
            danger
            loading={cleanupScanning}
            disabled={!canWrite || siteList.length === 0}
            onClick={() => void startCleanupScan()}
          >
            清理失效源
          </Button>
        }
      />

      <div className={styles.cardPanel}>
        <Card size="small" className={styles.toolbarCard} styles={{ body: { padding: 12 } }}>
          <div className={styles.toolbar}>
            <Space size={[8, 8]} wrap>
              <span className={styles.toolbarHint}>
                共 {siteList.length}/{MAX_COLLECT_SOURCES} 个
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
                description="禁用后会停止选中采集站的后续请求，已请求数据会继续入库，并阻止后续批量/自动采集调度。"
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

        {overallSession ? (
          <div className={styles.batchProgressBar} data-tour="collect-progress">
            <div className={styles.batchProgressMain}>
              <div className={styles.batchProgressHead}>
                <span className={styles.batchProgressTitle}>
                  {overallSession.running ? "采集进行中" : "采集已结束"}
                </span>
                <span className={styles.batchProgressHeadRight}>
                  <span className={styles.batchProgressStats}>
                    {overallSession.statsText}
                    {!overallSession.running && overallCountdown != null
                      ? ` · ${overallCountdown}s 后关闭`
                      : ""}
                  </span>
                  {overallSession.running ? (
                    <Popconfirm
                      title="终止当前采集任务？"
                      description="将强制停止当前所有进行中的采集；已请求数据会继续入库。"
                      onConfirm={() => void submitStopAllTasks()}
                      okText="确认终止"
                      cancelText="取消"
                      okButtonProps={{ danger: true, loading: stoppingAll }}
                    >
                      <Button
                        danger
                        size="small"
                        loading={stoppingAll}
                        disabled={!canWrite}
                        className={styles.batchStopBtn}
                      >
                        终止
                      </Button>
                    </Popconfirm>
                  ) : null}
                </span>
              </div>
              <div className={styles.batchProgressRow}>
                <Progress
                  percent={overallSession.percent}
                  status={
                    overallSession.running
                      ? "active"
                      : overallSession.failedCount > 0
                        ? "normal"
                        : "success"
                  }
                  strokeColor={overallSession.failed > 0 ? "#faad14" : undefined}
                  size="small"
                  className={styles.batchProgressFill}
                />
              </div>
            </div>
          </div>
        ) : null}

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
                    还可添加 {MAX_COLLECT_SOURCES - siteList.length} 个
                  </span>
                </button>
              ) : null}
            </div>
          </div>
        ) : (
          <div className={styles.emptyCard}>
            <Empty
              description={
                loading
                  ? "采集站加载中…"
                  : canAddSource && canWrite
                    ? "暂无采集站"
                    : `暂无采集站（上限 ${MAX_COLLECT_SOURCES}）`
              }
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
        open={sourceModalOpen}
        mode={sourceModalMode}
        loading={submitting}
        testing={testing}
        form={sourceForm}
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
