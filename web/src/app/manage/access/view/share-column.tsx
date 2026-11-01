"use client";

import { useId, useMemo, useState } from "react";
import { Tooltip } from "antd";
import { useContainerWidth } from "./use-container-width";
import type { ChartSlice } from "./types";
import styles from "./index.module.less";

const PALETTE = ["#1677ff", "#52c41a", "#fa8c16", "#722ed1", "#13c2c2", "#fa541c", "#eb2f96", "#2f54eb"];

function formatShare(count: number, total: number) {
  if (total <= 0) return "0%";
  const share = (count / total) * 100;
  return Number.isInteger(share) ? `${share}%` : `${share.toFixed(1)}%`;
}

function formatTick(n: number) {
  return Number.isInteger(n) ? n.toLocaleString() : n.toFixed(1);
}

function roundedBar(x: number, y: number, w: number, h: number) {
  const r = Math.min(7, w / 2, h / 2);
  if (h <= r * 2) {
    return `M${x} ${y + h} L${x} ${y} L${x + w} ${y} L${x + w} ${y + h} Z`;
  }
  return [
    `M${x} ${y + h}`,
    `L${x} ${y + r}`,
    `Q${x} ${y} ${x + r} ${y}`,
    `L${x + w - r} ${y}`,
    `Q${x + w} ${y} ${x + w} ${y + r}`,
    `L${x + w} ${y + h}`,
    "Z",
  ].join(" ");
}

function shortName(name: string, slot: number, rotate: boolean) {
  const max = rotate ? 10 : Math.max(4, Math.floor(slot / 8));
  if (name.length <= max) return name;
  return `${name.slice(0, Math.max(1, max - 1))}…`;
}

type Row = {
  key: string;
  name: string;
  count: number;
  color: string;
};

export default function ShareColumn({
  slices,
  unit = "次",
}: {
  slices: ChartSlice[];
  unit?: string;
}) {
  const uid = useId().replace(/:/g, "");
  const { ref, width } = useContainerWidth(320);
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);

  const rows = useMemo<Row[]>(() => {
    return slices
      .map((item, idx) => ({
        key: item.key || item.name || item.label || String(idx),
        name: item.name || item.label || `项 ${idx + 1}`,
        count: item.count ?? item.value ?? 0,
        color: item.color || PALETTE[idx % PALETTE.length],
      }))
      .filter((item) => item.count > 0)
      .sort((a, b) => b.count - a.count);
  }, [slices]);

  const total = rows.reduce((sum, item) => sum + item.count, 0);
  const max = rows[0]?.count || 1;
  const padLeft = 36;
  const padRight = 8;
  const padTop = 20;
  const plotH = 108;
  const innerW = Math.max(48, width - padLeft - padRight);
  const slot = rows.length > 0 ? innerW / rows.length : innerW;
  const rotate = rows.length > 1 && slot < 52;
  const padBottom = rotate ? 68 : 38;
  const height = padTop + plotH + padBottom;
  const plotBottom = padTop + plotH;
  const fraction = rows.length <= 1 ? 0.42 : rows.length === 2 ? 0.48 : 0.62;
  const hardCap = rows.length <= 2 ? 148 : 44;
  const barW = Math.min(hardCap, Math.max(8, slot * fraction));
  const ticks = max <= 1 || max < 10 ? [max, 0] : [max, max / 2, 0];

  if (rows.length === 0) return null;

  const yOf = (count: number) => plotBottom - (count / max) * plotH;

  return (
    <div className={styles.columnChart} ref={ref}>
      <div className={styles.columnHead}>
        <span>合计</span>
        <span className={styles.columnTotal}>
          <b>{total.toLocaleString()}</b> {unit}
        </span>
      </div>
      <div className={styles.columnPlotBox} style={{ height }}>
        <svg className={styles.columnPlot} width={width} height={height} aria-hidden>
          <defs>
            {rows.map((item, idx) => (
              <linearGradient key={item.key} id={`${uid}-bar-${idx}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={item.color} stopOpacity="1" />
                <stop offset="100%" stopColor={item.color} stopOpacity="0.45" />
              </linearGradient>
            ))}
          </defs>
          {ticks.map((tick) => {
            const y = yOf(tick);
            return (
              <g key={tick}>
                <line
                  x1={padLeft}
                  x2={width - padRight}
                  y1={y}
                  y2={y}
                  className={tick === 0 ? styles.columnAxis : styles.columnGrid}
                />
                <text x={padLeft - 6} y={y + 3} textAnchor="end" className={styles.columnTick}>
                  {formatTick(tick)}
                </text>
              </g>
            );
          })}
          {rows.map((item, idx) => {
            const x = padLeft + idx * slot + (slot - barW) / 2;
            const y = yOf(item.count);
            const h = Math.max(4, plotBottom - y);
            const cx = x + barW / 2;
            const dimmed = hoveredKey !== null && hoveredKey !== item.key;
            const nameY = plotBottom + 16;
            return (
              <g key={item.key} opacity={dimmed ? 0.35 : 1}>
                <path d={roundedBar(x, plotBottom - h, barW, h)} fill={`url(#${uid}-bar-${idx})`} />
                {(barW >= 22 || rows.length <= 3) && (
                  <text x={cx} y={Math.max(12, plotBottom - h - 6)} textAnchor="middle" className={styles.columnValue}>
                    {item.count.toLocaleString()}
                  </text>
                )}
                <text
                  x={cx}
                  y={nameY}
                  textAnchor={rotate ? "start" : "middle"}
                  transform={rotate ? `rotate(40 ${cx} ${nameY})` : undefined}
                  className={styles.columnName}
                >
                  {shortName(item.name, slot, rotate)}
                </text>
                {!rotate && (
                  <text x={cx} y={plotBottom + 30} textAnchor="middle" className={styles.columnPct} fill={item.color}>
                    {formatShare(item.count, total)}
                  </text>
                )}
              </g>
            );
          })}
        </svg>
        {rows.map((item, idx) => (
          <Tooltip
            key={item.key}
            title={`${item.name} · ${item.count.toLocaleString()} ${unit} · ${formatShare(item.count, total)}`}
          >
            <button
              type="button"
              className={styles.columnHit}
              style={{ left: padLeft + idx * slot, top: padTop, width: slot, height: height - padTop }}
              aria-label={`${item.name} ${item.count.toLocaleString()} ${unit}，${formatShare(item.count, total)}`}
              onMouseEnter={() => setHoveredKey(item.key)}
              onMouseLeave={() => setHoveredKey(null)}
              onFocus={() => setHoveredKey(item.key)}
              onBlur={() => setHoveredKey(null)}
            />
          </Tooltip>
        ))}
      </div>
    </div>
  );
}
