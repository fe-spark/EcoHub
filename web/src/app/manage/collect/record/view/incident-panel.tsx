"use client";

import { Button, Popconfirm, Table, Tag, Typography } from "antd";
import { ClockCircleOutlined, CloseCircleOutlined, CloudServerOutlined, EyeOutlined, ReloadOutlined, StopOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { SourceIncident } from "./types";
import { ErrorDetailButton } from "./error-detail-button";
import styles from "./index.module.less";

const { Text } = Typography;

export interface IncidentPanelProps {
  incidents: SourceIncident[];
  canWrite: boolean;
  retryingSourceKeys: Set<string>;
  onRetry: (batchId: string, sourceId: string) => void;
}

export function IncidentPanel({
  incidents,
  canWrite,
  retryingSourceKeys,
  onRetry,
}: IncidentPanelProps) {
  if (incidents.length === 0) {
    return null;
  }

  const columns: ColumnsType<SourceIncident> = [
    {
      title: (
        <span className={styles.withIcon}>
          <CloudServerOutlined />
          站点
        </span>
      ),
      dataIndex: "sourceName",
      key: "sourceName",
      width: 180,
      render: (name: string, item) => (
        <span className={styles.withIcon}>
          <CloudServerOutlined />
          {name || item.sourceId}
          <Text type="secondary" className={styles.incidentSourceId}>
            {item.sourceId}
          </Text>
        </span>
      ),
    },
    {
      title: (
        <span className={styles.withIcon}>
          <CloseCircleOutlined />
          连续失败
        </span>
      ),
      dataIndex: "consecutiveFailures",
      key: "consecutiveFailures",
      width: 100,
      align: "center",
    },
    {
      title: (
        <span className={styles.withIcon}>
          <ClockCircleOutlined />
          最近时间
        </span>
      ),
      dataIndex: "latestFailedAt",
      key: "latestFailedAt",
      width: 180,
      render: (val: string) =>
        val ? (
          <span className={styles.withIcon}>
            <ClockCircleOutlined />
            {dayjs(val).format("YYYY-MM-DD HH:mm:ss")}
          </span>
        ) : (
          "-"
        ),
    },
    {
      title: "原因",
      dataIndex: "latestError",
      key: "latestError",
      ellipsis: true,
      render: (msg?: string, item) =>
        msg ? <ErrorDetailButton text={msg} title={item.sourceName ? `${item.sourceName} 异常原因` : "异常原因"} /> : null,
    },
    {
      title: "熔断",
      dataIndex: "tripped",
      key: "tripped",
      width: 90,
      align: "center",
      render: (tripped: boolean) =>
        tripped ? (
          <Tag color="error" icon={<StopOutlined />}>
            已熔断
          </Tag>
        ) : (
          <Tag color="warning" icon={<EyeOutlined />}>
            观察中
          </Tag>
        ),
    },
    {
      title: "操作",
      key: "action",
      width: 100,
      align: "center",
      render: (_, item) => {
        if (!item.latestBatchId) {
          return <Text type="secondary">-</Text>;
        }
        const key = `${item.latestBatchId}:${item.sourceId}`;
        const loading = retryingSourceKeys.has(key);
        return (
          <Popconfirm
            title={`重试采集站「${item.sourceName || item.sourceId}」？`}
            onConfirm={() => onRetry(item.latestBatchId, item.sourceId)}
            disabled={!canWrite || loading}
          >
            <Button
              type="text"
              size="small"
              icon={<ReloadOutlined />}
              loading={loading}
              disabled={!canWrite}
              className={styles.actionButton}
            >
              重试该站
            </Button>
          </Popconfirm>
        );
      },
    },
  ];

  return (
    <Table
      className={styles.incidentTable}
      size="small"
      rowKey="sourceId"
      columns={columns}
      dataSource={incidents}
      pagination={false}
      scroll={{ x: "max-content" }}
      title={() => <div className={styles.tableTitle}>异常站点</div>}
    />
  );
}
