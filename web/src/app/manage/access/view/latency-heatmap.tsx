"use client";

import { useMemo, useState } from "react";
import dayjs from "dayjs";
import styles from "./index.module.less";

type HeatLog = {
  ts: string;
  latencyMs: number;
  path?: string;
};

const BANDS = [
  { id: "fast", max: 50, label: "≤50ms", color: "#52c41a" },
  { id: "ok", max: 200, label: "50–200", color: "#1677ff" },
  { id: "mid", max: 500, label: "200–500", color: "#fa8c16" },
  { id: "slow", max: Infinity, label: ">500ms", color: "#ff4d4f" },
] as const;

function bandIndex(ms: number) {
  if (ms <= 50) return 0;
  if (ms <= 200) return 1;
  if (ms <= 500) return 2;
  return 3;
}

function stepMs(span: number) {
  if (span <= 30 * 60 * 1000) return 60 * 1000;
  if (span <= 3 * 3600 * 1000) return 5 * 60 * 1000;
  if (span <= 12 * 3600 * 1000) return 15 * 60 * 1000;
  return 30 * 60 * 1000;
}

function cellFill(color: string, ratio: number) {
  const t = Math.sqrt(Math.max(0, Math.min(1, ratio)));
  const alpha = 0.12 + t * 0.78;
  const hex = color.slice(1);
  const r = parseInt(hex.slice(0, 2), 16);
  const g = parseInt(hex.slice(2, 4), 16);
  const b = parseInt(hex.slice(4, 6), 16);
  return `rgba(${r}, ${g}, ${b}, ${alpha})`;
}

export default function LatencyHeatmap({ logs = [] }: { logs?: HeatLog[] }) {
  const [hover, setHover] = useState<{
    col: number;
    row: number;
    count: number;
    start: number;
    end: number;
  } | null>(null);

  const grid = useMemo(() => {
    if (!logs.length) {
      return null;
    }
    const times = logs.map((l) => dayjs(l.ts).valueOf()).filter((n) => Number.isFinite(n));
    if (!times.length) {
      return null;
    }
    const minT = Math.min(...times);
    const maxT = Math.max(...times);
    const span = Math.max(60 * 1000, maxT - minT);
    const step = stepMs(span);
    const start = Math.floor(minT / step) * step;
    const end = Math.ceil(maxT / step) * step;
    const cols = Math.max(1, Math.min(48, Math.round((end - start) / step)));
    const counts = BANDS.map(() => Array.from({ length: cols }, () => 0));
    for (const log of logs) {
      const t = dayjs(log.ts).valueOf();
      if (!Number.isFinite(t)) continue;
      let c = Math.floor((t - start) / step);
      if (c < 0) c = 0;
      if (c >= cols) c = cols - 1;
      counts[bandIndex(log.latencyMs || 0)][c] += 1;
    }
    const maxCount = Math.max(1, ...counts.flat());
    return { start, step, cols, counts, maxCount };
  }, [logs]);

  if (!grid) {
    return (
      <div className={styles.sparkEmpty}>
        <span>暂无耗时分布数据</span>
      </div>
    );
  }

  return (
    <div className={styles.heatWrap}>
      <div className={styles.heatLegend}>
        {BANDS.map((b) => (
          <span key={b.id} className={styles.heatLegendItem}>
            <i style={{ background: b.color }} />
            {b.label}
          </span>
        ))}
      </div>
      <div
        className={styles.heatGrid}
        style={{ gridTemplateColumns: `52px repeat(${grid.cols}, minmax(0, 1fr))` }}
      >
        {[...BANDS].reverse().map((band, ri) => {
          const row = BANDS.length - 1 - ri;
          return (
            <div key={band.id} className={styles.heatRow}>
              <span className={styles.heatYLabel}>{band.label}</span>
              {grid.counts[row].map((count, col) => (
                <button
                  key={`${band.id}-${col}`}
                  type="button"
                  className={styles.heatCell}
                  style={{
                    background:
                      count > 0
                        ? cellFill(band.color, count / grid.maxCount)
                        : "var(--ant-color-fill-quaternary)",
                  }}
                  onMouseEnter={() =>
                    setHover({
                      col,
                      row,
                      count,
                      start: grid.start + col * grid.step,
                      end: grid.start + (col + 1) * grid.step,
                    })
                  }
                  onMouseLeave={() => setHover(null)}
                />
              ))}
            </div>
          );
        })}
        <div className={styles.heatXRow}>
          <span />
          {Array.from({ length: grid.cols }, (_, col) => {
            const show =
              col === 0 || col === grid.cols - 1 || col === Math.floor((grid.cols - 1) / 2);
            return (
              <span key={col} className={styles.heatXLabel}>
                {show ? dayjs(grid.start + col * grid.step).format("HH:mm") : ""}
              </span>
            );
          })}
        </div>
      </div>
      {hover ? (
        <div className={styles.heatHint}>
          {dayjs(hover.start).format("HH:mm")}–{dayjs(hover.end).format("HH:mm")}
          {" · "}
          {BANDS[hover.row].label}
          {" · "}
          {hover.count} 次
        </div>
      ) : (
        <div className={styles.heatHintMuted}>格子颜色越深，该时段该耗时档请求越多</div>
      )}
    </div>
  );
}
