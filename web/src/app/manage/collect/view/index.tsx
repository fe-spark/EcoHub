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
  Flex,
  Form,
  Popconfirm,
  Space,
  Table,
} from "antd";
import { PauseOutlined, PlusOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import BatchCollectModal from "./batch-collect-modal";
import { createCollectTableColumns } from "./collect-table-columns";
import CollectOverview from "./collect-overview";
import SourceFormModal from "./source-form-modal";
import {
  type BatchOption,
  type FilmSource,
  type SourceFormValues,
} from "./types";
import styles from "./index.module.less";

interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
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

  const activeCollectIds = useMemo(
    () => siteList.filter((item) => item.progress).map((item) => item.id),
    [siteList],
  );

  const startingCollectIds = useMemo(
    () => siteList.filter((item) => item.progress?.status === "starting").map((item) => item.id),
    [siteList],
  );

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      running: siteList.filter((item) => item.progress?.status === "running" || item.progress?.status === "finalizing").length,
      waiting: startingCollectIds.length,
      masters: siteList.filter((item) => item.grade === 0).length,
    }),
    [siteList, startingCollectIds.length],
  );

  const masterSite = useMemo(
    () => siteList.find((item) => item.grade === 0) ?? null,
    [siteList],
  );

const masterStatus = useMemo(() => {
    if (stats.masters === 1) {
      return { text: "正常", color: "success" as const };
    }
    if (stats.masters === 0) {
      return { text: "缺少主站", color: "warning" as const };
    }
    return { text: `${stats.masters} 个主站`, color: "error" as const };
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

  const changeSourceState = async (record: FilmSource) => {
    const resp = await ApiPost("/manage/collect/change", {
      id: record.id,
      state: record.state,
      syncPictures: record.syncPictures,
    });
    if (resp.code !== 0) {
      message.error(resp.msg || "状态更新失败");
      await getCollectList();
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
      message.info(state ? "选中站点已全部启用" : "选中站点已全部禁用");
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
        message.success(`已${state ? "启用" : "禁用"} ${sourceIdsToUpdate.length} 个站点`);
      }
      await getCollectList();
    } finally {
      setBatchStateUpdating(false);
    }
  };

  const startTask = async (record: FilmSource) => {
    const collectTime = collectDurationOverridesRef.current[record.id] ?? record.cd ?? 24;
    const resp = await ApiPost("/manage/spider/start", {
      id: record.id,
      time: collectTime,
      batch: false,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      await getCollectList();
      return;
    }
    message.error(resp.msg || "启动采集失败");
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
    message.error(resp.msg || "获取站点信息失败");
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
      message.error(resp.msg || "保存站点失败");
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
        message.warning("请先选择要采集的站点");
        return;
      }
      if (selectedEnabledIds.length === 0) {
        message.warning("选中的站点均未启用，无法批量采集");
        return;
      }
      const options = allOptions.filter((item) => selectedEnabledIds.includes(item.id));
      setBatchOptions(options);
      setBatchIds(selectedEnabledIds);
      setBatchOpen(true);
      return;
    }
    message.error(resp.msg || "加载批量采集站点失败");
  };

  const startBatchCollect = async () => {
    if (batchIds.length === 0) {
      message.warning("请至少选择一个站点");
      return;
    }
    const resp = await ApiPost("/manage/spider/start", {
      ids: batchIds,
      time: batchTime,
      batch: true,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setBatchOpen(false);
      await getCollectList();
      return;
    }
    message.error(resp.msg || "批量采集启动失败");
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

  const columns = createCollectTableColumns({
    activeCollectIds,
    onUpdateItem: updateSiteListItem,
    onChangeCollectDuration: changeCollectDuration,
    onChangeSourceState: (record) => void changeSourceState(record),
    onStartTask: (record) => void startTask(record),
    onTerminateTask: (id) => void stopTask(id),
    onEditSource: (id) => void openEditDialog(id),
    onDeleteSource: (id) => void delSource(id),
  });

  const selectedCount = selectedSourceIds.length;

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集站点"
        description="统一管理主站、附属站与采集任务。"
      />

      <div className={styles.layout}>
        <CollectOverview
          stats={stats}
          masterSite={masterSite}
          masterStatus={masterStatus}
        />

        <Table
          rowKey="id"
          size="middle"
          rowSelection={{
            selectedRowKeys: selectedSourceIds,
            onChange: setSelectedSourceIds,
          }}
          columns={columns}
          dataSource={siteList}
          loading={loading}
          pagination={false}
          scroll={{ x: "max-content" }}
          rowClassName={(record) =>
            selectedSourceIds.includes(record.id) ? styles.selectedRow : ""
          }
          className={styles.tableBlock}
          title={() => (
            <div className={styles.tableHeader}>
              <Space size={[12, 8]} wrap>
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
                    description="禁用后会停止选中站点的后续请求，已请求数据会继续入库，并阻止后续批量/自动采集调度。"
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
                    disabled={selectedCount === 0}
                    onClick={() => void openBatchCollect()}
                  >
                    批量采集{selectedCount > 0 ? ` (${selectedCount})` : ""}
                  </Button>
                </Space>
              </Space>
              <Space size={[8, 8]} wrap className={styles.tableActions}>
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={openAddDialog}
                >
                  新增站点
                </Button>
              </Space>
            </div>
          )}
          footer={() => (
            <Flex justify="flex-end">
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
            </Flex>
          )}
        />
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

    </div>
  );
}
