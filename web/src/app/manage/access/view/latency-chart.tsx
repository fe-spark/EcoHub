"use client";

import React, { useState } from "react";
import styles from "./index.module.less";

interface LatencyChartProps {
  hist?: Record<string, number>;
}

const BUCKETS = [
  { key: "<=50", label: "≤50ms", name: "极速", color: "#52c41a", bg: "rgba(82, 196, 26, 0.12)" },
  { key: "51-200", label: "51-200ms", name: "正常", color: "#1677ff", bg: "rgba(22, 119, 255, 0.12)" },
  { key: "201-500", label: "201-500ms", name: "一般", color: "#fa8c16", bg: "rgba(250, 140, 22, 0.12)" },
  { key: "501-1500", label: "501-1500ms", name: "偏慢", color: "#ff7875", bg: "rgba(255, 120, 117, 0.15)" },
  { key: ">1500", label: ">1500ms", name: "较慢", color: "#ff4d4f", bg: "rgba(255, 77, 79, 0.2)" },
];

export default function LatencyChart({ hist = {} }: LatencyChartProps) {
  const [hoverKey, setHoverKey] = useState<string | null>(null);

  const total = BUCKETS.reduce((sum, b) => sum + (hist[b.key] || 0), 0);
  const maxCount = Math.max(1, ...BUCKETS.map((b) => hist[b.key] || 0));

  return (
    <div className={styles.latencyChartContainer}>
      <div className={styles.latencyBarsWrap}>
        {BUCKETS.map((b) => {
          const count = hist[b.key] || 0;
          const pct = total > 0 ? Math.round((count / total) * 100) : 0;
          const barHeightPct = total > 0 && count > 0 ? Math.max(8, Math.round((count / maxCount) * 100)) : 4;
          const isHovered = hoverKey === b.key;

          return (
            <div
              key={b.key}
              className={`${styles.latencyCol} ${isHovered ? styles.latencyColActive : ""}`}
              onMouseEnter={() => setHoverKey(b.key)}
              onMouseLeave={() => setHoverKey(null)}
            >
              <div className={styles.barTopVal}>
                <span className={styles.barPct}>{pct}%</span>
                <span className={styles.barCount}>{count.toLocaleString()}</span>
              </div>
              <div className={styles.barTrack}>
                <div
                  className={styles.barFill}
                  style={{
                    height: `${barHeightPct}%`,
                    background: b.color,
                    boxShadow: isHovered ? `0 0 8px ${b.color}` : "none",
                  }}
                />
              </div>
              <div className={styles.barLabelGroup}>
                <span className={styles.bucketLabel}>{b.label}</span>
                <span
                  className={styles.bucketBadge}
                  style={{ color: b.color, background: b.bg }}
                >
                  {b.name}
                </span>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
