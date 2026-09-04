"use client";

import React, { useId, useMemo, useState } from "react";
import { Empty } from "antd";
import { useContainerSize } from "./use-container-width";
import styles from "./index.module.less";

export type DonutSlice = {
  key?: string;
  name?: string;
  label?: string;
  value?: number;
  count?: number;
  pct?: number;
  color?: string;
  icon?: React.ReactNode;
  desc?: string;
};

export interface DonutChartProps {
  data?: DonutSlice[];
  slices?: DonutSlice[];
  title?: string;
  centerLabel?: string;
  unit?: string;
  size?: number;
}

const DEFAULT_PALETTE = [
  "#3b82f6",
  "#22c55e",
  "#f59e0b",
  "#a855f7",
  "#06b6d4",
  "#eab308",
  "#f43f5e",
  "#6366f1",
];

type SliceItem = {
  key: string;
  name: string;
  count: number;
  pct: number;
  pctFormatted: string;
  color: string;
  icon?: React.ReactNode;
};

function polar(cx: number, cy: number, r: number, a: number): [number, number] {
  return [cx + r * Math.cos(a), cy + r * Math.sin(a)];
}

function ringSector(cx: number, cy: number, rInner: number, rOuter: number, a0: number, a1: number) {
  const span = a1 - a0;
  if (span <= 0) return "";
  const large = span > Math.PI ? 1 : 0;
  const [x0, y0] = polar(cx, cy, rOuter, a0);
  const [x1, y1] = polar(cx, cy, rOuter, a1);
  const [x2, y2] = polar(cx, cy, rInner, a1);
  const [x3, y3] = polar(cx, cy, rInner, a0);
  return `M${x0.toFixed(2)} ${y0.toFixed(2)} A${rOuter} ${rOuter} 0 ${large} 1 ${x1.toFixed(2)} ${y1.toFixed(2)} L${x2.toFixed(2)} ${y2.toFixed(2)} A${rInner} ${rInner} 0 ${large} 0 ${x3.toFixed(2)} ${y3.toFixed(2)} Z`;
}

function fullRing(cx: number, cy: number, rInner: number, rOuter: number) {
  return [
    `M ${cx} ${cy - rOuter}`,
    `A ${rOuter} ${rOuter} 0 1 1 ${cx} ${cy + rOuter}`,
    `A ${rOuter} ${rOuter} 0 1 1 ${cx} ${cy - rOuter}`,
    `M ${cx} ${cy - rInner}`,
    `A ${rInner} ${rInner} 0 1 0 ${cx} ${cy + rInner}`,
    `A ${rInner} ${rInner} 0 1 0 ${cx} ${cy - rInner}`,
    "Z",
  ].join(" ");
}

export default function DonutChart({
  data,
  slices: propSlices,
  title = "总计",
  centerLabel,
  unit = "次",
}: DonutChartProps) {
  const uid = useId().replace(/:/g, "");
  const { ref, width, height } = useContainerSize(280, 220);
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);

  const { list, total } = useMemo(() => {
    const rawList = propSlices || data || [];
    const validItems = rawList.filter((item) => (item.count ?? item.value ?? 0) > 0);
    const sum = validItems.reduce((acc, item) => acc + (item.count ?? item.value ?? 0), 0);
    const formatted: SliceItem[] = validItems.map((item, idx) => {
      const count = item.count ?? item.value ?? 0;
      const pct = sum > 0 ? (count / sum) * 100 : 0;
      const name = item.label || item.name || `项 ${idx + 1}`;
      return {
        key: item.key || name || String(idx),
        name,
        count,
        pct,
        pctFormatted: pct.toFixed(1),
        color: item.color || DEFAULT_PALETTE[idx % DEFAULT_PALETTE.length],
        icon: item.icon,
      };
    });
    formatted.sort((a, b) => b.count - a.count);
    return { list: formatted, total: sum };
  }, [propSlices, data]);

  const size = Math.max(136, Math.floor(Math.min(width, height) * 0.82));
  const cx = size / 2;
  const cy = size / 2;
  const stroke = Math.max(24, Math.min(44, Math.round(size * 0.18)));
  const rOuter = size / 2 - 8;
  const rInner = rOuter - stroke;
  const trackR = (rOuter + rInner) / 2;
  const decoR = rOuter + 6;

  const sectors = useMemo(() => {
    const gap = list.length <= 1 ? 0 : Math.min(0.04, 0.5 / list.length);
    const result = [];
    let cursor = -Math.PI / 2;
    for (const item of list) {
      const span = (item.pct / 100) * Math.PI * 2;
      const pad = span > gap * 2 ? gap / 2 : 0;
      const a0 = cursor + pad;
      const a1 = cursor + span - pad;
      cursor += span;
      const pathD =
        list.length === 1
          ? fullRing(cx, cy, rInner, rOuter)
          : ringSector(cx, cy, rInner, rOuter, a0, a1);
      result.push({ ...item, pathD });
    }
    return result;
  }, [list, cx, cy, rInner, rOuter]);

  if (total === 0 || list.length === 0) {
    return (
      <div className={styles.pieCardContainer}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分布数据" />
      </div>
    );
  }

  const dominant = list[0];
  const active = list.find((d) => d.key === hoveredKey) || null;
  const focus = active || dominant;
  const centerTitle = active ? active.name : centerLabel || title;
  const centerPct = active || list.length === 1 ? focus.pctFormatted : dominant.pctFormatted;
  const centerSub = active
    ? `${active.count.toLocaleString()} ${unit}`
    : `共 ${total.toLocaleString()} ${unit}`;

  const glowId = `ring-glow-${uid}`;
  const glowSoftId = `ring-soft-${uid}`;

  return (
    <div className={styles.ringChart}>
      <div ref={ref} className={styles.ringStage}>
        <div className={styles.ringWrap} style={{ width: size, height: size }}>
          <svg
            width={size}
            height={size}
            viewBox={`0 0 ${size} ${size}`}
            className={styles.ringSvg}
          >
            <defs>
              <filter id={glowId} x="-40%" y="-40%" width="180%" height="180%">
                <feGaussianBlur stdDeviation="3.5" result="b" />
                <feMerge>
                  <feMergeNode in="b" />
                  <feMergeNode in="SourceGraphic" />
                </feMerge>
              </filter>
              <filter id={glowSoftId} x="-50%" y="-50%" width="200%" height="200%">
                <feGaussianBlur stdDeviation="6" result="b" />
                <feMerge>
                  <feMergeNode in="b" />
                </feMerge>
              </filter>
            </defs>

            <circle
              cx={cx}
              cy={cy}
              r={decoR}
              fill="none"
              strokeWidth="1"
              strokeDasharray="2 6"
              className={styles.ringDeco}
            />
            <circle
              cx={cx}
              cy={cy}
              r={trackR}
              fill="none"
              strokeWidth={stroke}
              className={styles.ringTrack}
            />
            <circle
              cx={cx}
              cy={cy}
              r={Math.max(8, rInner - 8)}
              fill="none"
              strokeWidth="1"
              className={styles.ringInner}
            />

            {sectors.map((slice) => {
              const isActive = hoveredKey === slice.key;
              const dimmed = hoveredKey !== null && !isActive;
              const showSoftGlow = list.length <= 10;
              return (
                <g key={slice.key}>
                  {showSoftGlow ? (
                    <path
                      d={slice.pathD}
                      fill={slice.color}
                      opacity={dimmed ? 0.18 : 0.22}
                      filter={`url(#${glowSoftId})`}
                      className={styles.ringGlowPath}
                    />
                  ) : null}
                  <path
                    d={slice.pathD}
                    fill={slice.color}
                    fillOpacity={dimmed ? 0.35 : 1}
                    filter={isActive ? `url(#${glowId})` : undefined}
                    className={styles.ringSlice}
                    onMouseEnter={() => setHoveredKey(slice.key)}
                    onMouseLeave={() => setHoveredKey(null)}
                  />
                </g>
              );
            })}
          </svg>

          <div className={styles.ringCenter}>
            <div className={styles.ringCenterLabel}>{centerTitle}</div>
            <div
              className={styles.ringCenterVal}
              style={{
                color: focus.color,
                fontSize: size < 180 ? 26 : size < 230 ? 32 : 38,
              }}
            >
              {centerPct}
              <span className={styles.ringCenterUnit}>%</span>
            </div>
            <div className={styles.ringCenterSub}>{centerSub}</div>
          </div>
        </div>
      </div>

      <div className={styles.ringLegend}>
        {list.map((item) => {
          const isActive = hoveredKey === item.key;
          return (
            <button
              key={item.key}
              type="button"
              className={`${styles.ringChip} ${isActive ? styles.ringChipActive : ""}`}
              onMouseEnter={() => setHoveredKey(item.key)}
              onMouseLeave={() => setHoveredKey(null)}
            >
              <span className={styles.ringChipDot} style={{ background: item.color }} />
              <span className={styles.ringChipName}>{item.name}</span>
              <span className={styles.ringChipPct} style={{ color: item.color }}>
                {item.pctFormatted}%
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
