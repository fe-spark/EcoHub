"use client";

import React, { useMemo, useState } from "react";
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
};

export interface DonutChartProps {
  data?: DonutSlice[];
  slices?: DonutSlice[];
  title?: string;
  centerLabel?: string;
  unit?: string;
  size?: number;
}

// 统一高质感参考图经典调色板
const DEFAULT_PALETTE = [
  "#4C6EF5", // 宝蓝
  "#94D82D", // 草绿
  "#494E6B", // 深青灰
  "#FF922B", // 珊瑚橙
  "#22B8CF", // 纯净青蓝
  "#FCC419", // 金黄
  "#FF6B8B", // 玫瑰粉
  "#845EF7", // 紫色
];

export default function DonutChart({
  data,
  slices: propSlices,
  title = "总计",
  centerLabel,
  unit = "次",
}: DonutChartProps) {
  const { ref, width: containerWidth, height: containerHeight } = useContainerSize(400, 200);
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);

  // 标准化数据项并过滤无效值
  const { list, total } = useMemo(() => {
    const rawList = propSlices || data || [];
    const validItems = rawList.filter((item) => {
      const val = item.count ?? item.value ?? 0;
      return val > 0;
    });

    const sum = validItems.reduce((acc, item) => acc + (item.count ?? item.value ?? 0), 0);

    const formatted = validItems.map((item, idx) => {
      const count = item.count ?? item.value ?? 0;
      const pct = sum > 0 ? (count / sum) * 100 : 0;
      const name = item.label || item.name || `项 ${idx + 1}`;
      const key = item.key || name || String(idx);
      const color = item.color || DEFAULT_PALETTE[idx % DEFAULT_PALETTE.length];

      return {
        key,
        name,
        count,
        pct,
        pctFormatted: pct.toFixed(1),
        color,
      };
    });

    return {
      list: formatted,
      total: sum,
    };
  }, [propSlices, data]);

  // 严格依据容器最短边主导计算环形图外径与内径，保证圆环饱满大气
  const cx = containerWidth / 2;
  const cy = containerHeight / 2;

  const shortestSide = Math.min(containerWidth, containerHeight);
  // rOuter 占据最短边约 33%，让圆环视觉占比充足饱满
  const rOuter = Math.max(35, Math.floor(shortestSide * 0.33));
  const rInner = Math.max(20, Math.floor(rOuter * 0.60));

  // 适度拉开折线与文字间距，兼顾舒展与不超界
  const lineLength1 = Math.max(18, Math.min(26, Math.floor(shortestSide * 0.07)));
  const bendLen = Math.max(16, Math.min(24, Math.floor(shortestSide * 0.065)));
  const textGap = 6;

  // 计算环形扇区切片与引出折线坐标
  const slices = useMemo(() => {
    let currentAngle = -Math.PI / 2;
    const result = [];

    for (let i = 0; i < list.length; i++) {
      const item = list[i];
      const angleSpan = total > 0 ? (item.count / total) * Math.PI * 2 : 0;
      const startAngle = currentAngle;
      const endAngle = currentAngle + angleSpan;
      currentAngle = endAngle;

      const midAngle = (startAngle + endAngle) / 2;
      const isHovered = hoveredKey === item.key;

      const shift = isHovered ? 4 : 0;
      const offX = Math.cos(midAngle) * shift;
      const offY = Math.sin(midAngle) * shift;

      // 环形切片几何路径
      const x1 = cx + offX + rOuter * Math.cos(startAngle);
      const y1 = cy + offY + rOuter * Math.sin(startAngle);
      const x2 = cx + offX + rOuter * Math.cos(endAngle);
      const y2 = cy + offY + rOuter * Math.sin(endAngle);

      const x3 = cx + offX + rInner * Math.cos(endAngle);
      const y3 = cy + offY + rInner * Math.sin(endAngle);
      const x4 = cx + offX + rInner * Math.cos(startAngle);
      const y4 = cy + offY + rInner * Math.sin(startAngle);

      const largeArc = angleSpan > Math.PI ? 1 : 0;

      // 单一切片（100%）时绘制完整封闭双圆环
      const pathD =
        list.length === 1
          ? `M ${cx} ${cy - rOuter} A ${rOuter} ${rOuter} 0 1 1 ${cx} ${cy + rOuter} A ${rOuter} ${rOuter} 0 1 1 ${cx} ${cy - rOuter} M ${cx} ${cy - rInner} A ${rInner} ${rInner} 0 1 0 ${cx} ${cy + rInner} A ${rInner} ${rInner} 0 1 0 ${cx} ${cy - rInner} Z`
          : `M ${x1} ${y1} A ${rOuter} ${rOuter} 0 ${largeArc} 1 ${x2} ${y2} L ${x3} ${y3} A ${rInner} ${rInner} 0 ${largeArc} 0 ${x4} ${y4} Z`;

      // 单项时引出角度使用优雅的右上 -30°，多项时使用扇区正中 midAngle
      const labelAngle = list.length === 1 ? -Math.PI / 6 : midAngle;

      // 外部折线引出线（Leader line）
      const p0 = {
        x: cx + offX + rOuter * Math.cos(labelAngle),
        y: cy + offY + rOuter * Math.sin(labelAngle),
      };

      const elbowDist = rOuter + lineLength1;
      const p1 = {
        x: cx + offX + elbowDist * Math.cos(labelAngle),
        y: cy + offY + elbowDist * Math.sin(labelAngle),
      };

      const isRight = Math.cos(labelAngle) >= 0;
      const p2 = {
        x: isRight ? p1.x + bendLen : p1.x - bendLen,
        y: p1.y,
      };

      const textAnchor: "start" | "end" = isRight ? "start" : "end";
      const textX = isRight ? p2.x + textGap : p2.x - textGap;
      const textY = p2.y + 4;

      result.push({
        ...item,
        pathD,
        isHovered,
        linePoints: `${p0.x},${p0.y} ${p1.x},${p1.y} ${p2.x},${p2.y}`,
        textAnchor,
        textX,
        textY,
      });
    }

    return result;
  }, [list, total, hoveredKey, cx, cy, rOuter, rInner, lineLength1, bendLen, textGap]);

  const activeItem = list.find((d) => d.key === hoveredKey) || null;

  if (total === 0 || list.length === 0) {
    return (
      <div className={styles.pieCardContainer}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无分布数据" />
      </div>
    );
  }

  return (
    <div ref={ref} className={styles.pieCardContainer}>
      <svg
        viewBox={`0 0 ${containerWidth} ${containerHeight}`}
        width="100%"
        height="100%"
        className={styles.pieSvg}
      >
        {/* 环形扇区切片 */}
        <g className={styles.pieSlicesGroup}>
          {slices.map((slice) => (
            <path
              key={slice.key}
              d={slice.pathD}
              fill={slice.color}
              stroke="var(--ant-color-bg-container, #ffffff)"
              strokeWidth="1.5"
              className={styles.pieSlicePath}
              style={{
                opacity: hoveredKey === null || slice.isHovered ? 1 : 0.65,
                filter: slice.isHovered
                  ? "drop-shadow(0 3px 8px rgba(0,0,0,0.22))"
                  : "none",
                cursor: "pointer",
                transition: "opacity 0.2s ease, filter 0.2s ease",
              }}
              onMouseEnter={() => setHoveredKey(slice.key)}
              onMouseLeave={() => setHoveredKey(null)}
            />
          ))}
        </g>

        {/* 外部折线引出线与标签（严格位于安全边距内） */}
        <g className={styles.pieLabelsGroup}>
          {slices.map((slice) => (
            <g
              key={`label-${slice.key}`}
              style={{
                opacity: hoveredKey === null || slice.isHovered ? 1 : 0.45,
                cursor: "pointer",
                transition: "opacity 0.2s ease",
              }}
              onMouseEnter={() => setHoveredKey(slice.key)}
              onMouseLeave={() => setHoveredKey(null)}
            >
              <polyline
                points={slice.linePoints}
                fill="none"
                stroke={slice.color}
                strokeWidth="1.2"
              />
              <text
                x={slice.textX}
                y={slice.textY}
                textAnchor={slice.textAnchor}
                className={styles.sliceLabelText}
              >
                <tspan className={styles.sliceLabelName}>{slice.name}</tspan>
                <tspan dx="5" fill={slice.color} fontWeight="700">
                  {slice.pctFormatted}%
                </tspan>
              </text>
            </g>
          ))}
        </g>
      </svg>

      {/* 环心指标信息：解决单项时实心圆单调问题 */}
      <div
        className={styles.donutCenterWrap}
        style={{
          width: rInner * 2 - 4,
          height: rInner * 2 - 4,
          left: cx,
          top: cy,
        }}
      >
        {activeItem ? (
          <>
            <div className={styles.centerSubLabel} title={activeItem.name}>
              {activeItem.name}
            </div>
            <div
              className={styles.centerValue}
              style={{
                color: activeItem.color,
                fontSize: Math.max(14, Math.round(rInner * 0.42)),
              }}
            >
              {activeItem.pctFormatted}%
            </div>
            <div className={styles.centerUnit}>
              {activeItem.count.toLocaleString()} {unit}
            </div>
          </>
        ) : (
          <>
            <div className={styles.centerSubLabel}>{centerLabel || title}</div>
            <div
              className={styles.centerValue}
              style={{ fontSize: Math.max(14, Math.round(rInner * 0.42)) }}
            >
              {list.length === 1 ? list[0].pctFormatted + "%" : total.toLocaleString()}
            </div>
            <div className={styles.centerUnit}>
              {list.length === 1 ? `${total.toLocaleString()} ${unit}` : unit}
            </div>
          </>
        )}
      </div>
    </div>
  );
}
