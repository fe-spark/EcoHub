"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Table, Button, Popconfirm, Pagination, Tag, Empty, Space, Dropdown, Switch, Badge } from "antd";
import type { MenuProps } from "antd";
import {
  DeleteOutlined,
  ClearOutlined,
  ReloadOutlined,
  HistoryOutlined,
  DownOutlined,
} from "@ant-design/icons";
import type { Dayjs } from "dayjs";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import {
  ContentPageShell,
  ContentSectionCard,
} from "@/app/manage/components/content-shell";
import { CollectHistoryRecord, isSingleFilmRecord } from "./types";
import { BatchDetailDrawer } from "./batch-detail-drawer";
import { BatchFilmsModal } from "./batch-films-modal";
import { getHistoryColumns } from "./history-columns";
import { RecordFilterBar, FilterParams } from "./record-filter-bar";
import styles from "./index.module.less";

const DEFAULT_PARAMS: FilterParams = {
  trigger: "",
  status: "",
  beginTime: "",
  endTime: "",
  updated: "",
  failed: "",
};

const REFRESH_OPTIONS = [
  { label: "5 秒", value: 5 },
  { label: "10 秒", value: 10 },
  { label: "15 秒", value: 15 },
  { label: "30 秒", value: 30 },
];

export default function CollectRecordPageView() {
  const [records, setRecords] = useState<CollectHistoryRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshSeconds, setRefreshSeconds] = useState(10);
  const [selectedRowKeys, setSelectedRowKeys] = useState<React.Key[]>([]);
  const [batchDeleting, setBatchDeleting] = useState(false);
  const [retryingBatchIds, setRetryingBatchIds] = useState<Set<string>>(() => new Set());
  const [retryingSourceKeys, setRetryingSourceKeys] = useState<Set<string>>(() => new Set());
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });
  const [params, setParams] = useState<FilterParams>(DEFAULT_PARAMS);
  const [dateRange, setDateRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);

  const [activeRecord, setActiveRecord] = useState<CollectHistoryRecord | null>(null);
  const [detailDrawerOpen, setDetailDrawerOpen] = useState(false);
  const [filmsRecord, setFilmsRecord] = useState<CollectHistoryRecord | null>(null);

  const pageRef = useRef(page);
  const paramsRef = useRef(params);
  const requestSeq = useRef(0);
  const activeRecordRef = useRef(activeRecord);
  pageRef.current = page;
  paramsRef.current = params;
  activeRecordRef.current = activeRecord;

  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const getRecords = useCallback(
    async (
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
        const resp = await ApiGet<{
          list: CollectHistoryRecord[];
          total: number;
          current: number;
          pageSize: number;
        }>("/manage/collect/history/list", {
          ...reqParams,
          current: pg.current,
          pageSize: pg.pageSize,
        });
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
    },
    [],
  );

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

  const hasRunning =
    records.some((r) => r.status === "running") || activeRecord?.status === "running";

  useEffect(() => {
    if (!autoRefresh || refreshSeconds <= 0) return;
    const timer = window.setInterval(() => {
      if (document.visibilityState !== "visible") return;
      void getRecords(undefined, undefined, { silent: true });
    }, refreshSeconds * 1000);
    return () => window.clearInterval(timer);
  }, [getRecords, autoRefresh, refreshSeconds]);

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
          message.success(isSingleFilmRecord(record) ? "已启动重采" : "已启动失败重试");
          void getRecords();
        } else {
          message.error(resp.msg || "重试失败");
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
          message.success("已启动站点重试");
          void getRecords();
        } else {
          message.error(resp.msg || "重试失败");
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
        message.success("已删除");
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
      message.success("已清空");
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

  const handleReset = () => {
    setParams(DEFAULT_PARAMS);
    setDateRange(null);
    const newPage = { ...pageRef.current, current: 1 };
    setPage(newPage);
    void getRecords(newPage, DEFAULT_PARAMS);
  };

  const columns = useMemo(
    () =>
      getHistoryColumns({
        canWrite,
        retryingBatchIds,
        onRetryBatch: handleRetryBatch,
        onOpenDetail: handleOpenDetail,
        onOpenFilms: setFilmsRecord,
      }),
    [canWrite, retryingBatchIds, handleRetryBatch, handleOpenDetail],
  );

  const refreshMenuItems: MenuProps["items"] = [
    {
      key: "toggle",
      label: (
        <div className={styles.refreshMenuToggle} onClick={(e) => e.stopPropagation()}>
          <span>自动刷新</span>
          <Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} />
        </div>
      ),
    },
    { type: "divider" },
    {
      type: "group",
      label: "刷新间隔",
      children: REFRESH_OPTIONS.map((opt) => ({
        key: String(opt.value),
        label: opt.label,
        disabled: !autoRefresh,
      })),
    },
  ];

  const handleRefreshMenuClick: MenuProps["onClick"] = ({ key }) => {
    if (key === "toggle") return;
    const seconds = Number(key);
    if (!Number.isNaN(seconds)) {
      setRefreshSeconds(seconds);
    }
  };

  return (
    <ContentPageShell
      title="采集记录"
      headerActions={
        <Space size={8}>
          <Space.Compact>
            <Button onClick={() => void handleRefresh()}>
              <Space size={6} align="center">
                <ReloadOutlined spin={refreshing} />
                <span>刷新</span>
                {autoRefresh ? <span className={styles.refreshSecLabel}>{refreshSeconds}s</span> : null}
                <Badge status={autoRefresh ? "processing" : "default"} />
              </Space>
            </Button>
            <Dropdown
              placement="bottomRight"
              menu={{
                items: refreshMenuItems,
                selectable: true,
                selectedKeys: autoRefresh ? [String(refreshSeconds)] : [],
                onClick: handleRefreshMenuClick,
              }}
            >
              <Button icon={<DownOutlined />} />
            </Dropdown>
          </Space.Compact>
          <Popconfirm
            title="清空全部历史记录？"
            okText="清空"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            onConfirm={handleClearAll}
            disabled={!canWrite || page.total === 0}
          >
            <Button icon={<ClearOutlined />} disabled={!canWrite || page.total === 0}>
              清空历史
            </Button>
          </Popconfirm>
        </Space>
      }
    >
      <RecordFilterBar
        params={params}
        dateRange={dateRange}
        onParamsChange={handleParamsChange}
        onDateRangeChange={setDateRange}
        onReset={handleReset}
      />

      <ContentSectionCard
        icon={<HistoryOutlined />}
        title="批次列表"
        count={page.total}
        extra={
          hasRunning ? (
            <Tag color="processing" variant="filled" style={{ margin: 0 }}>
              运行中
            </Tag>
          ) : null
        }
        actions={
          selectedRowKeys.length > 0 ? (
            <div className={styles.tableActions}>
              <Tag color="processing" variant="filled" style={{ margin: 0 }}>
                已选 {selectedRowKeys.length} 项
              </Tag>
              <Button size="small" type="text" onClick={() => setSelectedRowKeys([])}>
                取消选择
              </Button>
              <Popconfirm
                title={`删除选中的 ${selectedRowKeys.length} 条记录？`}
                okType="danger"
                onConfirm={handleDeleteSelected}
                disabled={!canWrite}
              >
                <Button
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  loading={batchDeleting}
                  disabled={!canWrite}
                >
                  删除选中
                </Button>
              </Popconfirm>
            </div>
          ) : null
        }
        footer={
          page.total > 0 ? (
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
          ) : null
        }
      >
        <Table
          columns={columns}
          dataSource={records}
          rowKey="id"
          rowClassName={(record) => (record.status === "running" ? styles.runningRow : "")}
          locale={{
            emptyText: <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无采集记录" />,
          }}
          onRow={(record) => ({
            className: styles.clickableRow,
            onClick: (event) => {
              const target = event.target as HTMLElement | null;
              if (target?.closest("a, button, input, .ant-popover, .ant-checkbox-wrapper")) return;
              handleOpenDetail(record);
            },
          })}
          loading={loading}
          size="middle"
          pagination={false}
          scroll={{ x: 880 }}
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
        />
      </ContentSectionCard>

      <BatchDetailDrawer
        open={detailDrawerOpen}
        record={activeRecord}
        canWrite={canWrite}
        retryingBatchIds={retryingBatchIds}
        retryingSourceKeys={retryingSourceKeys}
        onClose={handleCloseDetail}
        onRetryBatch={handleRetryBatch}
        onRetrySingleSource={handleRetrySingleSource}
      />

      <BatchFilmsModal
        open={!!filmsRecord}
        batchId={filmsRecord?.batchId || ""}
        sourceId=""
        sourceName="全部站点"
        onClose={() => setFilmsRecord(null)}
      />
    </ContentPageShell>
  );
}
