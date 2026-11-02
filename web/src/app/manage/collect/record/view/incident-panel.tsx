"use client";

import { Button, Popconfirm, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { SourceIncident } from "./types";
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
      title: "站点",
      dataIndex: "sourceName",
      key: "sourceName",
      width: 180,
      render: (name: string, item) => (
        <span>
          {name || item.sourceId}
          <Text type="secondary" className={styles.incidentSourceId}>
            ({item.sourceId})
          </Text>
        </span>
      ),
    },
    {
      title: "连续失败",
      dataIndex: "consecutiveFailures",
      key: "consecutiveFailures",
      width: 100,
      align: "center",
    },
    {
      title: "最近时间",
      dataIndex: "latestFailedAt",
      key: "latestFailedAt",
      width: 180,
      render: (val: string) => (val ? dayjs(val).format("YYYY-MM-DD HH:mm:ss") : "-"),
    },
    {
      title: "原因",
      dataIndex: "latestError",
      key: "latestError",
      ellipsis: true,
      render: (msg?: string) => msg || <Text type="secondary">-</Text>,
    },
    {
      title: "熔断",
      dataIndex: "tripped",
      key: "tripped",
      width: 90,
      align: "center",
      render: (tripped: boolean) => (tripped ? <Tag color="error">已熔断</Tag> : <Text type="secondary">-</Text>),
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
            <Button type="link" size="small" loading={loading} disabled={!canWrite} className={styles.retryLink}>
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
