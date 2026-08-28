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

export default function ScatterChart({ logs = [] }: ScatterChartProps) {
  const { ref, width } = useContainerWidth(500);
  const [hoverItem, setHoverItem] = useState<{
    item: ScatterItem;
    x: number;
    y: number;
  } | null>(null);

  const validLogs = useMemo(() => {
    return (logs || []).slice(0, 100);
  }, [logs]);

  const maxLatency = useMemo(() => {
    if (validLogs.length === 0) return 500;
    const max = Math.max(...validLogs.map((l) => l.latencyMs || 0));
    return Math.max(300, Math.ceil((max * 1.15) / 100) * 100);
  }, [validLogs]);

  const height = 170;
  const padLeft = 46;
  const padRight = 20;
  const padTop = 15;
  const padBottom = 24;
  const chartW = Math.max(50, width - padLeft - padRight);
  const chartH = height - padTop - padBottom;

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
      const yRatio = Math.min(1, Math.max(0, (l.latencyMs || 0) / maxLatency));
      const y = padTop + chartH - yRatio * chartH;

      let color = "#52c41a";
      if (l.status >= 400 || (l.latencyMs || 0) >= 500) {
        color = "#ff4d4f";
      } else if ((l.latencyMs || 0) > 200) {
        color = "#fa8c16";
      } else if ((l.latencyMs || 0) > 50) {
        color = "#1677ff";
      }

      const isSlowOrErr = l.status >= 400 || (l.latencyMs || 0) >= 500;

      return {
        ...l,
        x,
        y,
        color,
        isSlowOrErr,
      };
    });
  }, [validLogs, maxLatency, timeRange, chartW, chartH]);

  if (validLogs.length === 0) {
    return (
      <div className={styles.sparkEmpty}>
        <span>暂无请求散点数据</span>
      </div>
    );
  }

  const y200 = padTop + chartH - (200 / maxLatency) * chartH;
  const y500 = padTop + chartH - (500 / maxLatency) * chartH;

  return (
    <div className={styles.scatterContainer} ref={ref}>
      <div className={styles.scatterHeader}>
        <Space size={12}>
          <Badge color="#52c41a" text="≤50ms" />
          <Badge color="#1677ff" text="50-200ms" />
          <Badge color="#fa8c16" text="200-500ms" />
          <Badge color="#ff4d4f" text=">500ms" />
        </Space>
      </div>

      <div className={styles.svgContainer}>
        <svg
          className={styles.trendSvg}
          viewBox={`0 0 ${width} ${height}`}
          width={width}
          height={height}
          onMouseLeave={() => setHoverItem(null)}
        >
          {/* 网格基准线 */}
          <line
            x1={padLeft}
            y1={padTop + chartH}
            x2={padLeft + chartW}
            y2={padTop + chartH}
            stroke="var(--ant-color-border-secondary)"
          />

          {/* 200ms 警戒参考线 */}
          {y200 >= padTop && (
            <line
              x1={padLeft}
              y1={y200}
              x2={padLeft + chartW}
              y2={y200}
              stroke="rgba(250, 140, 22, 0.3)"
              strokeDasharray="2 2"
            />
          )}

          {/* 500ms 慢请求参考线 */}
          {y500 >= padTop && (
            <line
              x1={padLeft}
              y1={y500}
              x2={padLeft + chartW}
              y2={y500}
              stroke="rgba(255, 77, 79, 0.35)"
              strokeDasharray="3 3"
            />
          )}

          {/* 纵轴刻度值 */}
          <text x={padLeft - 6} y={padTop + 4} textAnchor="end" className={styles.axisText}>
            {maxLatency}ms
          </text>
          {maxLatency >= 400 && (
            <text
              x={padLeft - 6}
              y={padTop + chartH / 2 + 4}
              textAnchor="end"
              className={styles.axisText}
            >
              {Math.round(maxLatency / 2)}ms
            </text>
          )}
          <text x={padLeft - 6} y={padTop + chartH} textAnchor="end" className={styles.axisText}>
            0ms
          </text>

          {/* 横轴起点与终点时间 */}
          {validLogs.length > 0 && (
            <>
              <text
                x={padLeft}
                y={padTop + chartH + 16}
                textAnchor="start"
                className={styles.axisText}
              >
                {dayjs(validLogs[validLogs.length - 1].ts).format("HH:mm:ss")}
              </text>
              <text
                x={padLeft + chartW}
                y={padTop + chartH + 16}
                textAnchor="end"
                className={styles.axisText}
              >
                {dayjs(validLogs[0].ts).format("HH:mm:ss")} (最新)
              </text>
            </>
          )}

          {/* 散点 */}
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
                  r="6.5"
                  fill="none"
                  stroke={dot.color}
                  strokeWidth="1.5"
                  opacity="0.5"
                />
              )}
              <circle
                cx={dot.x}
                cy={dot.y}
                r={dot.isSlowOrErr ? 4.5 : 3.5}
                fill={dot.color}
                opacity={hoverItem?.item === dot ? 1 : 0.85}
              />
            </g>
          ))}
        </svg>

        {/* Hover Tooltip */}
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
                {hoverItem.item.latencyMs}ms
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
