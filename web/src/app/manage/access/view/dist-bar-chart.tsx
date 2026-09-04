"use client";

import React, { useMemo, useState } from "react";
import { Empty, Tooltip } from "antd";
import type { DonutSlice } from "./donut-chart";
import styles from "./index.module.less";

const DEFAULT_PALETTE = [
  "#fa541c", // 寻片搜索 - 橙
  "#52c41a", // 分类与列表 - 绿
  "#fa8c16", // 影视点播 - 暖金
  "#722ed1", // 配置同步 - 紫
  "#1677ff", // 蓝
  "#13c2c2", // 青
];

export type DistBarItem = DonutSlice;

export interface DistBarChartProps {
  data?: DistBarItem[];
  slices?: DistBarItem[];
  unit?: string;
  title?: string;
}

type FormattedItem = {
  key: string;
  name: string;
  count: number;
  pct: number;
  pctFormatted: string;
  color: string;
  icon?: React.ReactNode;
  desc?: string;
};

export default function DistBarChart({
  data,
  slices: propSlices,
  unit = "次",
  title = "接口总调用构成 (100%)",
}: DistBarChartProps) {
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);

  const { list, total } = useMemo(() => {
    const rawList = propSlices || data || [];
    const validItems = rawList.filter((item) => (item.count ?? item.value ?? 0) > 0);
    const sum = validItems.reduce((acc, item) => acc + (item.count ?? item.value ?? 0), 0);

    const formatted: FormattedItem[] = validItems.map((item, idx) => {
      const count = item.count ?? item.value ?? 0;
      const pct = sum > 0 ? (count / sum) * 100 : 0;
      const name = item.label || item.name || `接口 ${idx + 1}`;
      return {
        key: item.key || name || String(idx),
        name,
        count,
        pct,
        pctFormatted: pct.toFixed(1),
        color: item.color || DEFAULT_PALETTE[idx % DEFAULT_PALETTE.length],
        icon: item.icon,
        desc: item.desc,
      };
    });

    // 默认按调用次数降序排列
    formatted.sort((a, b) => b.count - a.count);
    return { list: formatted, total: sum };
  }, [propSlices, data]);

  if (total === 0 || list.length === 0) {
    return (
      <div className={styles.pieCardContainer}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无接口调用分布数据" />
      </div>
    );
  }

  return (
    <div className={styles.distBarContainer}>
      {/* 顶部：全景分段比例胶囊条 */}
      <div className={styles.distTopSection}>
        <div className={styles.distRatioHeader}>
          <span className={styles.distRatioTitle}>{title}</span>
          <span className={styles.distRatioTotal}>
            调用总量：<b>{total.toLocaleString()}</b> {unit}
          </span>
        </div>

        <div className={styles.distSegmentTrack}>
          {list.map((item) => {
            const isActive = hoveredKey === item.key;
            return (
              <Tooltip
                key={item.key}
                title={`${item.name} · ${item.pctFormatted}% (${item.count.toLocaleString()} ${unit})`}
              >
                <div
                  className={`${styles.distSegmentSlice} ${isActive ? styles.distSegmentActive : ""}`}
                  style={{
                    width: `${item.pct}%`,
                    background: `linear-gradient(90deg, ${item.color}cc, ${item.color})`,
                    boxShadow: isActive ? `0 0 12px ${item.color}` : undefined,
                  }}
                  onMouseEnter={() => setHoveredKey(item.key)}
                  onMouseLeave={() => setHoveredKey(null)}
                />
              </Tooltip>
            );
          })}
        </div>
      </div>

      {/* 下方：按调用量排行的横向胶囊条形列表 */}
      <div className={styles.distRankedList}>
        {list.map((item, idx) => {
          const isActive = hoveredKey === item.key;
          const isDimmed = hoveredKey !== null && !isActive;
          const rankCls =
            idx === 0
              ? styles.distRank1
              : idx === 1
                ? styles.distRank2
                : idx === 2
                  ? styles.distRank3
                  : "";

          return (
            <div
              key={item.key}
              className={`${styles.distRankedRow} ${isActive ? styles.distRowActive : ""} ${
                isDimmed ? styles.distRowDimmed : ""
              }`}
              onMouseEnter={() => setHoveredKey(item.key)}
              onMouseLeave={() => setHoveredKey(null)}
            >
              {/* 左侧：排名、图标、名称与语义说明 */}
              <div className={styles.distRowMeta}>
                <span className={`${styles.distRankNum} ${rankCls}`}>#{idx + 1}</span>
                {item.icon && (
                  <span
                    className={styles.distIconWrap}
                    style={{
                      color: item.color,
                      background: `${item.color}1a`,
                    }}
                  >
                    {item.icon}
                  </span>
                )}
                <div className={styles.distTitles}>
                  <span className={styles.distName}>{item.name}</span>
                  {item.desc && <span className={styles.distDesc}>{item.desc}</span>}
                </div>
              </div>

              {/* 中间：全宽平滑进度条 */}
              <div className={styles.distProgressCol}>
                <div className={styles.distTrack}>
                  <div
                    className={styles.distFill}
                    style={{
                      width: `${Math.max(item.count > 0 ? 2 : 0, item.pct)}%`,
                      background: `linear-gradient(90deg, ${item.color}88, ${item.color})`,
                      boxShadow: isActive ? `0 0 8px ${item.color}aa` : undefined,
                    }}
                  />
                </div>
              </div>

              {/* 右侧：具体调用量与百分比徽标 */}
              <div className={styles.distStatsCol}>
                <span className={styles.distCountText}>
                  {item.count.toLocaleString()}
                  <span className={styles.distUnitText}>{unit}</span>
                </span>
                <span
                  className={styles.distPctBadge}
                  style={{
                    color: item.color,
                    background: `${item.color}15`,
                    border: `1px solid ${item.color}35`,
                  }}
                >
                  {item.pctFormatted}%
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
