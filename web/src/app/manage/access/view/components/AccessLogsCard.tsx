"use client";

import { useMemo } from "react";
import Link from "next/link";
import { Card, Input, Select, Space, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import ResourceCell from "./ResourceCell";
import { ACTION_META, CLIENT_META, isSafeInternalHref } from "../constants";
import type { AnalyticsModule, LogRow } from "../types";

interface AccessLogsCardProps {
  module: AnalyticsModule;
  logs: LogRow[];
  total: number;
  loading?: boolean;
  action: string;
  keyword: string;
  page: number;
  pageSize: number;
  onFilterChange: (action: string, keyword: string) => void;
  onPageChange: (page: number, pageSize: number) => void;
}

const ACTION_OPTIONS = [
  { value: "all", label: "全部行为" },
  { value: "play", label: "影视点播" },
  { value: "search", label: "寻片搜索" },
  { value: "classify", label: "分类筛选" },
  { value: "browse", label: "页面浏览" },
  { value: "config", label: "配置同步" },
];

export default function AccessLogsCard({
  module,
  logs,
  total,
  loading,
  action,
  keyword,
  page,
  pageSize,
  onFilterChange,
  onPageChange,
}: AccessLogsCardProps) {
  // 仅全站或 App（含多平台细分）视图保留客户端列；独立设备 ID 列收拢至设备/环境列次行，避免横向溢出
  const showClientCol = module === "all" || module === "app";

  const columns = useMemo<ColumnsType<LogRow>>(() => {
    const list: ColumnsType<LogRow> = [
      {
        title: "时间",
        dataIndex: "ts",
        key: "ts",
        width: 148,
        render: (ts: string) => (
          <Typography.Text type="secondary" style={{ fontSize: 12, whiteSpace: "nowrap" }}>
            {dayjs(ts).format("MM-DD HH:mm:ss")}
          </Typography.Text>
        ),
      },
    ];

    if (showClientCol) {
      list.push({
        title: "客户端",
        dataIndex: "clientType",
        key: "clientType",
        width: 100,
        render: (client: string) => {
          const meta = CLIENT_META[(client || "").toLowerCase()] || {
            label: client || "未知",
            color: "default",
          };
          return <Tag color={meta.color} style={{ margin: 0 }}>{meta.label}</Tag>;
        },
      });
    }

    list.push(
      {
        title: "行为",
        dataIndex: "action",
        key: "action",
        width: 96,
        render: (act: string) => {
          const meta = ACTION_META[(act || "").toLowerCase()] || {
            label: act || "访问",
            color: "default",
          };
          return <Tag color={meta.color} style={{ margin: 0 }}>{meta.label}</Tag>;
        },
      },
      {
        title: "页面 / 接口",
        dataIndex: "path",
        key: "path",
        width: 240,
        render: (path: string, record) => {
          const target = record.page || path || "/";
          const full = record.query ? `${target}?${record.query}` : target;
          return (
            <div style={{ display: "flex", flexDirection: "column", gap: 2, minWidth: 0 }}>
              {record.clientType === "web" && isSafeInternalHref(target) ? (
                <Link href={target} target="_blank" style={{ wordBreak: "break-all" }}>
                  {full}
                </Link>
              ) : (
                <Typography.Text code style={{ fontSize: 12, wordBreak: "break-all" }}>
                  {full}
                </Typography.Text>
              )}
              {record.pageTitle && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }} ellipsis>
                  {record.pageTitle}
                </Typography.Text>
              )}
            </div>
          );
        },
      },
      {
        title: "关联资源",
        dataIndex: "resource",
        key: "resource",
        width: 240,
        render: (_, record) => <ResourceCell record={record} />,
      },
      {
        title: "设备 / 环境",
        key: "env",
        width: 190,
        render: (_, record) => (
          <div style={{ display: "flex", flexDirection: "column", gap: 4 }}>
            <Space size={4} wrap>
              {record.appVersion && <Tag color="blue" style={{ margin: 0 }}>{record.appVersion}</Tag>}
              {record.deviceModel && <Tag style={{ margin: 0 }}>{record.deviceModel}</Tag>}
              {!record.appVersion && !record.deviceModel && (
                <Tag style={{ margin: 0 }}>{record.uaFamily || "other"}</Tag>
              )}
              {record.os && <Tag style={{ margin: 0 }}>{record.os}</Tag>}
            </Space>
            {record.deviceId && (
              <Typography.Text
                copyable={{ text: record.deviceId }}
                type="secondary"
                style={{ fontSize: 11 }}
                ellipsis={{ tooltip: record.deviceId }}
              >
                ID: {record.deviceId}
              </Typography.Text>
            )}
          </div>
        ),
      },
      {
        title: "访客 IP",
        dataIndex: "ipPreview",
        key: "ipPreview",
        width: 126,
        render: (ip: string) => <Typography.Text code>{ip || "local"}</Typography.Text>,
      },
    );

    return list;
  }, [showClientCol]);

  return (
    <Card
      size="small"
      title={
        <Space size={8} wrap>
          <Select
            value={action}
            options={ACTION_OPTIONS}
            onChange={(val) => onFilterChange(val, keyword)}
            style={{ width: 120 }}
          />
          <Input.Search
            allowClear
            placeholder="搜索路径 / 资源 / IP / 设备"
            value={keyword}
            onChange={(e) => onFilterChange(action, e.target.value)}
            style={{ width: 260 }}
          />
        </Space>
      }
      extra={
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          共 {total.toLocaleString()} 条记录
        </Typography.Text>
      }
    >
      <Table
        rowKey="_key"
        columns={columns}
        dataSource={logs}
        loading={loading}
        size="middle"
        scroll={{ x: 960 }}
        pagination={{
          current: page,
          pageSize,
          total,
          showSizeChanger: true,
          pageSizeOptions: [10, 20, 50],
          showTotal: (t) => `共 ${t} 条记录`,
          onChange: (nextPage, nextSize) => onPageChange(nextPage, nextSize),
        }}
      />
    </Card>
  );
}
