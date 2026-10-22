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
import { FailRecord, FAILURE_RECORD_STATUS } from "./types";
import { getRecordColumns, normalizeStatusOptionLabel } from "./columns";
import { applyRecordRetry, applyRecordSnapshot, RecordRetryEvent } from "./record-retry-state";
import { useRecordStream } from "./use-record-stream";
import styles from "./index.module.less";

const { RangePicker } = DatePicker;

export default function FailureRecordPageView() {
  const [records, setRecords] = useState<FailRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [retryingIds, setRetryingIds] = useState<Set<number>>(() => new Set());
  const [heldIds, setHeldIds] = useState<Set<number>>(() => new Set());
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [batchRetrying, setBatchRetrying] = useState(false);
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
    defaultSourceId: "",
  });

  const pageRef = useRef(page);
  const paramsRef = useRef(params);
  const recordsRef = useRef(records);
  const retryingRef = useRef(retryingIds);
  pageRef.current = page;
  paramsRef.current = params;
  recordsRef.current = records;
  retryingRef.current = retryingIds;

  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const getRecords = useCallback(async (p?: any, overrideParams?: any) => {
    setSelectedRowKeys([]);
    setLoading(true);
    const pg = p || pageRef.current;
    const reqParams = overrideParams || paramsRef.current;
    try {
      const resp = await ApiGet("/manage/collect/record/list", {
        ...reqParams,
        current: pg.current,
        pageSize: pg.pageSize,
      });
      if (resp.code === 0) {
        const list = resp.data.list || [];
        recordsRef.current = list;
        setRecords(list);
        if (resp.data.params?.paging) {
          setPage((prev) => ({
            ...prev,
            ...resp.data.params.paging,
          }));
        }
        if (resp.data.options) {
          setOptions(resp.data.options);
        }
        const resolved =
          reqParams.originId ||
          resp.data.params?.originId ||
          resp.data.options?.defaultSourceId ||
          "";
        if (resolved && reqParams.originId !== resolved) {
          setParams((prev) => ({ ...prev, originId: resolved }));
        }
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void getRecords();
  }, [getRecords]);

  const releaseHold = useCallback((id: number) => {
    setHeldIds((prev) => {
      if (!prev.has(id)) {
        return prev;
      }
      const next = new Set(prev);
      next.delete(id);
      return next;
    });
  }, []);

  const onSnapshot = useCallback((inflightIds: number[]) => {
    const pageIds = recordsRef.current.map((row) => row.ID);
    const next = applyRecordSnapshot(pageIds, inflightIds);
    retryingRef.current = next;
    setRetryingIds(next);
    setHeldIds((prev) => {
      if (prev.size === 0) {
        return prev;
      }
      const keep = new Set<number>();
      for (const id of prev) {
        if (next.has(id)) {
          keep.add(id);
        }
      }
      return keep.size === prev.size ? prev : keep;
    });
  }, []);

  const onRecord = useCallback((ev: RecordRetryEvent) => {
    releaseHold(ev.id);
    const view = applyRecordRetry(recordsRef.current, retryingRef.current, ev);
    recordsRef.current = view.records;
    retryingRef.current = view.retrying;
    setRecords(view.records);
    setRetryingIds(view.retrying);
    if (ev.hasResult && ev.status === FAILURE_RECORD_STATUS.success) {
      setSelectedRowKeys((prev) => prev.filter((key) => Number(key) !== ev.id));
    }
  }, [releaseHold]);

  const recordIds = useMemo(() => records.map((row) => row.ID), [records]);
  useRecordStream(recordIds, { onSnapshot, onRecord });

  const handleRetry = useCallback(
    async (id: number) => {
      setHeldIds((prev) => new Set(prev).add(id));
      const resp = await ApiPost("/manage/collect/record/retry", { ids: [id] });
      if (resp.code === 0) {
        message.success("重试任务已加入队列");
        return;
      }
      if (resp.msg !== "正在重试中") {
        releaseHold(id);
      }
      message.error(resp.msg);
    },
    [message, releaseHold],
  );

  const handleRetrySelected = async () => {
    const selectedIds = new Set(selectedRowKeys.map((key) => Number(key)));
    const targets = records.filter(
      (r) => selectedIds.has(r.ID) && r.status !== FAILURE_RECORD_STATUS.success,
    );
    if (targets.length === 0) {
      message.warning("所选记录均已重试成功，无需重复重试");
      return;
    }

    const targetIds = targets.map((t) => t.ID);
    setHeldIds((prev) => {
      const next = new Set(prev);
      targetIds.forEach((id) => next.add(id));
      return next;
    });

    setBatchRetrying(true);
    try {
      const resp = await ApiPost("/manage/collect/record/retry", { ids: targetIds });
      if (resp.code === 0) {
        message.success(`已提交 ${targetIds.length} 条重试任务`);
        setSelectedRowKeys([]);
      } else {
        message.error(resp.msg || "批量重试提交失败");
        targetIds.forEach((id) => releaseHold(id));
      }
    } catch {
      message.error("批量重试提交失败");
      targetIds.forEach((id) => releaseHold(id));
    } finally {
      setBatchRetrying(false);
    }
  };

  const reloadFirstPage = () => {
    const firstPage = { ...pageRef.current, current: 1 };
    setPage(firstPage);
    void getRecords(firstPage);
  };

  const handleRetryAll = async () => {
    const resp = await ApiPost<{ queued?: number; already?: number; skipped?: number }>(
      "/manage/collect/record/retry",
      { all: true },
    );
    if (resp.code !== 0) {
      message.error(resp.msg);
      return;
    }
    const queued = Number(resp.data?.queued || 0);
    const already = Number(resp.data?.already || 0);
    const skipped = Number(resp.data?.skipped || 0);
    if (queued === 0 || already > 0 || skipped > 0) {
      message.warning(resp.msg);
    } else {
      message.success(resp.msg);
    }
  };

  const handleCleanResult = async () => {
    const resp = await ApiPost("/manage/collect/record/clear", { scope: "retried" });
    if (resp.code === 0) {
      message.success(resp.msg || "已清理所有已完结记录");
      reloadFirstPage();
    } else {
      message.error(resp.msg);
    }
  };

  const handleCleanAll = async () => {
    const resp = await ApiPost("/manage/collect/record/clear", { scope: "all" });
    if (resp.code === 0) {
      message.success(resp.msg || "已清空所有失败记录");
      reloadFirstPage();
    } else {
      message.error(resp.msg);
    }
  };

  const handleSourceChange = (value: string) => {
    const next = { ...paramsRef.current, originId: value };
    setParams(next);
    const newPage = { ...pageRef.current, current: 1 };
    setPage(newPage);
    void getRecords(newPage, next);
  };

  const queuedRetryIds = useMemo(() => {
    const next = new Set(retryingIds);
    heldIds.forEach((id) => next.add(id));
    return next;
  }, [retryingIds, heldIds]);

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
          placeholder="选择采集源"
          value={params.originId || undefined}
          onChange={handleSourceChange}
          options={(options.origin || [])
            .filter((o: any) => o.value !== "" && o.name !== "全部")
            .map((o: any) => ({
              label: o.name,
              value: o.value,
            }))}
          className={styles.filterSelect}
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
            const newPage = { ...pageRef.current, current: 1 };
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
              originId: options.defaultSourceId || "",
              status: -1,
              beginTime: "",
              endTime: "",
            };
            setParams(defaultParams);
            setDateRange(null);
            const newPage = { ...pageRef.current, current: 1 };
            setPage(newPage);
            void getRecords(newPage, defaultParams);
          }}
        >
          重置
        </Button>
      </Space>

      <Table
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
                getCheckboxProps: (record) => ({
                  disabled:
                    queuedRetryIds.has(record.ID) ||
                    record.status === FAILURE_RECORD_STATUS.success,
                }),
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

              <Tooltip title="把所有采集站中「待自动重试」的记录加入队列。正在采集的站点会跳过">
                <Popconfirm
                  title="重试全部待处理记录？"
                  icon={<QuestionCircleOutlined style={{ color: "#1677ff" }} />}
                  onConfirm={handleRetryAll}
                  disabled={!canWrite}
                >
                  <Button type="primary" icon={<ReloadOutlined />} disabled={!canWrite}>
                    重试全部待处理
                  </Button>
                </Popconfirm>
              </Tooltip>

              <Tooltip title="安全清理：删除数据库中所有「重试成功」与「最终失败」的历史记录，保留待重试项">
                <Popconfirm
                  title="清理已完结记录？"
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
                  title="清空全部失败记录？"
                  description="不可恢复"
                  okType="danger"
                  okText="清空"
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
                const newPage = { ...pageRef.current, current, pageSize };
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
