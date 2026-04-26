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
import ManagePageShell from "../../../components/page-shell";
import styles from "./index.module.less";

const { RangePicker } = DatePicker;

interface FailRecord {
  ID: number;
  originName: string;
  originId: string;
  pageNumber: number;
  hour: number;
  cause: string;
  status: number;
  retryCount: number;
  UpdatedAt: string;
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
    if (resp.code === 0) message.success(resp.msg);
    else message.error(resp.msg);
  };

  const handleRetryAll = async () => {
    const resp = await ApiPost("/manage/collect/record/retry/all", {});
    if (resp.code === 0) message.success(resp.msg);
    else message.error(resp.msg);
  };

  const handleCleanDone = async () => {
    const resp = await ApiPost("/manage/collect/record/clear/done", {});
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
      ellipsis: true,
      render: (v) => <Tag color="red">{v}</Tag>,
    },
    {
      title: "状态",
      dataIndex: "status",
      align: "center",
      render: (v) =>
        v === 1 ? (
          <Tag color="warning">待重试</Tag>
        ) : (
          <Tag color="success">已处理</Tag>
        ),
    },
    {
      title: "重试次数",
      dataIndex: "retryCount",
      align: "center",
      width: 100,
      render: (v) => <Tag color={v > 1 ? "volcano" : "default"}>{v || 1}</Tag>,
    },
    {
      title: "执行时间",
      dataIndex: "UpdatedAt",
      align: "center",
      width: 170,
      render: (v) => dayjs(v).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      width: 80,
      fixed: "right",
      render: (_, record) => (
        <Tooltip title="采集重试">
          <Button
            type="primary"
            shape="circle"
            size="small"
            style={{ background: "#52c41a", borderColor: "#52c41a" }}
            icon={<ReloadOutlined />}
            onClick={() => handleRetry(record.ID)}
          />
        </Tooltip>
      ),
    },
  ];

  return (
    <ManagePageShell
      eyebrow="采集中心"
      title="失败记录"
      description="查看采集失败明细、快速重试异常任务，并统一清理已处理或全部失败记录。"
      extra={
        <div className={styles.filterBar}>
          <Select
            placeholder="采集来源"
            value={params.originId || undefined}
            onChange={(v) => setParams({ ...params, originId: v })}
            options={options.origin?.map((o: any) => ({
              label: o.name,
              value: o.value,
            }))}
            style={{ width: 160 }}
            allowClear
          />
          <Select
            placeholder="记录状态"
            value={params.status}
            onChange={(v) => setParams({ ...params, status: v })}
            options={options.status?.map((o: any) => ({
              label: o.name,
              value: o.value,
            }))}
            style={{ width: 120 }}
          />
          <RangePicker
            showTime
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
          <Button type="primary" onClick={() => getRecords()}>
            查询
          </Button>
        </div>
      }
      panelClassName={styles.tablePanel}
      panelless
    >
      <Table
        columns={columns}
        dataSource={records}
        rowKey="ID"
        loading={loading}
        bordered
        size="middle"
        pagination={false}
        scroll={{ x: "max-content" }}
      />

      <div className={styles.toolbar}>
        <Space>
          <Popconfirm title="确认重试所有失效记录？" onConfirm={handleRetryAll}>
            <Button type="primary" icon={<ReloadOutlined />}>
              全部重试
            </Button>
          </Popconfirm>
          <Popconfirm title="确认清除已处理记录？" onConfirm={handleCleanDone}>
            <Button
              icon={<WarningOutlined />}
              style={{
                color: "var(--ant-color-warning)",
                borderColor: "var(--ant-color-warning)",
              }}
            >
              清除已处理
            </Button>
          </Popconfirm>
          <Popconfirm title="确认清除所有记录？" onConfirm={handleCleanAll}>
            <Button danger icon={<DeleteOutlined />}>
              清除所有
            </Button>
          </Popconfirm>
        </Space>
      </div>

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
    </ManagePageShell>
  );
}
