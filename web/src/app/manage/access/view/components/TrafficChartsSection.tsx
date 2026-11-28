"use client";

import { useMemo } from "react";
import { Card, Col, Empty, Row, Skeleton, Typography, theme } from "antd";
import dayjs from "dayjs";
import type { EChartsCoreOption } from "echarts/core";
import EChartBox from "./EChartBox";
import {
  ACTION_META,
  BROWSER_META,
  CHART_PALETTE,
  CLIENT_META,
  HIST_BUCKETS,
  OS_META,
} from "../constants";
import type { AnalyticsModule, AppPlatform, Overview } from "../types";
import styles from "../index.module.less";

const CARD_BODY_FLEX_STYLE = { flex: 1, display: "flex", flexDirection: "column" as const };

interface TrafficChartsSectionProps {
  globalOverview?: Overview | null;
  scopedOverview?: Overview | null;
  module: AnalyticsModule;
  platform: AppPlatform;
  loading?: boolean;
}

interface ChartEntry {
  name: string;
  value: number;
  color: string;
}

function toEntries(
  source: Record<string, number> | undefined,
  metaMap?: Record<string, { label: string; color: string }>,
): ChartEntry[] {
  return Object.entries(source || {})
    .filter(([, value]) => value > 0)
    .sort((a, b) => b[1] - a[1])
    .map(([key, value], idx) => {
      const lower = key.toLowerCase();
      const meta = metaMap?.[lower];
      return {
        name: meta?.label || key,
        value,
        color: meta?.color || CHART_PALETTE[idx % CHART_PALETTE.length],
      };
    });
}

function buildDonutOption(entries: ChartEntry[], unit = "次"): EChartsCoreOption {
  const total = entries.reduce((sum, item) => sum + item.value, 0);
  return {
    tooltip: { trigger: "item", formatter: `{b}: {c} ${unit} ({d}%)` },
    legend: { bottom: 0, type: "scroll" },
    series: [
      {
        type: "pie",
        radius: ["48%", "72%"],
        center: ["50%", "44%"],
        avoidLabelOverlap: true,
        itemStyle: { borderRadius: 6, borderWidth: 2, borderColor: "transparent" },
        label: {
          show: true,
          position: "center",
          formatter: () => `${total.toLocaleString()}\n总${unit}`,
          fontSize: 13,
          fontWeight: 600,
          lineHeight: 18,
        },
        data: entries.map((item) => ({
          name: item.name,
          value: item.value,
          itemStyle: { color: item.color },
        })),
      },
    ],
  };
}

function buildBarOption(entries: ChartEntry[], horizontal = false): EChartsCoreOption {
  const labels = entries.map((e) => e.name);
  const values = entries.map((e, idx) => ({
    value: e.value,
    itemStyle: {
      color: e.color || CHART_PALETTE[idx % CHART_PALETTE.length],
      borderRadius: horizontal ? [0, 6, 6, 0] : [6, 6, 0, 0],
    },
  }));
  return {
    tooltip: { trigger: "axis", axisPointer: { type: "shadow" } },
    grid: {
      left: 12,
      right: horizontal ? 44 : 18,
      top: 16,
      bottom: 12,
      containLabel: true,
    },
    xAxis: horizontal
      ? { type: "value", splitLine: { lineStyle: { type: "dashed" } } }
      : {
          type: "category",
          data: labels,
          axisLabel: { interval: 0, rotate: labels.length > 5 ? 20 : 0 },
        },
    yAxis: horizontal
      ? {
          type: "category",
          data: [...labels].reverse(),
          axisLabel: { width: 110, overflow: "truncate" },
        }
      : { type: "value", splitLine: { lineStyle: { type: "dashed" } } },
    series: [
      {
        type: "bar",
        barMaxWidth: horizontal ? 20 : 28,
        label: horizontal
          ? { show: true, position: "right", fontSize: 11 }
          : undefined,
        data: horizontal ? [...values].reverse() : values,
      },
    ],
  };
}

export default function TrafficChartsSection({
  globalOverview,
  scopedOverview,
  module,
  platform,
  loading,
}: TrafficChartsSectionProps) {
  const { token } = theme.useToken();
  const active = (module === "all" ? globalOverview : scopedOverview) ?? globalOverview;

  const trendOption = useMemo<EChartsCoreOption>(() => {
    const series = active?.series || [];
    const labels = series.map((pt) => (pt.t ? dayjs(pt.t).format("HH:mm") : ""));
    if (module === "all") {
      return {
        tooltip: { trigger: "axis" },
        legend: { top: 0, right: 8 },
        grid: { left: 12, right: 16, top: 38, bottom: 10, containLabel: true },
        xAxis: { type: "category", boundaryGap: false, data: labels },
        yAxis: { type: "value", splitLine: { lineStyle: { type: "dashed" } } },
        series: [
          {
            name: "全站总 PV",
            type: "line",
            smooth: true,
            showSymbol: false,
            lineStyle: { width: 2.5, color: token.colorPrimary },
            itemStyle: { color: token.colorPrimary },
            areaStyle: {
              color: {
                type: "linear",
                x: 0,
                y: 0,
                x2: 0,
                y2: 1,
                colorStops: [
                  { offset: 0, color: "rgba(250, 140, 22, 0.28)" },
                  { offset: 1, color: "rgba(250, 140, 22, 0.02)" },
                ],
              },
            },
            data: series.map((pt) => pt.pv || 0),
          },
          {
            name: "Web 网页",
            type: "line",
            smooth: true,
            showSymbol: false,
            lineStyle: { width: 2, color: "#1677ff" },
            itemStyle: { color: "#1677ff" },
            data: series.map((pt) => pt.webPv || 0),
          },
          {
            name: "App 客户端",
            type: "line",
            smooth: true,
            showSymbol: false,
            lineStyle: { width: 2, color: "#52c41a" },
            itemStyle: { color: "#52c41a" },
            data: series.map((pt) => pt.appPv || 0),
          },
          {
            name: "TVBox 订阅",
            type: "line",
            smooth: true,
            showSymbol: false,
            lineStyle: { width: 2, color: "#722ed1" },
            itemStyle: { color: "#722ed1" },
            data: series.map((pt) => pt.providePv || 0),
          },
        ],
      };
    }

    return {
      tooltip: { trigger: "axis" },
      grid: { left: 12, right: 16, top: 20, bottom: 10, containLabel: true },
      xAxis: { type: "category", boundaryGap: false, data: labels },
      yAxis: { type: "value", splitLine: { lineStyle: { type: "dashed" } } },
      series: [
        {
          name: "访问量",
          type: "line",
          smooth: true,
          showSymbol: false,
          lineStyle: { width: 2.5, color: token.colorPrimary },
          itemStyle: { color: token.colorPrimary },
          areaStyle: {
            color: {
              type: "linear",
              x: 0,
              y: 0,
              x2: 0,
              y2: 1,
              colorStops: [
                { offset: 0, color: "rgba(250, 140, 22, 0.30)" },
                { offset: 1, color: "rgba(250, 140, 22, 0.02)" },
              ],
            },
          },
          data: series.map((pt) => pt.pv || 0),
        },
      ],
    };
  }, [active?.series, module, token.colorPrimary]);

  // Row 1 右侧构成环形图：全站看多端流量占比，分端看行为构成
  const compositionEntries = useMemo(() => {
    if (module === "all") {
      const client = globalOverview?.client || {};
      const aggregated: Record<string, number> = {
        web: client.web || 0,
        app:
          (client.android || 0) +
          (client.harmony || 0) +
          (client.ios || 0) +
          (client.app || 0),
        tvbox: client.tvbox || 0,
      };
      return toEntries(aggregated, CLIENT_META);
    }
    return toEntries(active?.action, ACTION_META);
  }, [module, globalOverview?.client, active?.action]);

  // Row 2 左侧：确保与 Row 1 环形图完全互补不重复
  const secondaryLeft = useMemo<ChartEntry[]>(() => {
    if (module === "all") {
      return toEntries(globalOverview?.action, ACTION_META);
    }
    if (module === "web") {
      return toEntries(active?.browsers, BROWSER_META);
    }
    if (module === "app") {
      return platform === "all"
        ? toEntries(active?.platforms, CLIENT_META)
        : toEntries(active?.versions);
    }
    // TVBox 维度展示接口响应状态构成（正常 / 4xx / 5xx），与 Row 1 行为分布互补
    const totalPv = active?.pv ?? 0;
    const err4 = active?.err4 ?? 0;
    const err5 = active?.err5 ?? 0;
    const okCount = Math.max(0, totalPv - err4 - err5);
    return [
      { name: "正常响应 (2xx/3xx)", value: okCount, color: "#52c41a" },
      { name: "客户端异常 (4xx)", value: err4, color: "#faad14" },
      { name: "服务端异常 (5xx)", value: err5, color: "#ff4d4f" },
    ].filter((item) => item.value > 0);
  }, [module, platform, globalOverview?.action, active]);

  // Row 2 右侧：耗时分布 / 操作系统 / 机型 TOP 10
  const secondaryRight = useMemo<ChartEntry[]>(() => {
    if (module === "all" || module === "tvbox") {
      return HIST_BUCKETS.map((b, idx) => ({
        name: b.label,
        value: globalOverview?.hist?.[b.key] || 0,
        color: CHART_PALETTE[idx % CHART_PALETTE.length],
      })).filter((item) => item.value > 0);
    }
    if (module === "web") {
      return toEntries(active?.os, OS_META);
    }
    return toEntries(active?.models).slice(0, 10);
  }, [module, globalOverview?.hist, active]);

  const compositionTitle = module === "all" ? "多端流量占比" : "行为构成分布";
  const leftTitle =
    module === "all"
      ? "全站行为分布"
      : module === "web"
        ? "浏览器分布"
        : module === "app"
          ? platform === "all"
            ? "App 平台分布"
            : "App 版本分布"
          : "TVBox 接口响应状态";
  const rightTitle =
    module === "all"
      ? "接口响应耗时分布"
      : module === "tvbox"
        ? "全站接口响应耗时参考"
        : module === "web"
          ? "操作系统分布"
          : "设备机型 TOP 10";

  const leftHorizontal = module === "all" || module === "web";
  const rightHorizontal = module === "web" || module === "app";

  return (
    <Row gutter={[16, 16]}>
      <Col xs={24} lg={16}>
        <Card
          title="24 小时流量走势"
          size="small"
          className={styles.fullHeightCard}
          styles={{ body: CARD_BODY_FLEX_STYLE }}
          extra={
            module === "all" ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                点击图例可切换各端曲线
              </Typography.Text>
            ) : undefined
          }
        >
          {loading && !active ? (
            <Skeleton active paragraph={{ rows: 5 }} />
          ) : (active?.series?.length ?? 0) === 0 ? (
            <div className={styles.emptyCenter} style={{ minHeight: 290 }}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无流量走势数据" />
            </div>
          ) : (
            <EChartBox option={trendOption} height={290} />
          )}
        </Card>
      </Col>

      <Col xs={24} lg={8}>
        <Card
          title={compositionTitle}
          size="small"
          className={styles.fullHeightCard}
          styles={{ body: CARD_BODY_FLEX_STYLE }}
        >
          {loading && !active ? (
            <Skeleton active paragraph={{ rows: 5 }} />
          ) : compositionEntries.length === 0 ? (
            <div className={styles.emptyCenter} style={{ minHeight: 290 }}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无构成数据" />
            </div>
          ) : (
            <EChartBox option={buildDonutOption(compositionEntries)} height={290} />
          )}
        </Card>
      </Col>

      <Col xs={24} lg={12}>
        <Card
          title={leftTitle}
          size="small"
          className={styles.fullHeightCard}
          styles={{ body: CARD_BODY_FLEX_STYLE }}
        >
          {loading && !active ? (
            <Skeleton active paragraph={{ rows: 4 }} />
          ) : secondaryLeft.length === 0 ? (
            <div className={styles.emptyCenter} style={{ minHeight: 250 }}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分布数据" />
            </div>
          ) : module === "tvbox" ? (
            <EChartBox option={buildDonutOption(secondaryLeft)} height={250} />
          ) : (
            <EChartBox option={buildBarOption(secondaryLeft, leftHorizontal)} height={250} />
          )}
        </Card>
      </Col>

      <Col xs={24} lg={12}>
        <Card
          title={rightTitle}
          size="small"
          className={styles.fullHeightCard}
          styles={{ body: CARD_BODY_FLEX_STYLE }}
        >
          {loading && !active ? (
            <Skeleton active paragraph={{ rows: 4 }} />
          ) : secondaryRight.length === 0 ? (
            <div className={styles.emptyCenter} style={{ minHeight: 250 }}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分布数据" />
            </div>
          ) : (
            <EChartBox option={buildBarOption(secondaryRight, rightHorizontal)} height={250} />
          )}
        </Card>
      </Col>
    </Row>
  );
}
