"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  ConfigProvider,
  Table,
  Button,
  Space,
  Popconfirm,
  Pagination,
  theme,
} from "antd";
import {
  DeleteOutlined,
  ClearOutlined,
} from "@ant-design/icons";
import type { Dayjs } from "dayjs";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import { CollectHistoryRecord, SourceIncident } from "./types";
import { BatchDetailDrawer } from "./batch-detail-drawer";
import { getHistoryColumns } from "./history-columns";
import { IncidentPanel } from "./incident-panel";
import { RecordFilterBar, FilterParams } from "./record-filter-bar";
import { useRecordStream } from "./use-record-stream";
import styles from "./index.module.less";

export default function CollectRecordPageView() {
  const [records, setRecords] = useState<CollectHistoryRecord[]>([]);
  const [incidents, setIncidents] = useState<SourceIncident[]>([]);
  const [loading, setLoading] = useState(false);
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
  pageRef.current = page;
  paramsRef.current = params;

  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const { token } = theme.useToken();

  const historyTableTheme = useMemo(
    () => ({
      components: {
        Table: {
          rowHoverBg: token.colorFillQuaternary,
          rowSelectedBg: token.colorFillTertiary,
          rowSelectedHoverBg: token.colorFillTertiary,
        },
      },
    }),
    [token.colorFillQuaternary, token.colorFillTertiary],
  );

  const getRecords = useCallback(async (p?: { current: number; pageSize: number }, overrideParams?: typeof params) => {
    setSelectedRowKeys([]);
    setLoading(true);
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
      if (resp.code === 0 && resp.data) {
        setRecords(resp.data.list || []);
        setPage((prev) => ({
          ...prev,
          current: resp.data.current || pg.current,
          pageSize: resp.data.pageSize || pg.pageSize,
          total: resp.data.total || 0,
        }));
      }
    } finally {
      setLoading(false);
    }
  }, []);

  const getIncidents = useCallback(async () => {
    try {
      const resp = await ApiGet<{ list: SourceIncident[] }>("/manage/collect/history/incidents");
      if (resp.code === 0) {
        setIncidents(resp.data?.list || []);
        return;
      }
      message.error(resp.msg || "异常站点加载失败");
    } catch {
      // 网络错误已由请求拦截器提示
    }
  }, [message]);

  useEffect(() => {
    void getRecords();
    void getIncidents();
  }, [getRecords, getIncidents]);

  const currentBatchIds = useMemo(
    () => records.map((r) => r.batchId).filter(Boolean),
    [records],
  );

  useRecordStream(currentBatchIds, {
    onProgress: (payload) => {
      setRecords((prev) =>
        prev.map((r) => {
          if (r.batchId !== payload.batchId) return r;
          const updated = {
            ...r,
            status: payload.status || r.status,
            durationMs: payload.durationMs ?? r.durationMs,
            filmCount: payload.filmCount ?? r.filmCount,
            pageTotal: payload.pageTotal ?? r.pageTotal,
            pageSuccess: payload.pageSuccess ?? r.pageSuccess,
            pageFailed: payload.pageFailed ?? r.pageFailed,
            successSources: payload.successSources ?? r.successSources,
            failedSources: payload.failedSources ?? r.failedSources,
            detailJson: payload.detailJson ?? r.detailJson,
          };
          if (activeRecord?.batchId === r.batchId) {
            setActiveRecord(updated);
          }
          return updated;
        }),
      );
    },
    onFinished: (payload) => {
      setRecords((prev) =>
        prev.map((r) => {
          if (r.batchId !== payload.batchId) return r;
          const updated = {
            ...r,
            status: payload.status || r.status,
            finishedAt: payload.finishedAt || r.finishedAt,
            durationMs: payload.durationMs ?? r.durationMs,
            filmCount: payload.filmCount ?? r.filmCount,
            pageTotal: payload.pageTotal ?? r.pageTotal,
            pageSuccess: payload.pageSuccess ?? r.pageSuccess,
            pageFailed: payload.pageFailed ?? r.pageFailed,
            successSources: payload.successSources ?? r.successSources,
            failedSources: payload.failedSources ?? r.failedSources,
            errorMsg: payload.errorMsg ?? r.errorMsg,
            detailJson: payload.detailJson ?? r.detailJson,
          };
          if (activeRecord?.batchId === r.batchId) {
            setActiveRecord(updated);
          }
          return updated;
        }),
      );
      void getIncidents();
    },
  });

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
          void getIncidents();
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
    [message, getRecords, getIncidents],
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
          void getIncidents();
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
    [message, getRecords, getIncidents],
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
        void getIncidents();
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
      void getIncidents();
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
        description="管理每次影视采集的批次运行状态与成果指标，查看各站点抓取入库的影片明细，并对异常站点执行就地恢复重试。"
      />

      <IncidentPanel
        incidents={incidents}
        canWrite={canWrite}
        retryingSourceKeys={retryingSourceKeys}
        onRetry={handleRetrySingleSource}
      />

      <RecordFilterBar
        params={params}
        dateRange={dateRange}
        onParamsChange={handleParamsChange}
        onDateRangeChange={setDateRange}
        onSearch={handleSearch}
        onReset={handleReset}
      />

      <ConfigProvider theme={historyTableTheme}>
        <Table
          columns={columns}
          dataSource={records}
          rowKey="id"
          loading={loading}
          size="middle"
          pagination={false}
          tableLayout="fixed"
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
              <div className={styles.tableTitle}>
                采集记录列表
                {selectedRowKeys.length > 0 && (
                  <span className={styles.selectionCount}>（已选中 {selectedRowKeys.length} 项）</span>
                )}
              </div>
              <Space size={[8, 8]} wrap className={styles.tableActions}>
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
                  <Button danger icon={<ClearOutlined />} disabled={!canWrite}>
                    清空历史
                  </Button>
                </Popconfirm>
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
      </ConfigProvider>

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
