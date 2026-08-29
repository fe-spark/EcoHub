"use client";

import React, { useState, useMemo } from "react";
import { Badge, Space } from "antd";
import dayjs from "dayjs";
import { useContainerWidth } from "./use-container-width";
import styles from "./index.module.less";

export type ScatterItem = {
  ts: string;
  path: string;
  latencyMs: number;
  status: number;
  clientType: string;
  resource?: string;
};

interface ScatterChartProps {
  logs?: ScatterItem[];
}

/** 对数轴下限：把 ≤10ms 铺在底带，避免被 10s 离群点压扁。 */
const Y_MIN_MS = 10;
const TICK_MS = [10, 50, 100, 200, 500, 1000, 2000, 5000, 10000, 20000, 30000];

function niceLatencyMax(maxMs: number) {
  const padded = Math.max(1000, maxMs * 1.08);
  const steps = [1000, 2000, 5000, 10000, 15000, 20000, 30000, 60000];
  return steps.find((s) => s >= padded) ?? Math.ceil(padded / 10000) * 10000;
}

function formatLatency(ms: number) {
  if (ms >= 1000) {
    const s = ms / 1000;
    return Number.isInteger(s) || s >= 10 ? `${Math.round(s)}s` : `${s.toFixed(1)}s`;
  }
  return `${Math.round(ms)}ms`;
}

function logRatio(ms: number, domainMax: number) {
  const lo = Math.log10(Y_MIN_MS);
  const hi = Math.log10(Math.max(domainMax, Y_MIN_MS * 10));
  const v = Math.log10(Math.max(ms, Y_MIN_MS));
  return Math.min(1, Math.max(0, (v - lo) / (hi - lo)));
}

export default function ScatterChart({ logs = [] }: ScatterChartProps) {
  const { ref, width } = useContainerWidth(500);
  const [hoverItem, setHoverItem] = useState<{
    item: ScatterItem;
    x: number;
    y: number;
  } | null>(null);

  const validLogs = useMemo(() => (logs || []).slice(0, 100), [logs]);

  const domainMax = useMemo(() => {
    if (validLogs.length === 0) return 1000;
    return niceLatencyMax(Math.max(...validLogs.map((l) => l.latencyMs || 0)));
  }, [validLogs]);

  const height = 220;
  const padLeft = 44;
  const padRight = 16;
  const padTop = 10;
  const padBottom = 28;
  const chartW = Math.max(50, width - padLeft - padRight);
  const chartH = height - padTop - padBottom;

  const yOf = (ms: number) => padTop + chartH - logRatio(ms, domainMax) * chartH;

  const timeRange = useMemo(() => {
    if (validLogs.length === 0) return { min: 0, max: 1 };
    const timestamps = validLogs.map((l) => dayjs(l.ts).valueOf());
    const min = Math.min(...timestamps);
    const max = Math.max(...timestamps);
    return { min, max: min === max ? min + 1000 : max };
  }, [validLogs]);

  const dots = useMemo(() => {
    return validLogs.map((l, i) => {
      const tsVal = dayjs(l.ts).valueOf();
      const xRatio =
        timeRange.max > timeRange.min && validLogs.length > 2
          ? (tsVal - timeRange.min) / (timeRange.max - timeRange.min)
          : validLogs.length > 1
            ? i / (validLogs.length - 1)
            : 0.5;

      const x = padLeft + xRatio * chartW;
      const y = padTop + chartH - logRatio(l.latencyMs || 0, domainMax) * chartH;

      let color = "#52c41a";
      if (l.status >= 400 || (l.latencyMs || 0) >= 500) {
        color = "#ff4d4f";
      } else if ((l.latencyMs || 0) > 200) {
        color = "#fa8c16";
      } else if ((l.latencyMs || 0) > 50) {
        color = "#1677ff";
      }

      const isSlowOrErr = l.status >= 400 || (l.latencyMs || 0) >= 500;

      return { ...l, x, y, color, isSlowOrErr };
    });
  }, [validLogs, domainMax, timeRange, chartW, chartH]);

  const yTicks = useMemo(() => {
    const ticks = TICK_MS.filter((ms) => ms >= Y_MIN_MS && ms <= domainMax * 1.001);
    if (!ticks.includes(domainMax)) {
      ticks.push(domainMax);
    }
    return ticks;
  }, [domainMax]);

  if (validLogs.length === 0) {
    return (
      <div className={styles.sparkEmpty}>
        <span>暂无请求散点数据</span>
      </div>
    );
  }

  const y200 = yOf(200);
  const y500 = yOf(500);
  const oldest = validLogs[validLogs.length - 1];
  const newest = validLogs[0];

  return (
    <div className={styles.scatterContainer}>
      <div className={styles.scatterHeader}>
        <Space size={12}>
          <Badge color="#52c41a" text="≤50ms" />
          <Badge color="#1677ff" text="50–200ms" />
          <Badge color="#fa8c16" text="200–500ms" />
          <Badge color="#ff4d4f" text=">500ms" />
        </Space>
      </div>

      <div className={styles.svgContainer} ref={ref}>
        <svg
          className={styles.scatterSvg}
          viewBox={`0 0 ${width} ${height}`}
          preserveAspectRatio="none"
          onMouseLeave={() => setHoverItem(null)}
        >
          <rect
            x={padLeft}
            y={padTop}
            width={chartW}
            height={Math.max(0, y500 - padTop)}
            fill="rgba(255, 77, 79, 0.05)"
          />

          {yTicks.map((ms) => {
            const y = yOf(ms);
            return (
              <g key={ms}>
                <line
                  x1={padLeft}
                  y1={y}
                  x2={padLeft + chartW}
                  y2={y}
                  stroke="var(--ant-color-border-secondary)"
                  strokeOpacity="0.7"
                />
                <text
                  x={padLeft - 6}
                  y={y + 3.5}
                  textAnchor="end"
                  className={styles.axisText}
                >
                  {formatLatency(ms)}
                </text>
              </g>
            );
          })}

          <line
            x1={padLeft}
            y1={y200}
            x2={padLeft + chartW}
            y2={y200}
            stroke="rgba(250, 140, 22, 0.45)"
            strokeDasharray="3 3"
          />
          <line
            x1={padLeft}
            y1={y500}
            x2={padLeft + chartW}
            y2={y500}
            stroke="rgba(255, 77, 79, 0.45)"
            strokeDasharray="4 3"
          />

          <text
            x={padLeft}
            y={padTop + chartH + 18}
            textAnchor="start"
            className={styles.axisText}
          >
            {dayjs(oldest.ts).format("HH:mm:ss")}
          </text>
          <text
            x={padLeft + chartW}
            y={padTop + chartH + 18}
            textAnchor="end"
            className={styles.axisText}
          >
            {dayjs(newest.ts).format("HH:mm:ss")} · 最新
          </text>

          {dots.map((dot, idx) => (
            <g
              key={`${dot.ts}-${idx}`}
              className={styles.scatterDotGroup}
              onMouseEnter={() =>
                setHoverItem({
                  item: dot,
                  x: dot.x,
                  y: dot.y,
                })
              }
            >
              {dot.isSlowOrErr && (
                <circle
                  cx={dot.x}
                  cy={dot.y}
                  r="7"
                  fill="none"
                  stroke={dot.color}
                  strokeWidth="1.25"
                  opacity="0.4"
                />
              )}
              <circle
                cx={dot.x}
                cy={dot.y}
                r={dot.isSlowOrErr ? 4 : 3.25}
                fill={dot.color}
                opacity={hoverItem?.item === dot ? 1 : 0.9}
              />
            </g>
          ))}
        </svg>

        {hoverItem && (
          <div
            className={styles.tooltipBox}
            style={{
              left: `${hoverItem.x}px`,
              transform:
                hoverItem.x > width * 0.65 ? "translateX(-100%)" : "translateX(12px)",
            }}
          >
            <div className={styles.tooltipTime}>
              {dayjs(hoverItem.item.ts).format("YYYY-MM-DD HH:mm:ss")}
            </div>
            <div className={styles.tooltipRow}>
              <span>耗时:</span>
              <b style={{ color: hoverItem.item.latencyMs > 200 ? "#ff4d4f" : "#52c41a" }}>
                {formatLatency(hoverItem.item.latencyMs)}
              </b>
            </div>
            <div className={styles.tooltipRow}>
              <span>状态:</span>
              <b>{hoverItem.item.status}</b>
            </div>
            <div className={styles.tooltipRow}>
              <span>路径:</span>
              <code>{hoverItem.item.path}</code>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
