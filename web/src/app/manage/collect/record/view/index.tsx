"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Table,
  Tag,
  Button,
  Space,
  Select,
  DatePicker,
  Popconfirm,
  Pagination,
  Tooltip,
  Typography,
} from "antd";
import {
  ReloadOutlined,
  DeleteOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/client-api";
import dayjs from "dayjs";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

const { RangePicker } = DatePicker;
const RECOVER_MAX_RETRY_COUNT = 5;

interface FailRecord {
  ID: number;
  originName: string;
  originId: string;
  pageNumber: number;
  hour: number;
  cause: string;
  status: number;
  retryCount: number;
  CreatedAt: string;
  UpdatedAt: string;
}

const FAILURE_RECORD_STATUS = {
  pending: 1,
  success: 0,
  failed: 2,
} as const;

function renderStatusTag(status: number) {
  if (status === FAILURE_RECORD_STATUS.pending) {
    return <Tag color="processing">待自动重试</Tag>;
  }
  if (status === FAILURE_RECORD_STATUS.success) {
    return <Tag color="success">重试成功</Tag>;
  }
  return <Tag color="error">最终失败</Tag>;
}

function normalizeStatusOptionLabel(name: string, value: number) {
  if (value === FAILURE_RECORD_STATUS.pending) {
    return "待自动重试";
  }
  if (value === FAILURE_RECORD_STATUS.failed) {
    return "最终失败";
  }
  return name;
}

export default function FailureRecordPageView() {
  const [records, setRecords] = useState<FailRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [page, setPage] = useState({ current: 1, pageSize: 10, total: 0 });
  const [params, setParams] = useState({
    originId: "",
    status: -1,
    beginTime: "",
    endTime: "",
  });
  const [options, setOptions] = useState<any>({
    origin: [],
    status: [],
  });
  const { message } = useAppMessage();

  const getRecords = useCallback(
    async (p?: any) => {
      setLoading(true);
      const pg = p || page;
      try {
        const resp = await ApiGet("/manage/collect/record/list", {
          ...params,
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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleRetry = async (id: number) => {
    const resp = await ApiPost("/manage/collect/record/retry", { id });
    if (resp.code === 0) {
      message.success(resp.msg);
      void getRecords();
    } else message.error(resp.msg);
  };

  const handleRetryAll = async () => {
    const resp = await ApiPost("/manage/collect/record/retry/all", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      void getRecords();
    } else message.error(resp.msg);
  };

  const handleCleanResult = async () => {
    const resp = await ApiPost("/manage/collect/record/clear/result", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      getRecords();
    } else message.error(resp.msg);
  };

  const handleCleanAll = async () => {
    const resp = await ApiPost("/manage/collect/record/clear/all", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      getRecords();
    } else message.error(resp.msg);
  };

  const columns: ColumnsType<FailRecord> = [
    {
      title: "ID",
      dataIndex: "ID",
      width: 60,
      fixed: "left",
      align: "center",
      render: (v) => (
          <span style={{ color: "var(--ant-color-purple)" }}>{v}</span>
      ),
    },
    {
      title: "采集站",
      dataIndex: "originName",
      align: "center",
      render: (v) => <Tag color="blue">{v}</Tag>,
    },
    {
      title: "采集源ID",
      dataIndex: "originId",
      align: "center",
      render: (v) => <Tag color="green">{v}</Tag>,
    },
    {
      title: "分页页码",
      dataIndex: "pageNumber",
      align: "center",
      render: (v) => <Tag color="orange">{v}</Tag>,
    },
    {
      title: "采集时长",
      dataIndex: "hour",
      align: "center",
      render: (v) => <Tag color="orange">{v}</Tag>,
    },
    {
      title: "失败原因",
      dataIndex: "cause",
      align: "left",
      ellipsis: true,
      render: (v) => <Typography.Text type="danger">{v}</Typography.Text>,
    },
    {
      title: "状态",
      dataIndex: "status",
      align: "center",
      render: (v) => renderStatusTag(v),
    },
    {
      title: "重试次数",
      dataIndex: "retryCount",
      align: "center",
      render: (v) => {
        const retryCount = v || 1;
        const color = retryCount >= RECOVER_MAX_RETRY_COUNT ? "error" : retryCount > 1 ? "warning" : "default";
        return <Tag color={color}>{retryCount}/{RECOVER_MAX_RETRY_COUNT}</Tag>;
      },
    },
    {
      title: "请求时间",
      dataIndex: "CreatedAt",
      align: "center",
      render: (v) => dayjs(v).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      fixed: "right",
      render: (_, record) => {
        const isSuccess = record.status === FAILURE_RECORD_STATUS.success;
        const isFinalFailed = record.status === FAILURE_RECORD_STATUS.failed;
        const tooltipTitle = isSuccess
          ? "已重试成功，无需再次重试"
          : isFinalFailed
            ? "手动再试，失败后仍保持最终失败"
            : "立即重试此记录";
        return (
          <Tooltip title={tooltipTitle}>
            <Button
              type="primary"
              shape="circle"
              size="small"
              disabled={isSuccess}
              style={isSuccess ? undefined : { background: "#52c41a", borderColor: "#52c41a" }}
              icon={<ReloadOutlined />}
              onClick={() => handleRetry(record.ID)}
            />
          </Tooltip>
        );
      },
    },
  ];

  return (
    <div className={styles.pageBody}>
        <ManagePageHeader
          title="失败记录"
          description="查看采集失败明细、自动重试次数和最终失败记录，并统一清理已有重试结果或全部失败记录。"
        />

      <Space size={[8, 8]} wrap className={styles.filterBar}>
        <Select
          placeholder="采集来源"
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
          className={styles.dateRange}
          onChange={(dates) => {
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
          onClick={() => getRecords()}
          className={styles.searchButton}
        >
          查询
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
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>失败记录列表</div>
            <Space size={[8, 8]} wrap className={styles.tableActions}>
              <Popconfirm title="确认立即重试所有待自动重试记录？" onConfirm={handleRetryAll}>
                <Button type="primary" icon={<ReloadOutlined />}>
                  重试待重试记录
                </Button>
              </Popconfirm>
              <Popconfirm title="确认清除已有重试结果的记录？" onConfirm={handleCleanResult}>
                <Button
                  icon={<WarningOutlined />}
                  style={{
                    color: "var(--ant-color-warning)",
                    borderColor: "var(--ant-color-warning)",
                  }}
                >
                  清除重试结果
                </Button>
              </Popconfirm>
              <Popconfirm title="确认清除所有记录？" onConfirm={handleCleanAll}>
                <Button danger icon={<DeleteOutlined />}>
                  清除所有
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
              pageSizeOptions={[10, 20, 50, 100, 500]}
              onChange={(current, pageSize) => {
                const newPage = { ...page, current, pageSize };
                setPage(newPage);
                getRecords(newPage);
              }}
            />
          </div>
        )}
      />
    </div>
  );
}
