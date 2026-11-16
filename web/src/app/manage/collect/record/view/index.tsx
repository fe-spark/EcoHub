"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  Table,
  Button,
  Popconfirm,
  Pagination,
  Segmented,
} from "antd";
import {
  DeleteOutlined,
  ClearOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import type { Dayjs } from "dayjs";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import { CollectHistoryRecord } from "./types";
import { BatchDetailDrawer } from "./batch-detail-drawer";
import { getHistoryColumns } from "./history-columns";
import { RecordFilterBar, FilterParams } from "./record-filter-bar";
import styles from "./index.module.less";

export default function CollectRecordPageView() {
  const [records, setRecords] = useState<CollectHistoryRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [refreshSeconds, setRefreshSeconds] = useState(10);
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [batchDeleting, setBatchDeleting] = useState(false);
  const [retryingBatchIds, setRetryingBatchIds] = useState<Set<string>>(() => new Set());
  const [retryingSourceKeys, setRetryingSourceKeys] = useState<Set<string>>(() => new Set());
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });
  const [params, setParams] = useState<FilterParams>({
    trigger: "",
    status: "",
    beginTime: "",
    endTime: "",
    updated: "",
    failed: "",
  });
  const [dateRange, setDateRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);

  // 批次成果抽屉状态
  const [activeRecord, setActiveRecord] = useState<CollectHistoryRecord | null>(null);
  const [detailDrawerOpen, setDetailDrawerOpen] = useState(false);

  const pageRef = useRef(page);
  const paramsRef = useRef(params);
  const requestSeq = useRef(0);
  const activeRecordRef = useRef(activeRecord);
  pageRef.current = page;
  paramsRef.current = params;
  activeRecordRef.current = activeRecord;

  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const getRecords = useCallback(async (
    p?: { current: number; pageSize: number },
    overrideParams?: typeof params,
    options?: { silent?: boolean },
  ) => {
    const seq = ++requestSeq.current;
    const silent = options?.silent === true;
    if (!silent) {
      setSelectedRowKeys([]);
      setLoading(true);
    }
    const pg = p || pageRef.current;
    const reqParams = overrideParams || paramsRef.current;
    try {
      const resp = await ApiGet<{ list: CollectHistoryRecord[]; total: number; current: number; pageSize: number }>(
        "/manage/collect/history/list",
        {
          ...reqParams,
          current: pg.current,
          pageSize: pg.pageSize,
        },
      );
      if (seq !== requestSeq.current) return;
      if (resp.code === 0 && resp.data) {
        const list = resp.data.list || [];
        setRecords(list);
        const openRecord = activeRecordRef.current;
        const stillListed = openRecord ? list.some((item) => item.batchId === openRecord.batchId) : false;
        setActiveRecord((current) => {
          if (!current) return current;
          return list.find((item) => item.batchId === current.batchId) ?? current;
        });
        if (openRecord && !stillListed) {
          void ApiGet<{ list: CollectHistoryRecord[] }>("/manage/collect/history/list", {
            batchId: openRecord.batchId,
            current: 1,
            pageSize: 1,
          }).then((one) => {
            if (seq !== requestSeq.current) return;
            const row = one.code === 0 ? one.data?.list?.[0] : undefined;
            setActiveRecord((current) => {
              if (!current || current.batchId !== openRecord.batchId) return current;
              return row ?? null;
            });
          });
        }
        setPage((prev) => ({
          ...prev,
          current: resp.data.current || pg.current,
          pageSize: resp.data.pageSize || pg.pageSize,
          total: resp.data.total || 0,
        }));
      }
    } finally {
      if (seq === requestSeq.current) setLoading(false);
    }
  }, []);

  const handleRefresh = useCallback(async () => {
    setRefreshing(true);
    try {
      await getRecords(undefined, undefined, { silent: true });
    } finally {
      setRefreshing(false);
    }
  }, [getRecords]);

  useEffect(() => {
    void getRecords();
  }, [getRecords]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "visible") return;
      void getRecords(undefined, undefined, { silent: true });
    }, refreshSeconds * 1000);
    return () => window.clearInterval(timer);
  }, [getRecords, refreshSeconds]);

  useEffect(() => {
    const onVisible = () => {
      if (document.visibilityState === "visible") {
        void getRecords(undefined, undefined, { silent: true });
      }
    };
    document.addEventListener("visibilitychange", onVisible);
    return () => document.removeEventListener("visibilitychange", onVisible);
  }, [getRecords]);

  const handleOpenDetail = useCallback((record: CollectHistoryRecord) => {
    setActiveRecord(record);
    setDetailDrawerOpen(true);
  }, []);

  const handleCloseDetail = useCallback(() => {
    setDetailDrawerOpen(false);
  }, []);

  const handleRetryBatch = useCallback(
    async (record: CollectHistoryRecord) => {
      setRetryingBatchIds((prev) => new Set(prev).add(record.batchId));
      try {
        const resp = await ApiPost("/manage/collect/history/retry", {
          batchId: record.batchId,
        });
        if (resp.code === 0) {
          message.success(record.trigger === "single_update" ? "已按原影片重新采集" : "已启动失败站点重试");
          void getRecords();
        } else {
          message.error(resp.msg || "重试启动失败");
        }
      } finally {
        setRetryingBatchIds((prev) => {
          const next = new Set(prev);
          next.delete(record.batchId);
          return next;
        });
      }
    },
    [message, getRecords],
  );

  const handleRetrySingleSource = useCallback(
    async (batchId: string, sourceId: string) => {
      const key = `${batchId}:${sourceId}`;
      setRetryingSourceKeys((prev) => new Set(prev).add(key));
      try {
        const resp = await ApiPost("/manage/collect/history/retry", {
          batchId,
          sourceIds: [sourceId],
        });
        if (resp.code === 0) {
          message.success("已启动该站点采集重试");
          void getRecords();
        } else {
          message.error(resp.msg || "重试启动失败");
        }
      } finally {
        setRetryingSourceKeys((prev) => {
          const next = new Set(prev);
          next.delete(key);
          return next;
        });
      }
    },
    [message, getRecords],
  );

  const handleDeleteSelected = async () => {
    if (selectedRowKeys.length === 0) return;
    setBatchDeleting(true);
    try {
      const resp = await ApiPost("/manage/collect/history/delete", {
        ids: selectedRowKeys.map((k) => Number(k)),
      });
      if (resp.code === 0) {
        message.success("选定记录已删除");
        setSelectedRowKeys([]);
        void getRecords();
      } else {
        message.error(resp.msg || "删除失败");
      }
    } finally {
      setBatchDeleting(false);
    }
  };

  const handleClearAll = async () => {
    const resp = await ApiPost("/manage/collect/history/clear");
    if (resp.code === 0) {
      message.success("采集历史已清空");
      setSelectedRowKeys([]);
      const newPage = { ...pageRef.current, current: 1 };
      setPage(newPage);
      void getRecords(newPage);
    } else {
      message.error(resp.msg || "清空失败");
    }
  };

  const handleParamsChange = (newParams: FilterParams) => {
    setParams(newParams);
    const newPage = { ...pageRef.current, current: 1 };
    setPage(newPage);
    void getRecords(newPage, newParams);
  };

  const handleSearch = () => {
    const newPage = { ...pageRef.current, current: 1 };
    setPage(newPage);
    void getRecords(newPage, params);
  };

  const handleReset = () => {
    const defaultParams: FilterParams = {
      trigger: "",
      status: "",
      beginTime: "",
      endTime: "",
      updated: "",
      failed: "",
    };
    setParams(defaultParams);
    setDateRange(null);
    const newPage = { ...pageRef.current, current: 1 };
    setPage(newPage);
    void getRecords(newPage, defaultParams);
  };

  const columns = useMemo(
    () =>
      getHistoryColumns({
        canWrite,
        retryingBatchIds,
        onRetryBatch: handleRetryBatch,
        onOpenDetail: handleOpenDetail,
      }),
    [canWrite, retryingBatchIds, handleRetryBatch, handleOpenDetail],
  );

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        title="采集记录"
        description="查看每一轮采集的结果，以及更新了哪些影片。"
      />

      <RecordFilterBar
        params={params}
        dateRange={dateRange}
        onParamsChange={handleParamsChange}
        onDateRangeChange={setDateRange}
        onSearch={handleSearch}
        onReset={handleReset}
      />

      <Table
          className={styles.recordTable}
          columns={columns}
          dataSource={records}
          rowKey="id"
          rowClassName={(record) => (record.status === "running" ? styles.runningRow : "")}
          onRow={(record) => ({
            style: { cursor: "pointer" },
            onClick: (event) => {
              const target = event.target as HTMLElement | null;
              if (target?.closest("a, button, input, .ant-popover, .ant-checkbox-wrapper")) return;
              handleOpenDetail(record);
            },
          })}
          loading={loading}
          size="middle"
          pagination={false}
          tableLayout="fixed"
          scroll={{ x: 1360 }}
          rowSelection={
            canWrite
              ? {
                  selectedRowKeys,
                  onChange: (keys) => setSelectedRowKeys(keys),
                  getCheckboxProps: (record: CollectHistoryRecord) => ({
                    disabled: record.status === "running",
                  }),
                }
              : undefined
          }
          title={() => (
            <div className={styles.tableHeader}>
              {selectedRowKeys.length > 0 ? (
                <div className={styles.tableTitle}>
                  <span className={styles.selectionCount}>已选 {selectedRowKeys.length}</span>
                </div>
              ) : null}
              <div className={styles.tableActions}>
                <div className={styles.refreshGroup}>
                  <span className={styles.autoRefresh}>自动刷新</span>
                  <Segmented
                    size="small"
                    value={refreshSeconds}
                    onChange={(value) => setRefreshSeconds(Number(value))}
                    options={[
                      { label: "5 秒", value: 5 },
                      { label: "10 秒", value: 10 },
                      { label: "15 秒", value: 15 },
                      { label: "30 秒", value: 30 },
                    ]}
                  />
                </div>
                <Button icon={<ReloadOutlined />} loading={refreshing} onClick={() => void handleRefresh()}>
                  刷新
                </Button>
                {selectedRowKeys.length > 0 && (
                  <Popconfirm
                    title="删除选中记录？"
                    description="确认删除所选的采集运行记录？"
                    okType="danger"
                    onConfirm={handleDeleteSelected}
                    disabled={!canWrite}
                  >
                    <Button danger icon={<DeleteOutlined />} loading={batchDeleting} disabled={!canWrite}>
                      删除选中项 ({selectedRowKeys.length})
                    </Button>
                  </Popconfirm>
                )}

                <Popconfirm
                  title="清空全部历史记录？"
                  description="进行中的批次会保留，其余采集历史删除后不可恢复。"
                  okText="清空"
                  cancelText="取消"
                  okButtonProps={{ danger: true }}
                  onConfirm={handleClearAll}
                  disabled={!canWrite}
                >
                  <Button icon={<ClearOutlined />} disabled={!canWrite}>
                    清空历史
                  </Button>
                </Popconfirm>
              </div>
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
                pageSizeOptions={[10, 20, 50, 100]}
                onChange={(current, pageSize) => {
                  setSelectedRowKeys([]);
                  const newPage = { ...pageRef.current, current, pageSize };
                  setPage(newPage);
                  void getRecords(newPage);
                }}
              />
            </div>
          )}
      />

      <BatchDetailDrawer
        open={detailDrawerOpen}
        record={activeRecord}
        canWrite={canWrite}
        retryingSourceKeys={retryingSourceKeys}
        onClose={handleCloseDetail}
        onRetrySingleSource={handleRetrySingleSource}
      />
    </div>
  );
}
