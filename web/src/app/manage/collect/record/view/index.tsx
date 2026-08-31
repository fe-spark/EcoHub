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
import styles from "./index.module.less";

const { RangePicker } = DatePicker;

export default function FailureRecordPageView() {
  const [records, setRecords] = useState<FailRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [queuedRetryIds, setQueuedRetryIds] = useState<Set<number>>(() => new Set());
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
  });

  const pageRef = useRef(page);
  const paramsRef = useRef(params);
  useEffect(() => {
    pageRef.current = page;
    paramsRef.current = params;
  }, [page, params]);

  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const getRecords = useCallback(
    async (p?: any, overrideParams?: any, silent = false) => {
      if (!silent) {
        setLoading(true);
      }
      const pg = p || pageRef.current;
      const reqParams = overrideParams || paramsRef.current;
      try {
        const resp = await ApiGet("/manage/collect/record/list", {
          ...reqParams,
          current: pg.current,
          pageSize: pg.pageSize,
        });
        if (resp.code === 0) {
          setRecords(resp.data.list || []);
          if (resp.data.params?.paging) {
            setPage((prev) => ({
              ...prev,
              ...resp.data.params.paging,
            }));
          }
          if (resp.data.options) {
            setOptions(resp.data.options);
          }
        }
      } finally {
        if (!silent) {
          setLoading(false);
        }
      }
    },
    [],
  );

  useEffect(() => {
    void getRecords();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRetry = useCallback(
    async (id: number) => {
      setQueuedRetryIds((prev) => new Set(prev).add(id));
      const resp = await ApiPost("/manage/collect/record/retry", { id });
      if (resp.code === 0) {
        message.success("重试任务已加入队列");
        window.setTimeout(() => {
          setQueuedRetryIds((prev) => {
            const next = new Set(prev);
            next.delete(id);
            return next;
          });
          void getRecords(undefined, undefined, true);
        }, 3000);
      } else {
        setQueuedRetryIds((prev) => {
          const next = new Set(prev);
          next.delete(id);
          return next;
        });
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

    const targetIds = targets.map((t) => t.ID);
    setQueuedRetryIds((prev) => {
      const next = new Set(prev);
      targetIds.forEach((id) => next.add(id));
      return next;
    });

    setBatchRetrying(true);
    let successCount = 0;
    let failCount = 0;

    for (const target of targets) {
      const resp = await ApiPost("/manage/collect/record/retry", { id: target.ID });
      if (resp.code === 0) {
        successCount++;
      } else {
        failCount++;
      }
    }

    setBatchRetrying(false);
    setSelectedRowKeys([]);
    message.success(`已提交重试任务：成功 ${successCount} 条，失败 ${failCount} 条`);

    window.setTimeout(() => {
      setQueuedRetryIds((prev) => {
        const next = new Set(prev);
        targetIds.forEach((id) => next.delete(id));
        return next;
      });
      void getRecords(undefined, undefined, true);
    }, 3000);
  };

  const handleRetryAll = async () => {
    const pendingIds = records
      .filter((r) => r.status === FAILURE_RECORD_STATUS.pending)
      .map((r) => r.ID);
    setQueuedRetryIds((prev) => {
      const next = new Set(prev);
      pendingIds.forEach((id) => next.add(id));
      return next;
    });

    const resp = await ApiPost("/manage/collect/record/retry/all", {});
    if (resp.code !== 0) {
      setQueuedRetryIds((prev) => {
        const next = new Set(prev);
        pendingIds.forEach((id) => next.delete(id));
        return next;
      });
      message.error(resp.msg);
      return;
    }

    message.success(resp.msg || "已触发全量待处理项重试，系统正在后台并发执行");

    window.setTimeout(() => {
      setQueuedRetryIds((prev) => {
        const next = new Set(prev);
        pendingIds.forEach((id) => next.delete(id));
        return next;
      });
      void getRecords(undefined, undefined, true);
    }, 4000);
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
              originId: "",
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

              <Tooltip title="并发拉取所有状态为「待自动重试」的页面进行重试采集">
                <Popconfirm
                  title="确认重试所有待处理记录？"
                  description="系统将并发拉取所有待自动重试状态的页面，已成功和已超限失败的记录不受影响。"
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
