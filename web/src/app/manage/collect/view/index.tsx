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
  Space,
} from "antd";
import { ClearOutlined, PauseOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost, ApiPostLong } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectModal from "./batch-collect-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import CollectMasterPanel from "./collect-master-panel";
import CollectSourceCard from "./collect-source-card";
import CollectOverview from "./collect-overview";
import SourceFormModal from "./source-form-modal";
import {
  isActiveCollectStatus,
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

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    syncPictures: Boolean(item.syncPictures),
    state: Boolean(item.state),
    grade: Number(item.grade ?? 1),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd ?? 24),
    lastCollectTime: item.lastCollectTime,
    progress: item.progress ?? null,
  };
}

export default function CollectManagePageView() {
  const { message } = useAppMessage();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [selectedSourceIds, setSelectedSourceIds] = useState<React.Key[]>([]);
  const [batchStateUpdating, setBatchStateUpdating] = useState(false);
  const [loading, setLoading] = useState(false);
  const timerRef = useRef<NodeJS.Timeout | null>(null);
  const mountedRef = useRef(false);
  const pollFailuresRef = useRef(0);
  const requestRef = useRef<((silent?: boolean) => Promise<void>) | null>(null);
  const collectDurationOverridesRef = useRef<Record<string, number>>({});

  const [sourceForm] = Form.useForm<SourceFormValues>();
  const [sourceModalMode, setSourceModalMode] = useState<"add" | "edit">("add");
  const [sourceModalOpen, setSourceModalOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

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

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      // 真正在拉页/写库
      running: siteList.filter((item) => item.progress?.status === "running").length,
      // 排队 + 分页完成等待整批收尾
      waiting: siteList.filter(
        (item) =>
          item.progress?.status === "starting" ||
          item.progress?.status === "page_done" ||
          item.progress?.status === "waiting_publish" ||
          item.progress?.status === "finalizing",
      ).length,
      masters: siteList.filter((item) => item.grade === 0).length,
    }),
    [siteList],
  );

  const masterSite = useMemo(
    () => siteList.find((item) => item.grade === 0) ?? null,
    [siteList],
  );

  /** 业务上主采集站应唯一；异常时可能多于 1，仍全部列出便于修正 */
  const masterSites = useMemo(
    () => siteList.filter((item) => item.grade === 0),
    [siteList],
  );

  const affiliateSites = useMemo(
    () => siteList.filter((item) => item.grade !== 0),
    [siteList],
  );

  const masterStatus = useMemo(() => {
    if (stats.masters === 1) {
      return { text: "正常", color: "success" as const };
    }
    if (stats.masters === 0) {
      return { text: "缺少主采集站", color: "warning" as const };
    }
    return { text: `${stats.masters} 个主采集站`, color: "error" as const };
  }, [stats.masters]);

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
        const overrides = collectDurationOverridesRef.current;
        setSiteList(list.map((item) => ({
          ...item,
          cd: overrides[item.id] ?? item.cd,
        })));
        setSelectedSourceIds((current) =>
          current.filter((id) => list.some((item) => item.id === id)),
        );
      } else {
        pollFailuresRef.current += 1;
        message.error(resp.msg || "采集站列表加载失败");
      }
    } catch {
      pollFailuresRef.current += 1;
      message.error("采集站列表加载失败");
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
      collectDurationOverridesRef.current = {
        ...collectDurationOverridesRef.current,
        [id]: value,
      };
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
      message.error("失效源检测失败");
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
      message.error("清理失败");
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

  const startTask = async (record: FilmSource) => {
    if (isActiveCollectStatus(record.progress?.status)) {
      message.warning("该采集站已在采集中");
      return;
    }
    // 点击后立即展示 0% 进度条，再等接口与列表校准
    updateSiteListItem(record.id, (item) => ({
      ...item,
      progress: makeStartingProgress(record.id, record.name),
    }));
    const collectTime = collectDurationOverridesRef.current[record.id] ?? record.cd ?? 24;
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
    const resp = await ApiPost("/manage/collect/change", {
      id,
      state: false,
      syncPictures: siteList.find((item) => item.id === id)?.syncPictures ?? false,
    });
    if (resp.code === 0) {
      message.success("已停止后续请求，已请求数据将继续入库");
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
    setSourceModalMode("add");
    setEditingId(null);
    sourceForm.resetFields();
    sourceForm.setFieldsValue({
      grade: 1,
      syncPictures: false,
      state: false,
      interval: 0,
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
        syncPictures: Boolean(resp.data.syncPictures),
        state: Boolean(resp.data.state),
        grade: Number(resp.data.grade ?? 1),
        interval: Number(resp.data.interval ?? 0),
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
      const resp = await ApiPost("/manage/collect/test", values);
      if (resp.code === 0) {
        message.success(resp.msg);
        return;
      }
      message.error(resp.msg || "接口测试失败");
    } catch {
      // 表单校验失败时不额外提示。
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
    setBatchOpen(false);
    await getCollectList();
  };

  const submitStopAllTasks = async () => {
    const resp = await ApiPost("/manage/spider/stopAll", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      await getCollectList();
      return;
    }
    message.error(resp.msg || "终止任务失败");
  };

  const selectedCount = selectedSourceIds.length;

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集站"
        description="统一管理主采集站、附属采集站与采集任务。"
        actions={
          <>
            <Button
              icon={<ClearOutlined />}
              loading={cleanupScanning}
              onClick={() => void startCleanupScan()}
            >
              清理失效源
            </Button>
            <Popconfirm
              title="一键终止所有采集"
              description="确定要强制终止当前所有正在运行的采集任务吗？"
              onConfirm={() => void submitStopAllTasks()}
              okText="确认终止"
              cancelText="取消"
              okButtonProps={{ danger: true }}
              disabled={activeCollectIds.length === 0}
            >
              <Button
                danger
                icon={<PauseOutlined />}
                disabled={activeCollectIds.length === 0}
              >
                终止全部任务
              </Button>
            </Popconfirm>
            <Button type="primary" icon={<PlusOutlined />} onClick={openAddDialog}>
              新增采集站
            </Button>
          </>
        }
      />

      <div className={styles.layout}>
        <CollectOverview
          stats={stats}
          masterSite={masterSite}
          masterStatus={masterStatus}
        />

        <div className={styles.cardPanel}>
          <Card size="small" className={styles.toolbarCard} styles={{ body: { padding: 12 } }}>
            <div className={styles.toolbar}>
              <Space size={[8, 8]} wrap>
                <span className={styles.toolbarHint}>
                  共 {siteList.length} 个采集站
                  {selectedCount > 0 ? ` · 已选 ${selectedCount}` : ""}
                </span>
                <Button onClick={selectAllSources}>全选</Button>
                <Button onClick={invertSelection}>反选</Button>
                <Button disabled={selectedCount === 0} onClick={clearSelection}>
                  清空选择
                </Button>
              </Space>
              <Space size={[8, 8]} wrap>
                <Button
                  loading={batchStateUpdating}
                  disabled={selectedCount === 0}
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
                    disabled={selectedCount === 0}
                  >
                    批量禁用{selectedCount > 0 ? ` (${selectedCount})` : ""}
                  </Button>
                </Popconfirm>
                <Button
                  type="primary"
                  ghost
                  disabled={selectedCount === 0}
                  onClick={() => void openBatchCollect()}
                >
                  批量采集{selectedCount > 0 ? ` (${selectedCount})` : ""}
                </Button>
              </Space>
            </div>
          </Card>

          {siteList.length > 0 ? (
            <div className={styles.sourceGroups}>
              {/* 主采集站：横向操作条（唯一配置） */}
              <section className={styles.masterSection}>
                <header className={styles.sectionHead}>
                  <h3 className={styles.sectionTitle}>主采集站</h3>
                  <span className={styles.sectionHint}>全局唯一数据源</span>
                  {masterSites.length > 1 ? (
                    <span className={styles.sectionWarn}>
                      当前有 {masterSites.length} 个，请只保留一个
                    </span>
                  ) : null}
                </header>
                {masterSites.length > 0 ? (
                  <div className={styles.masterList}>
                    {masterSites.map((site) => (
                      <CollectMasterPanel
                        key={site.id}
                        record={site}
                        selected={selectedSourceIds.includes(site.id)}
                        active={activeCollectIds.includes(site.id)}
                        onSelect={handleSelectSource}
                        onChangeCollectDuration={changeCollectDuration}
                        onStartTask={(record) => void startTask(record)}
                        onTerminateTask={(id) => void stopTask(id)}
                        onEditSource={(id) => void openEditDialog(id)}
                        onDeleteSource={(id) => void delSource(id)}
                      />
                    ))}
                  </div>
                ) : (
                  <div className={styles.masterEmpty}>
                    未配置主采集站。新增采集站时将类型设为「主采集站」。
                  </div>
                )}
              </section>

              {/* 附属采集站：多源卡片网格 */}
              <section className={styles.affiliateSection}>
                <header className={styles.sectionHead}>
                  <h3 className={styles.sectionTitle}>附属采集站</h3>
                  <span className={styles.sectionCount}>{affiliateSites.length}</span>
                </header>
                {affiliateSites.length > 0 ? (
                  <div className={styles.cardGrid}>
                    {affiliateSites.map((site) => (
                      <CollectSourceCard
                        key={site.id}
                        record={site}
                        selected={selectedSourceIds.includes(site.id)}
                        active={activeCollectIds.includes(site.id)}
                        onSelect={handleSelectSource}
                        onChangeCollectDuration={changeCollectDuration}
                        onStartTask={(record) => void startTask(record)}
                        onTerminateTask={(id) => void stopTask(id)}
                        onEditSource={(id) => void openEditDialog(id)}
                        onDeleteSource={(id) => void delSource(id)}
                      />
                    ))}
                  </div>
                ) : (
                  <div className={styles.groupEmpty}>暂无附属采集站</div>
                )}
              </section>
            </div>
          ) : (
            <div className={styles.emptyCard}>
              <Empty
                description={
                  loading
                    ? "采集站加载中…"
                    : "暂无采集站，点击「新增采集站」开始配置"
                }
              />
            </div>
          )}
        </div>
      </div>

      <SourceFormModal
        open={sourceModalOpen}
        mode={sourceModalMode}
        loading={submitting}
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
