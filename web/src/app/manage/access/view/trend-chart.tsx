"use client";

import React, { useState, useMemo } from "react";
import dayjs from "dayjs";
import { useContainerWidth } from "./use-container-width";
import styles from "./index.module.less";

export type SeriesPoint = {
  t: string;
  pv: number;
  err4: number;
  err5: number;
  providePv: number;
};

interface TrendChartProps {
  series?: SeriesPoint[];
}

function getSplinePath(pts: { x: number; y: number }[]): string {
  if (pts.length === 0) return "";
  if (pts.length === 1) return `M ${pts[0].x},${pts[0].y}`;
  let d = `M ${pts[0].x},${pts[0].y}`;
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i === 0 ? 0 : i - 1];
    const p1 = pts[i];
    const p2 = pts[i + 1];
    const p3 = pts[i + 2 < pts.length ? i + 2 : i + 1];
    const cp1x = p1.x + (p2.x - p0.x) / 6;
    const cp1y = p1.y + (p2.y - p0.y) / 6;
    const cp2x = p2.x - (p3.x - p1.x) / 6;
    const cp2y = p2.y - (p3.y - p1.y) / 6;
    d += ` C ${cp1x},${cp1y} ${cp2x},${cp2y} ${p2.x},${p2.y}`;
  }
  return d;
}

export default function TrendChart({ series = [] }: TrendChartProps) {
  const { ref, width } = useContainerWidth(600);
  const [hoverIndex, setHoverIndex] = useState<number | null>(null);

  const totalSeries = useMemo(() => {
    return (series || []).map((p) => ({
      t: p.t,
      count: (p.pv || 0) + (p.providePv || 0),
    }));
  }, [series]);

  const maxVal = useMemo(() => {
    if (totalSeries.length === 0) return 10;
    const max = Math.max(...totalSeries.map((p) => p.count));
    return Math.max(10, Math.ceil(max * 1.15));
  }, [totalSeries]);

  const height = 170;
  const padLeft = 40;
  const padRight = 20;
  const padTop = 15;
  const padBottom = 24;
  const chartW = Math.max(50, width - padLeft - padRight);
  const chartH = height - padTop - padBottom;

  const getX = React.useCallback(
    (index: number) => {
      if (totalSeries.length <= 1) return padLeft + chartW / 2;
      return padLeft + (index / (totalSeries.length - 1)) * chartW;
    },
    [totalSeries.length, chartW, padLeft],
  );

  const getY = React.useCallback(
    (val: number) => {
      const clamped = Math.max(0, val);
      return padTop + chartH - (clamped / maxVal) * chartH;
    },
    [maxVal, padTop, chartH],
  );

  const points = useMemo(
    () => totalSeries.map((p, i) => ({ x: getX(i), y: getY(p.count) })),
    [totalSeries, getX, getY],
  );

  const splinePath = useMemo(() => getSplinePath(points), [points]);

  const areaPath = useMemo(() => {
    if (points.length === 0) return "";
    const startX = points[0].x;
    const endX = points[points.length - 1].x;
    const bottomY = padTop + chartH;
    return `${splinePath} L ${endX},${bottomY} L ${startX},${bottomY} Z`;
  }, [splinePath, points, padTop, chartH]);

  const timeLabels = useMemo(() => {
    if (totalSeries.length < 2) return [];
    const step = Math.max(1, Math.floor(totalSeries.length / 6));
    const labels = [];
    for (let i = 0; i < totalSeries.length; i += step) {
      const timeStr = totalSeries[i].t ? dayjs(totalSeries[i].t).format("HH:mm") : `${i}:00`;
      labels.push({ x: getX(i), label: timeStr });
    }
    return labels;
  }, [totalSeries, getX]);

  const activePoint = hoverIndex !== null && totalSeries[hoverIndex] ? totalSeries[hoverIndex] : null;

  if (totalSeries.length === 0) {
    return <div className={styles.sparkEmpty}>暂无走势数据</div>;
  }

  const formatTimeRange = (tStr: string) => {
    if (!tStr) return "";
    const start = dayjs(tStr);
    const end = start.add(15, "minute");
    return `${start.format("HH:mm")} ~ ${end.format("HH:mm")}`;
  };

  return (
    <div className={styles.trendChartWrap} ref={ref}>
      <div className={styles.svgContainer}>
        <svg
          className={styles.trendSvg}
          viewBox={`0 0 ${width} ${height}`}
          width={width}
          height={height}
          onMouseLeave={() => setHoverIndex(null)}
          onMouseMove={(e) => {
            const rect = e.currentTarget.getBoundingClientRect();
            const mouseX = e.clientX - rect.left;
            if (mouseX < padLeft || mouseX > padLeft + chartW) {
              setHoverIndex(null);
              return;
            }
            const ratio = (mouseX - padLeft) / chartW;
            const idx = Math.min(
              totalSeries.length - 1,
              Math.max(0, Math.round(ratio * (totalSeries.length - 1))),
            );
            setHoverIndex(idx);
          }}
        >
          <defs>
            <linearGradient id="areaGradientTotal" x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor="#1677ff" stopOpacity="0.25" />
              <stop offset="100%" stopColor="#1677ff" stopOpacity="0.0" />
            </linearGradient>
          </defs>

          {/* 网格参考线 */}
          <line
            x1={padLeft}
            y1={padTop + chartH / 2}
            x2={padLeft + chartW}
            y2={padTop + chartH / 2}
            stroke="var(--ant-color-border-secondary)"
            strokeDasharray="3 3"
          />
          <line
            x1={padLeft}
            y1={padTop + chartH}
            x2={padLeft + chartW}
            y2={padTop + chartH}
            stroke="var(--ant-color-border-secondary)"
          />

          {/* 纵轴刻度值 */}
          <text x={padLeft - 6} y={padTop + 4} textAnchor="end" className={styles.axisText}>
            {Math.round(maxVal)}
          </text>
          <text x={padLeft - 6} y={padTop + chartH / 2 + 4} textAnchor="end" className={styles.axisText}>
            {Math.round(maxVal / 2)}
          </text>
          <text x={padLeft - 6} y={padTop + chartH} textAnchor="end" className={styles.axisText}>
            0
          </text>

          {/* 渐变面积 */}
          <path d={areaPath} fill="url(#areaGradientTotal)" />

          {/* 平滑曲线折线 */}
          <path
            d={splinePath}
            fill="none"
            stroke="#1677ff"
            strokeWidth="2"
            strokeLinecap="round"
          />

          {/* 横轴时间标签 */}
          {timeLabels.map((lbl, idx) => (
            <text
              key={`${lbl.label}-${idx}`}
              x={lbl.x}
              y={padTop + chartH + 16}
              textAnchor="middle"
              className={styles.axisText}
            >
              {lbl.label}
            </text>
          ))}

          {/* Hover 垂直指示线 */}
          {hoverIndex !== null && (
            <line
              x1={getX(hoverIndex)}
              y1={padTop}
              x2={getX(hoverIndex)}
              y2={padTop + chartH}
              stroke="#1677ff"
              strokeWidth="1.5"
              strokeDasharray="2 2"
            />
          )}
        </svg>

        {/* Hover Tooltip */}
        {activePoint && hoverIndex !== null && (
          <div
            className={styles.tooltipBox}
            style={{
              left: `${getX(hoverIndex)}px`,
              transform: getX(hoverIndex) > width * 0.7 ? "translateX(-100%)" : "translateX(10px)",
            }}
          >
            <div className={styles.tooltipTime}>
              {formatTimeRange(activePoint.t)}
            </div>
            <div className={styles.tooltipRow}>
              <span>请求数:</span>
              <b>{activePoint.count} 次</b>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
