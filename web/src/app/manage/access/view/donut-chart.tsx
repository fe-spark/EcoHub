"use client";

import React, { useState } from "react";
import { useContainerWidth } from "./use-container-width";
import styles from "./index.module.less";

export type DonutSlice = {
  key: string;
  label: string;
  count: number;
  pct: number;
  color: string;
  icon?: React.ReactNode;
};

interface DonutChartProps {
  data: DonutSlice[];
  title?: string;
  unit?: string;
  size?: number;
}

export default function DonutChart({
  data = [],
  title = "操作总量",
  unit = "次",
  size: propSize,
}: DonutChartProps) {
  const { ref, width: containerWidth } = useContainerWidth(400);
  const [hoverKey, setHoverKey] = useState<string | null>(null);

  // 响应式尺寸计算：根据卡片真实宽度自适应环形图大小
  const isVertical = containerWidth < 280;
  const isCompact = containerWidth < 380;
  const size = propSize || (containerWidth < 340 ? 116 : containerWidth < 420 ? 130 : 148);

  const total = data.reduce((sum, item) => sum + (item.count || 0), 0);
  const activeItem = data.find((d) => d.key === hoverKey) || null;

  const validData = data.filter((d) => d.count > 0);

  const cx = size / 2;
  const cy = size / 2;
  const rOuter = size * 0.42;
  const rInner = size * 0.27;

  let currentAngle = -Math.PI / 2;

  const slices = validData.map((item) => {
    const angleSpan = total > 0 ? (item.count / total) * Math.PI * 2 : 0;
    const startAngle = currentAngle;
    const endAngle = currentAngle + angleSpan;
    currentAngle = endAngle;

    const isHovered = hoverKey === item.key;
    const offset = isHovered ? 3 : 0;
    const midAngle = (startAngle + endAngle) / 2;
    const offX = Math.cos(midAngle) * offset;
    const offY = Math.sin(midAngle) * offset;

    const x1 = cx + offX + rOuter * Math.cos(startAngle);
    const y1 = cy + offY + rOuter * Math.sin(startAngle);
    const x2 = cx + offX + rOuter * Math.cos(endAngle);
    const y2 = cy + offY + rOuter * Math.sin(endAngle);

    const x3 = cx + offX + rInner * Math.cos(endAngle);
    const y3 = cy + offY + rInner * Math.sin(endAngle);
    const x4 = cx + offX + rInner * Math.cos(startAngle);
    const y4 = cy + offY + rInner * Math.sin(startAngle);

    const largeArc = angleSpan > Math.PI ? 1 : 0;

    const pathD =
      validData.length === 1
        ? `M ${cx} ${cy - rOuter} A ${rOuter} ${rOuter} 0 1 1 ${cx} ${cy + rOuter} A ${rOuter} ${rOuter} 0 1 1 ${cx} ${cy - rOuter} M ${cx} ${cy - rInner} A ${rInner} ${rInner} 0 1 0 ${cx} ${cy + rInner} A ${rInner} ${rInner} 0 1 0 ${cx} ${cy - rInner} Z`
        : `M ${x1} ${y1} A ${rOuter} ${rOuter} 0 ${largeArc} 1 ${x2} ${y2} L ${x3} ${y3} A ${rInner} ${rInner} 0 ${largeArc} 0 ${x4} ${y4} Z`;

    return {
      ...item,
      pathD,
      isHovered,
    };
  });

  return (
    <div
      ref={ref}
      className={`${styles.donutContainer} ${isVertical ? styles.donutContainerVertical : ""}`}
    >
      <div className={styles.donutSvgWrap} style={{ width: size, height: size }}>
        <svg
          viewBox={`0 0 ${size} ${size}`}
          width={size}
          height={size}
          className={styles.donutSvg}
        >
          {total === 0 ? (
            <circle
              cx={cx}
              cy={cy}
              r={(rOuter + rInner) / 2}
              fill="none"
              stroke="var(--ant-color-fill-secondary)"
              strokeWidth={rOuter - rInner}
            />
          ) : (
            slices.map((slice) => (
              <path
                key={slice.key}
                d={slice.pathD}
                fill={slice.color}
                className={styles.donutSlice}
                style={{
                  opacity: hoverKey === null || slice.isHovered ? 1 : 0.6,
                  transform: slice.isHovered ? "scale(1.02)" : "scale(1)",
                  transformOrigin: `${cx}px ${cy}px`,
                  transition: "all 0.2s ease",
                }}
                onMouseEnter={() => setHoverKey(slice.key)}
                onMouseLeave={() => setHoverKey(null)}
              />
            ))
          )}
        </svg>

        {/* 中心文字指示器 */}
        <div className={styles.donutCenter}>
          {activeItem ? (
            <>
              <div className={styles.centerLabel}>{activeItem.label}</div>
              <div className={styles.centerVal} style={{ color: activeItem.color }}>
                {activeItem.pct}%
              </div>
              <div className={styles.centerSub}>
                {activeItem.count.toLocaleString()} {unit}
              </div>
            </>
          ) : (
            <>
              <div className={styles.centerLabel}>{title}</div>
              <div className={styles.centerVal}>
                {total.toLocaleString()}
                <span className={styles.centerUnit}> {unit}</span>
              </div>
            </>
          )}
        </div>
      </div>

      {/* 饼图右侧图例与数值 */}
      <div className={styles.donutLegend}>
        {data.map((item) => (
          <div
            key={item.key}
            className={`${styles.donutLegendRow} ${hoverKey === item.key ? styles.donutRowActive : ""}`}
            onMouseEnter={() => setHoverKey(item.key)}
            onMouseLeave={() => setHoverKey(null)}
          >
            <div className={styles.legendDotCol}>
              {item.icon ? (
                <span className={styles.legendIconWrap} style={{ color: item.color }}>
                  {item.icon}
                </span>
              ) : (
                <span className={styles.legendDot} style={{ background: item.color }} />
              )}
              <span className={styles.legendName} title={item.label}>
                {item.label}
              </span>
            </div>

            <div className={styles.legendBarCol}>
              <div className={styles.miniProgressWrap}>
                <div
                  className={styles.miniProgressBar}
                  style={{
                    width: `${Math.max(item.count > 0 ? 3 : 0, item.pct)}%`,
                    background: item.color,
                  }}
                />
              </div>
              <span className={styles.legendCount}>
                {item.count.toLocaleString()} {unit}
              </span>
              <span className={styles.legendPct}>{item.pct}%</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
