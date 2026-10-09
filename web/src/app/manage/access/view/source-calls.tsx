"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Card, Table, Tag, Typography } from "antd";
import type { ColumnsType } from "antd/es/table";
import { ApiGet } from "@/lib/client-api";

type SourceCallRow = {
  id: string;
  name: string;
  enabled: boolean;
  isPrimary: boolean;
  count: number;
};

export default function SourceCalls({ dayStr, refreshKey }: { dayStr: string; refreshKey?: number }) {
  const [rows, setRows] = useState<SourceCallRow[]>([]);
  const [loading, setLoading] = useState(false);
  const reqSeq = useRef(0);

  const load = useCallback(async () => {
    const seq = ++reqSeq.current;
    setLoading(true);
    try {
      const res = await ApiGet<{ list: SourceCallRow[] }>(`/manage/access/sources?day=${dayStr}`);
      if (seq !== reqSeq.current) {
        return;
      }
      if (res.code === 0 && res.data) {
        setRows(res.data.list || []);
      }
    } catch {
      if (seq === reqSeq.current) {
        setRows([]);
      }
    } finally {
      if (seq === reqSeq.current) {
        setLoading(false);
      }
    }
  }, [dayStr]);

  useEffect(() => {
    void load();
  }, [load, refreshKey]);

  const columns: ColumnsType<SourceCallRow> = [
    {
      title: "采集站",
      dataIndex: "name",
      render: (_, row) => (
        <span>
          {row.name || row.id}
          {row.isPrimary ? (
            <Tag color="gold" style={{ marginInlineStart: 8 }}>
              首选站
            </Tag>
          ) : null}
          {!row.enabled && row.name !== row.id ? (
            <Tag style={{ marginInlineStart: 8 }}>已停用</Tag>
          ) : null}
        </span>
      ),
    },
    {
      title: "次数",
      dataIndex: "count",
      width: 120,
      align: "right",
      render: (count: number) => (count || 0).toLocaleString(),
    },
  ];

  return (
    <Card
      title="采集站调用"
      extra={
        <Typography.Text type="secondary">
          按请求发生时的采集站计数。换首选站不会改历史。
        </Typography.Text>
      }
    >
      <Table
        rowKey="id"
        size="middle"
        pagination={false}
        loading={loading}
        columns={columns}
        dataSource={rows}
        locale={{ emptyText: "还没有采集站" }}
      />
    </Card>
  );
}
