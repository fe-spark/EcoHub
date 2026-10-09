"use client";

import React, { useEffect, useState } from "react";
import { Card, Table, Tag, Typography } from "antd";
import type { TableColumnsType } from "antd";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./inventory-stats.module.less";

interface LibraryInventory {
  films: number;
  playable: number;
  categories: number;
  failures: number;
}

interface SourceInventory {
  id: string;
  name: string;
  enabled: boolean;
  isPrimary: boolean;
  films: number;
  categories: number;
  failures: number;
}

interface InventoryStatsData {
  library: LibraryInventory;
  sources: SourceInventory[];
}

const metricItems: Array<{ key: keyof LibraryInventory; label: string; hint: string }> = [
  { key: "films", label: "片库影片", hint: "全部采集站入库" },
  { key: "playable", label: "有播放线路", hint: "至少一条播放线路" },
  { key: "categories", label: "展示分类", hint: "共享展示树" },
  { key: "failures", label: "失败记录", hint: "全部来源" },
];

function formatCount(value: number | undefined) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "—";
  }
  return value.toLocaleString("zh-CN");
}

export default function InventoryStats() {
  const { isAdmin } = useManagePermission();
  const [stats, setStats] = useState<InventoryStatsData | null>(null);

  useEffect(() => {
    let active = true;
    ApiGet<InventoryStatsData>("/manage/spider/clear/stats")
      .then((resp) => {
        if (active && resp.code === 0 && resp.data) {
          setStats(resp.data);
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  const columns: TableColumnsType<SourceInventory> = [
    {
      title: "采集站",
      dataIndex: "name",
      ellipsis: true,
      render: (_, row) => (
        <span className={styles.nameCell}>
          <span className={styles.sourceName}>{row.name}</span>
          {row.isPrimary ? <Tag color="gold">首选站</Tag> : null}
          {!row.enabled ? <Tag>已停用</Tag> : null}
        </span>
      ),
    },
    {
      title: "影片",
      dataIndex: "films",
      width: 120,
      align: "right",
      render: (value: number) => formatCount(value),
    },
    {
      title: "分类",
      dataIndex: "categories",
      width: 150,
      align: "right",
      render: (value: number) => (
        <span className={styles.categoryCell}>
          {value === 0 ? <Tag color="warning">缺分类</Tag> : null}
          <span>{formatCount(value)}</span>
        </span>
      ),
    },
    {
      title: "失败记录",
      dataIndex: "failures",
      width: 120,
      align: "right",
      render: (value: number) => formatCount(value),
    },
  ];

  return (
    <Card
      className={styles.panel}
      title="片库规模"
      extra={
        isAdmin ? (
          <Link href="/manage/system?tab=security" className={styles.securityLink}>
            数据安全
          </Link>
        ) : null
      }
    >
      <div className={styles.metrics}>
        {metricItems.map((item) => (
          <div key={item.key} className={styles.metric}>
            <div className={styles.metricValue}>{formatCount(stats?.library?.[item.key])}</div>
            <div className={styles.metricLabel}>{item.label}</div>
            <div className={styles.metricHint}>{item.hint}</div>
          </div>
        ))}
      </div>

      <div className={styles.sectionHead}>
        <span className={styles.sectionTitle}>各采集站</span>
        <span className={styles.sectionHint}>影片跟该站播放线路，分类跟该站已映射的展示分类</span>
      </div>
      <Table<SourceInventory>
        size="small"
        rowKey="id"
        pagination={false}
        columns={columns}
        dataSource={stats?.sources ?? []}
        locale={{ emptyText: stats ? "还没有采集站" : "—" }}
        scroll={{ x: 640 }}
      />
      <Typography.Text type="secondary" className={styles.note}>
        前台首页、分类和搜索只用首选站有播放线路的影片。同一部片子可以挂多个站的线路，各站影片相加会大于片库影片。数据重置清空的是整库。
      </Typography.Text>
    </Card>
  );
}
