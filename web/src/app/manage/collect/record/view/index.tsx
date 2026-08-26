"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  Table,
  Button,
  Space,
  Select,
  DatePicker,
  Popconfirm,
  Pagination,
  Tooltip,
} from "antd";
import {
  SearchOutlined,
  ReloadOutlined,
  DeleteOutlined,
  ClearOutlined,
  QuestionCircleOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import { FailRecord, FAILURE_RECORD_STATUS, RetryProgressState } from "./types";
import { getRecordColumns, normalizeStatusOptionLabel } from "./columns";
import RetryProgressCard from "./retry-progress-card";
import styles from "./index.module.less";

const { RangePicker } = DatePicker;

export default function FailureRecordPageView() {
  const [records, setRecords] = useState<FailRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [queuedRetryIds, setQueuedRetryIds] = useState<Set<number>>(() => new Set());
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [batchRetrying, setBatchRetrying] = useState(false);
  const [progressState, setProgressState] = useState<RetryProgressState | null>(null);
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });
  const [params, setParams] = useState({
    originId: "",
    status: -1,
    beginTime: "",
    endTime: "",
  });
  const [dateRange, setDateRange] = useState<any>(null);
  const [options, setOptions] = useState<any>({
    origin: [],
    status: [],
  });
  const pollTimerRef = useRef<number | null>(null);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const getRecords = useCallback(
    async (p?: any, overrideParams?: any) => {
      setLoading(true);
      const pg = p || page;
      const reqParams = overrideParams || params;
      try {
        const resp = await ApiGet("/manage/collect/record/list", {
          ...reqParams,
          current: pg.current,
          pageSize: pg.pageSize,
        });
        if (resp.code === 0) {
          setRecords(resp.data.list || []);
          if (resp.data.params?.paging) {
            setPage(resp.data.params.paging);
          }
          if (resp.data.options) {
            setOptions(resp.data.options);
          }
        }
      } finally {
        setLoading(false);
      }
    },
    [params, page],
  );

  useEffect(() => {
    void getRecords();
    return () => {
      if (pollTimerRef.current) {
        window.clearInterval(pollTimerRef.current);
      }
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRetry = useCallback(
    async (id: number) => {
      const resp = await ApiPost("/manage/collect/record/retry", { id });
      if (resp.code === 0) {
        setQueuedRetryIds((prev) => new Set(prev).add(id));
        message.success("重试任务已加入队列；若对应采集站正在运行，将在采集结束后自动执行");
        window.setTimeout(() => {
          setQueuedRetryIds((prev) => {
            const next = new Set(prev);
            next.delete(id);
            return next;
          });
        }, 5000);
        void getRecords();
      } else {
        message.error(resp.msg);
      }
    },
    [getRecords, message],
  );

  const handleRetrySelected = async () => {
    const targets = records.filter(
      (r) => selectedRowKeys.includes(r.ID) && r.status !== FAILURE_RECORD_STATUS.success,
    );
    if (targets.length === 0) {
      message.warning("所选记录均已重试成功，无需重复重试");
      return;
    }

    const total = targets.length;
    setBatchRetrying(true);
    setProgressState({
      active: true,
      type: "selected",
      total,
      completed: 0,
      successCount: 0,
      failCount: 0,
      text: `正在提交选中重试任务 (0/${total})`,
    });

    const targetIds = targets.map((t) => t.ID);
    setQueuedRetryIds((prev) => {
      const next = new Set(prev);
      targetIds.forEach((id) => next.add(id));
      return next;
    });

    let successCount = 0;
    let failCount = 0;

    for (let i = 0; i < targets.length; i++) {
      const target = targets[i];
      const resp = await ApiPost("/manage/collect/record/retry", { id: target.ID });
      if (resp.code === 0) {
        successCount++;
      } else {
        failCount++;
      }
      const completed = i + 1;
      setProgressState({
        active: true,
        type: "selected",
        total,
        completed,
        successCount,
        failCount,
        text:
          completed === total
            ? `已完成提交全部选中项 (${completed}/${total})`
            : `正在提交重试任务 (${completed}/${total})`,
      });
    }

    setBatchRetrying(false);
    setSelectedRowKeys([]);
    message.success(`已完成批量重试提交：成功 ${successCount} 条，失败 ${failCount} 条`);
    void getRecords();

    window.setTimeout(() => {
      setProgressState((prev) => (prev ? { ...prev, active: false } : null));
    }, 4000);

    window.setTimeout(() => {
      setQueuedRetryIds((prev) => {
        const next = new Set(prev);
        targetIds.forEach((id) => next.delete(id));
        return next;
      });
    }, 5000);
  };

  const handleRetryAll = async () => {
    const pendingResp = await ApiGet("/manage/collect/record/list", {
      status: FAILURE_RECORD_STATUS.pending,
      current: 1,
      pageSize: 1,
    });
    const totalPending = pendingResp.data?.params?.paging?.total || 0;

    const resp = await ApiPost("/manage/collect/record/retry/all", {});
    if (resp.code !== 0) {
      message.error(resp.msg);
      return;
    }

    message.success(resp.msg || "已触发全量待处理项重试任务");

    if (totalPending <= 0) {
      void getRecords();
      return;
    }

    setProgressState({
      active: true,
      type: "all",
      total: totalPending,
      completed: 0,
      successCount: 0,
      failCount: 0,
      text: `全量重试执行中，初始待处理 ${totalPending} 条...`,
    });

    if (pollTimerRef.current) {
      window.clearInterval(pollTimerRef.current);
    }

    let pollCount = 0;
    pollTimerRef.current = window.setInterval(async () => {
      pollCount++;
      const checkResp = await ApiGet("/manage/collect/record/list", {
        status: FAILURE_RECORD_STATUS.pending,
        current: 1,
        pageSize: 1,
      });
      const currentRemaining = checkResp.data?.params?.paging?.total ?? 0;
      const completedCount = Math.max(0, totalPending - currentRemaining);
      const isDone = currentRemaining === 0 || pollCount >= 24;

      setProgressState({
        active: true,
        type: "all",
        total: totalPending,
        completed: isDone ? totalPending : completedCount,
        successCount: completedCount,
        failCount: 0,
        text: isDone
          ? `全量重试已完成 (${totalPending}/${totalPending})`
          : `全量重试执行中: 剩余 ${currentRemaining} 条待处理 (${completedCount}/${totalPending})`,
      });

      void getRecords();

      if (isDone) {
        if (pollTimerRef.current) {
          window.clearInterval(pollTimerRef.current);
          pollTimerRef.current = null;
        }
        window.setTimeout(() => {
          setProgressState((prev) => (prev ? { ...prev, active: false } : null));
        }, 4000);
      }
    }, 2500);
  };

  const handleCleanResult = async () => {
    const resp = await ApiPost("/manage/collect/record/clear/result", {});
    if (resp.code === 0) {
      message.success(resp.msg || "已清理所有已完结（成功/最终失败）记录");
      setSelectedRowKeys([]);
      void getRecords();
    } else {
      message.error(resp.msg);
    }
  };

  const handleCleanAll = async () => {
    const resp = await ApiPost("/manage/collect/record/clear/all", {});
    if (resp.code === 0) {
      message.success(resp.msg || "已清空所有失败记录");
      setSelectedRowKeys([]);
      void getRecords();
    } else {
      message.error(resp.msg);
    }
  };

  const columns = useMemo(
    () =>
      getRecordColumns({
        canWrite,
        queuedRetryIds,
        onRetry: handleRetry,
      }),
    [canWrite, queuedRetryIds, handleRetry],
  );

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="失败记录"
        description="管理影视采集失败记录。支持针对性重试待处理页码、清理已完结归档，以及全表数据维护。"
      />

      <Space size={[8, 8]} wrap className={styles.filterBar}>
        <Select
          placeholder="采集源"
          value={params.originId || undefined}
          onChange={(v) => setParams({ ...params, originId: v })}
          options={options.origin?.map((o: any) => ({
            label: o.name,
            value: o.value,
          }))}
          className={styles.filterSelect}
          allowClear
        />
        <Select
          placeholder="记录状态"
          value={params.status}
          onChange={(v) => setParams({ ...params, status: v })}
          options={options.status?.map((o: any) => ({
            label: normalizeStatusOptionLabel(o.name, o.value),
            value: o.value,
          }))}
          className={styles.statusSelect}
        />
        <RangePicker
          showTime
          value={dateRange}
          className={styles.dateRange}
          onChange={(dates) => {
            setDateRange(dates);
            if (dates && dates[0] && dates[1]) {
              setParams({
                ...params,
                beginTime: dates[0].format("YYYY-MM-DD HH:mm:ss"),
                endTime: dates[1].format("YYYY-MM-DD HH:mm:ss"),
              });
            } else {
              setParams({ ...params, beginTime: "", endTime: "" });
            }
          }}
        />
        <Button
          type="primary"
          icon={<SearchOutlined />}
          onClick={() => {
            const newPage = { ...page, current: 1 };
            setPage(newPage);
            void getRecords(newPage, params);
          }}
          className={styles.searchButton}
        >
          搜索
        </Button>
        <Button
          icon={<ReloadOutlined />}
          onClick={() => {
            const defaultParams = {
              originId: "",
              status: -1,
              beginTime: "",
              endTime: "",
            };
            setParams(defaultParams);
            setDateRange(null);
            const newPage = { ...page, current: 1 };
            setPage(newPage);
            void getRecords(newPage, defaultParams);
          }}
        >
          重置
        </Button>
      </Space>

      <RetryProgressCard progressState={progressState} />

      <Table
        bordered
        columns={columns}
        dataSource={records}
        rowKey="ID"
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: "max-content" }}
        rowSelection={
          canWrite
            ? {
                selectedRowKeys,
                onChange: (keys) => setSelectedRowKeys(keys),
              }
            : undefined
        }
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>
              失败记录列表
              {selectedRowKeys.length > 0 && (
                <span className={styles.selectionCount}>（已选中 {selectedRowKeys.length} 项）</span>
              )}
            </div>
            <Space size={[8, 8]} wrap className={styles.tableActions}>
              {selectedRowKeys.length > 0 && (
                <Button
                  type="primary"
                  icon={<ReloadOutlined />}
                  loading={batchRetrying}
                  disabled={!canWrite}
                  onClick={handleRetrySelected}
                >
                  重试选中项 ({selectedRowKeys.length})
                </Button>
              )}

              <Tooltip title="并发拉取所有状态为「待自动重试」的页面进行重试采集">
                <Popconfirm
                  title="确认重试所有待处理记录？"
                  description="系统将并发拉取所有待自动重试状态的页面，已成功和已超限失败的记录不受影响。"
                  icon={<QuestionCircleOutlined style={{ color: "#1677ff" }} />}
                  onConfirm={handleRetryAll}
                  disabled={!canWrite || progressState?.active}
                >
                  <Button
                    type="primary"
                    icon={<ReloadOutlined />}
                    disabled={!canWrite || progressState?.active}
                    loading={progressState?.active && progressState.type === "all"}
                  >
                    重试全部待处理
                  </Button>
                </Popconfirm>
              </Tooltip>

              <Tooltip title="安全清理：删除数据库中所有「重试成功」与「最终失败」的历史记录，保留待重试项">
                <Popconfirm
                  title="确认清理所有已完结记录？"
                  description="将删除所有已成功和最终失败的历史归档，待自动重试的记录将继续保留。"
                  icon={<QuestionCircleOutlined style={{ color: "var(--ant-color-warning)" }} />}
                  onConfirm={handleCleanResult}
                  disabled={!canWrite}
                >
                  <Button
                    icon={<ClearOutlined />}
                    disabled={!canWrite}
                    style={{
                      color: "var(--ant-color-warning)",
                      borderColor: "var(--ant-color-warning)",
                    }}
                  >
                    清理已完结记录
                  </Button>
                </Popconfirm>
              </Tooltip>

              <Tooltip title="高危操作：清空失败记录全表数据（包括所有待重试项）">
                <Popconfirm
                  title="确认清空全部失败记录？"
                  description="警告：此操作将永久清空表中所有记录（包括待处理项），不可恢复！"
                  okType="danger"
                  okText="确定清空"
                  cancelText="取消"
                  onConfirm={handleCleanAll}
                  disabled={!canWrite}
                >
                  <Button danger icon={<DeleteOutlined />} disabled={!canWrite}>
                    清空全部记录
                  </Button>
                </Popconfirm>
              </Tooltip>
            </Space>
          </div>
        )}
        footer={() => (
          <div className={styles.pagination}>
            <Pagination
              current={page.current}
              pageSize={page.pageSize}
              total={page.total}
              showSizeChanger
              showTotal={(total) => `共 ${total} 条`}
              pageSizeOptions={[10, 20, 50, 100, 500]}
              onChange={(current, pageSize) => {
                const newPage = { ...page, current, pageSize };
                setPage(newPage);
                void getRecords(newPage);
              }}
            />
          </div>
        )}
      />
    </div>
  );
}
