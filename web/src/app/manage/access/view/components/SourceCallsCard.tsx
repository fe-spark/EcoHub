"use client";

import { useMemo } from "react";
import { Card, Empty, Skeleton, Space, Tag, Typography } from "antd";
import { PlaySquareOutlined } from "@ant-design/icons";
import type { EChartsCoreOption } from "echarts/core";
import EChartBox from "./EChartBox";
import { CHART_PALETTE } from "../constants";
import type { SourceCallRow } from "../types";
import styles from "../index.module.less";

const CARD_BODY_FLEX_STYLE = { flex: 1, display: "flex", flexDirection: "column" as const };

interface SourceCallsCardProps {
  sources: SourceCallRow[];
  loading?: boolean;
}

export default function SourceCallsCard({ sources, loading }: SourceCallsCardProps) {
  const validRows = useMemo(
    () => (sources || []).filter((item) => (item.count || 0) > 0),
    [sources],
  );
  const totalCount = useMemo(
    () => validRows.reduce((sum, item) => sum + item.count, 0),
    [validRows],
  );

  // 单图直出：直接在 Y 轴标注主站/停用标记，在柱条右侧展示次数与占比，取消重复列表
  const barOption = useMemo<EChartsCoreOption>(() => {
    const topRows = validRows.slice(0, 10);
    const reversed = [...topRows].reverse();

    const formatRowName = (r: SourceCallRow) => {
      const base = r.name || r.id;
      if (r.isPrimary) return `${base} [主]`;
      if (!r.enabled) return `${base} [停]`;
      return base;
    };

    return {
      tooltip: {
        trigger: "axis",
        axisPointer: { type: "shadow" },
        formatter: (params: unknown) => {
          const arr = Array.isArray(params) ? params : [params];
          const first = arr[0] as { dataIndex?: number } | undefined;
          if (!first || typeof first.dataIndex !== "number") return "";
          const row = reversed[first.dataIndex];
          if (!row) return "";
          const pct = totalCount > 0 ? ((row.count / totalCount) * 100).toFixed(1) : "0.0";
          const flags = [row.isPrimary ? "主站" : "", !row.enabled ? "已停用" : ""]
            .filter(Boolean)
            .join(" / ");
          return `${row.name || row.id}${flags ? ` (${flags})` : ""}<br/>点播：<b>${row.count.toLocaleString()}</b> 次 (${pct}%)`;
        },
      },
      grid: { left: 8, right: 86, top: 8, bottom: 8, containLabel: true },
      xAxis: { type: "value", splitLine: { lineStyle: { type: "dashed" } } },
      yAxis: {
        type: "category",
        data: reversed.map(formatRowName),
        axisLabel: { width: 120, overflow: "truncate" },
      },
      series: [
        {
          type: "bar",
          barMaxWidth: 20,
          label: {
            show: true,
            position: "right",
            fontSize: 11,
            formatter: (params: { value?: unknown }) => {
              const val = Number(params.value || 0);
              const pct = totalCount > 0 ? ((val / totalCount) * 100).toFixed(1) : "0.0";
              return `${val.toLocaleString()} (${pct}%)`;
            },
          },
          data: reversed.map((r, idx) => ({
            value: r.count,
            itemStyle: {
              color: CHART_PALETTE[(reversed.length - 1 - idx) % CHART_PALETTE.length],
              borderRadius: [0, 6, 6, 0],
            },
          })),
        },
      ],
    };
  }, [validRows, totalCount]);

  const activeSourceCount = validRows.length;

  return (
    <Card
      size="small"
      className={styles.fullHeightCard}
      styles={{ body: CARD_BODY_FLEX_STYLE }}
      title={
        <Space size={8}>
          <PlaySquareOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>片源线路点播分布</span>
        </Space>
      }
      extra={
        <Space size={6}>
          {activeSourceCount > 0 && (
            <Tag style={{ margin: 0 }}>{activeSourceCount} 条活跃线路</Tag>
          )}
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            共 {totalCount.toLocaleString()} 次
          </Typography.Text>
        </Space>
      }
    >
      {loading && validRows.length === 0 ? (
        <Skeleton active paragraph={{ rows: 5 }} />
      ) : validRows.length === 0 ? (
        <div className={styles.emptyCenter} style={{ minHeight: 260 }}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="当日暂无片源点播记录" />
        </div>
      ) : (
        <EChartBox
          option={barOption}
          height={Math.max(260, Math.min(360, validRows.slice(0, 10).length * 34))}
        />
      )}
    </Card>
  );
}
