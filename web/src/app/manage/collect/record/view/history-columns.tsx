import React from "react";
import { Tag, Badge, Tooltip, Space, Typography, Button, Popconfirm } from "antd";
import { ReloadOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { CollectHistoryRecord } from "./types";

const { Text } = Typography;

export function formatDuration(ms: number): string {
  if (ms <= 0) return "< 1 秒";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const remainingSec = seconds % 60;
  if (minutes < 60) {
    return remainingSec > 0 ? `${minutes} 分 ${remainingSec} 秒` : `${minutes} 分钟`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMin = minutes % 60;
  return `${hours} 小时 ${remainingMin} 分`;
}

export function renderTriggerTag(trigger: string, tag?: string) {
  if (trigger === "cron") {
    return <Tag color="cyan">定时任务</Tag>;
  }
  if (trigger === "manual") {
    return <Tag color="blue">{tag || "手动采集"}</Tag>;
  }
  if (trigger === "retry") {
    return <Tag color="purple">失败重试</Tag>;
  }
  if (trigger === "single_update") {
    return <Tag color="geekblue">单片更新</Tag>;
  }
  return <Tag>{tag || trigger || "未知"}</Tag>;
}

export function renderHourTag(hour: number) {
  if (hour === -1) {
    return <Tag color="geekblue">全量</Tag>;
  }
  if (hour > 0) {
    return <Tag color="default">{hour} 小时增量</Tag>;
  }
  return <Tag color="default">默认</Tag>;
}

export function renderStatusBadge(status: string) {
  switch (status) {
    case "success":
      return <Badge status="success" text="全部成功" />;
    case "partial":
      return <Badge status="warning" text="部分失败" />;
    case "failed":
      return <Badge status="error" text="采集失败" />;
    case "running":
      return <Badge status="processing" text="执行中" />;
    case "stopped":
      return <Badge status="default" text="已终止" />;
    case "interrupted":
      return <Badge status="error" text="异常中断" />;
    default:
      return <Badge status="default" text={status || "未知"} />;
  }
}

export interface GetHistoryColumnsOptions {
  canWrite: boolean;
  retryingBatchIds: Set<string>;
  onRetryBatch: (record: CollectHistoryRecord) => void;
}

export function getHistoryColumns(opts: GetHistoryColumnsOptions): ColumnsType<CollectHistoryRecord> {
  const { canWrite, retryingBatchIds, onRetryBatch } = opts;

  return [
    {
      title: "开始时间 / 耗时",
      dataIndex: "startedAt",
      key: "startedAt",
      width: 190,
      render: (val: string, record) => (
        <Space direction="vertical" size={2}>
          <Text style={{ fontSize: 13 }}>
            {val ? dayjs(val).format("YYYY-MM-DD HH:mm:ss") : "-"}
          </Text>
          <Text type="secondary" style={{ fontSize: 12 }}>
            耗时：{formatDuration(record.durationMs)}
          </Text>
        </Space>
      ),
    },
    {
      title: "触发源",
      dataIndex: "trigger",
      key: "trigger",
      width: 110,
      align: "center",
      render: (trigger: string, record) => renderTriggerTag(trigger, record.tag),
    },
    {
      title: "采集模式",
      dataIndex: "hour",
      key: "hour",
      width: 120,
      align: "center",
      render: (hour: number) => renderHourTag(hour),
    },
    {
      title: "站点成败概况",
      key: "sourceSummary",
      width: 150,
      align: "center",
      render: (_, record) => {
        const hasFailed = record.failedSources > 0;
        return (
          <Space size={4}>
            <span style={{ color: "var(--ant-color-success)" }}>
              {record.successSources} 成功
            </span>
            {hasFailed && (
              <Tag color="error" style={{ margin: 0, padding: "0 6px", fontSize: 12 }}>
                {record.failedSources} 失败
              </Tag>
            )}
            {!hasFailed && (
              <span style={{ color: "var(--ant-color-text-secondary)" }}>
                / 共 {record.sourceCount}
              </span>
            )}
          </Space>
        );
      },
    },
    {
      title: "执行状态",
      dataIndex: "status",
      key: "status",
      width: 110,
      align: "center",
      render: (status: string) => renderStatusBadge(status),
    },
    {
      title: "入库 / 更新影片",
      dataIndex: "filmCount",
      key: "filmCount",
      width: 130,
      align: "center",
      render: (count: number) => (
        <span style={{ fontWeight: 600, color: "var(--ant-color-primary)" }}>
          {count} 部
        </span>
      ),
    },
    {
      title: "页码进度",
      key: "pages",
      width: 140,
      align: "center",
      render: (_, record) => {
        const hasFailed = record.pageFailed > 0;
        return (
          <Space size={4}>
            <span>{record.pageSuccess} / {record.pageTotal} 页</span>
            {hasFailed && (
              <Tag color="error" style={{ margin: 0, padding: "0 4px", fontSize: 11 }}>
                败 {record.pageFailed}
              </Tag>
            )}
          </Space>
        );
      },
    },
    {
      title: "错误摘要",
      dataIndex: "errorMsg",
      key: "errorMsg",
      ellipsis: true,
      render: (msg: string) => {
        if (!msg) return <Text type="secondary">-</Text>;
        return (
          <Tooltip title={msg}>
            <Text type="danger" ellipsis style={{ maxWidth: 220 }}>
              {msg}
            </Text>
          </Tooltip>
        );
      },
    },
    {
      title: "操作",
      key: "action",
      width: 130,
      align: "center",
      render: (_, record) => {
        const isRetrying = retryingBatchIds.has(record.batchId);
        const canRetry = record.failedSources > 0;
        if (!canRetry) {
          return <Text type="secondary">-</Text>;
        }
        return (
          <Popconfirm
            title="重试本次失败的站点？"
            description={`将重新采集本次失败的 ${record.failedSources} 个站点`}
            onConfirm={() => onRetryBatch(record)}
            disabled={!canWrite || isRetrying}
          >
            <Button
              type="link"
              size="small"
              icon={<ReloadOutlined />}
              loading={isRetrying}
              disabled={!canWrite}
              style={{ padding: 0 }}
            >
              重试失败站点
            </Button>
          </Popconfirm>
        );
      },
    },
  ];
}
