import React from "react";
import { Table, Space, Tag, Typography, Button, Popconfirm } from "antd";
import type { ColumnsType } from "antd/es/table";
import { CollectHistoryRecord, CollectRecordSourceDetail } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;

export interface BatchDetailExpandedProps {
  record: CollectHistoryRecord;
  canWrite: boolean;
  retryingSourceKeys: Set<string>;
  onRetrySingleSource: (batchId: string, sourceId: string) => void;
}

export function BatchDetailExpanded({
  record,
  canWrite,
  retryingSourceKeys,
  onRetrySingleSource,
}: BatchDetailExpandedProps) {
  let details: CollectRecordSourceDetail[] = [];
  if (record.detailJson) {
    try {
      details = JSON.parse(record.detailJson) as CollectRecordSourceDetail[];
    } catch {
      details = [];
    }
  }

  if (details.length === 0) {
    return (
      <div style={{ padding: "8px 16px", color: "var(--ant-color-text-secondary)" }}>
        暂无单站执行明细快照
      </div>
    );
  }

  const sorted = [...details].sort((a, b) => {
    const aFail = a.status === "failed" ? 1 : 0;
    const bFail = b.status === "failed" ? 1 : 0;
    return bFail - aFail;
  });

  const detailCols: ColumnsType<CollectRecordSourceDetail> = [
    {
      title: "采集站点",
      dataIndex: "sourceName",
      key: "sourceName",
      width: 180,
      render: (name: string, item) => (
        <Space size={6}>
          <span style={{ fontWeight: 500 }}>{name}</span>
          <Text type="secondary" style={{ fontSize: 11 }}>
            ({item.sourceId})
          </Text>
        </Space>
      ),
    },
    {
      title: "执行状态",
      dataIndex: "status",
      key: "status",
      width: 100,
      align: "center",
      render: (status: string) => {
        if (status === "done" || status === "success") {
          return <Tag color="success">成功</Tag>;
        }
        if (status === "stopped") {
          return <Tag color="default">已停止</Tag>;
        }
        return <Tag color="error">失败</Tag>;
      },
    },
    {
      title: "页码进度",
      key: "pages",
      width: 130,
      align: "center",
      render: (_, item) => `${item.pageCurrent} / ${item.pageTotal} 页`,
    },
    {
      title: "成败页",
      key: "resultPages",
      width: 140,
      align: "center",
      render: (_, item) => {
        const succ = item.successCnt ?? 0;
        const fail = item.failedCnt ?? 0;
        return (
          <Space size={4}>
            <span style={{ color: "var(--ant-color-success)" }}>{succ} 成功</span>
            {fail > 0 && <span style={{ color: "var(--ant-color-error)" }}>/ {fail} 失败</span>}
          </Space>
        );
      },
    },
    {
      title: "更新影片",
      dataIndex: "filmsTotal",
      key: "filmsTotal",
      width: 100,
      align: "center",
      render: (val: number) => `${val ?? 0} 部`,
    },
    {
      title: "异常原因",
      dataIndex: "error",
      key: "error",
      render: (err: string) => {
        if (!err) return <Text type="secondary">-</Text>;
        return (
          <Text type="danger" style={{ fontWeight: 500 }}>
            {err}
          </Text>
        );
      },
    },
    {
      title: "操作",
      key: "sourceAction",
      width: 100,
      align: "center",
      render: (_, item) => {
        if (item.status !== "failed") {
          return <Text type="secondary">-</Text>;
        }
        const sourceKey = `${record.batchId}:${item.sourceId}`;
        const isSingleRetrying = retryingSourceKeys.has(sourceKey);
        return (
          <Popconfirm
            title={`重试采集站「${item.sourceName}」？`}
            description="将按本次采集的时间窗口重新执行采集"
            onConfirm={() => onRetrySingleSource(record.batchId, item.sourceId)}
            disabled={!canWrite || isSingleRetrying}
          >
            <Button
              type="link"
              size="small"
              loading={isSingleRetrying}
              disabled={!canWrite}
              style={{ padding: 0 }}
            >
              重试该站
            </Button>
          </Popconfirm>
        );
      },
    },
  ];

  return (
    <div style={{ padding: "6px 0" }}>
      <Table
        columns={detailCols}
        dataSource={sorted}
        rowKey={(item, idx) => item.sourceId || String(idx)}
        pagination={false}
        size="small"
        rowClassName={(item) => (item.status === "failed" ? styles.failedRowHighlight : "")}
      />
    </div>
  );
}
