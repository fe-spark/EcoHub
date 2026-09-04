"use client";

import React, { useMemo, useState } from "react";
import { Card, Space, Tag, Empty } from "antd";
import { CompassOutlined, CrownOutlined } from "@ant-design/icons";
import { useContainerSize } from "./use-container-width";
import styles from "./index.module.less";

export interface BrowserDistributionProps {
  browsers: Record<string, number>;
  loading?: boolean;
}

interface SliceItem {
  key: string;
  name: string;
  count: number;
  pct: number;
  pctFormatted: string;
  color: string;
}

// 经典参考图配色板
const PALETTE = [
  "#4C6EF5", // 经典宝蓝
  "#94D82D", // 鲜亮草绿
  "#494E6B", // 典雅深青灰
  "#FF922B", // 暖亮珊瑚橙
  "#22B8CF", // 纯净天蓝
  "#FCC419", // 亮黄
  "#FF6B8B", // 玫瑰粉
  "#845EF7", // 浅紫
];

const BROWSER_NAMES: Record<string, string> = {
  chrome: "Chrome",
  safari: "Safari",
  edge: "Edge",
  firefox: "Firefox",
  wechat: "微信",
  other: "其他",
};

export default function BrowserDistributionCard({
  browsers,
  loading = false,
}: BrowserDistributionProps) {
  const { ref, width: containerWidth, height: containerHeight } = useContainerSize(400, 200);
  const [hoveredKey, setHoveredKey] = useState<string | null>(null);

  // 格式化实际有访问量的数据项
  const { list, total, dominant, dominantPct } = useMemo(() => {
    const rawEntries = Object.entries(browsers || {}).filter(([_, count]) => count > 0);
    const sum = rawEntries.reduce((acc, [_, count]) => acc + count, 0);

    const formatted: SliceItem[] = rawEntries
      .map(([key, count], idx) => {
        const lower = key.toLowerCase();
        const displayName =
          BROWSER_NAMES[lower] || (key.charAt(0).toUpperCase() + key.slice(1));
        const pct = sum > 0 ? (count / sum) * 100 : 0;
        return {
          key: lower,
          name: displayName,
          count,
          pct,
          pctFormatted: pct.toFixed(1),
          color: PALETTE[idx % PALETTE.length],
        };
      })
      .sort((a, b) => b.count - a.count);

    const dom = formatted[0] || null;
    const domPct = dom && sum > 0 ? ((dom.count / sum) * 100).toFixed(1) : "0";

    return {
      list: formatted,
      total: sum,
      dominant: dom,
      dominantPct: domPct,
    };
  }, [browsers]);

  // 以当前容器真实可用宽高严格约束饼图大小，杜绝高度溢出
  const cx = containerWidth / 2;
  const cy = containerHeight / 2;

  const textReserveX = Math.max(65, Math.min(100, Math.floor(containerWidth * 0.2)));
  const textReserveY = Math.max(16, Math.min(26, Math.floor(containerHeight * 0.1)));
  const availW = Math.max(50, containerWidth - 2 * (textReserveX + 10));
  const availH = Math.max(50, containerHeight - 2 * (textReserveY + 10));
  const pieRadius = Math.max(25, Math.floor(Math.min(availW, availH) / 2));

  // 纯函数计算每个扇区和防重叠折线
  const slices = useMemo(() => {
    let currentAngle = -Math.PI / 2;
    const rawSlices = [];

    for (let i = 0; i < list.length; i++) {
      const item = list[i];
      const angleSpan = total > 0 ? (item.count / total) * Math.PI * 2 : 0;
      const startAngle = currentAngle;
      const endAngle = currentAngle + angleSpan;
      currentAngle = endAngle;

      const midAngle = (startAngle + endAngle) / 2;
      const isHovered = hoveredKey === item.key;

      // 悬停时微微向外膨胀 4px
      const shift = isHovered ? 4 : 0;
      const offX = Math.cos(midAngle) * shift;
      const offY = Math.sin(midAngle) * shift;

      // 扇区几何路径
      const x1 = cx + offX + pieRadius * Math.cos(startAngle);
      const y1 = cy + offY + pieRadius * Math.sin(startAngle);
      const x2 = cx + offX + pieRadius * Math.cos(endAngle);
      const y2 = cy + offY + pieRadius * Math.sin(endAngle);

      const largeArc = angleSpan > Math.PI ? 1 : 0;

      const pathD =
        list.length === 1
          ? ""
          : `M ${cx + offX} ${cy + offY} L ${x1} ${y1} A ${pieRadius} ${pieRadius} 0 ${largeArc} 1 ${x2} ${y2} Z`;

      const labelAngle = list.length === 1 ? -Math.PI / 6 : midAngle;
      const cosA = Math.cos(labelAngle);
      const sinA = Math.sin(labelAngle);
      const isRight = cosA >= 0;

      const p0 = {
        x: cx + offX + pieRadius * cosA,
        y: cy + offY + pieRadius * sinA,
      };

      const elbowDist = pieRadius + 10;
      const p1 = {
        x: cx + offX + elbowDist * cosA,
        y: cy + offY + elbowDist * sinA,
      };

      rawSlices.push({
        ...item,
        pathD,
        isHovered,
        isRight,
        p0,
        p1,
        idealY: p1.y,
        targetY: p1.y,
      });
    }

    const leftGroup = rawSlices.filter((s) => !s.isRight);
    const rightGroup = rawSlices.filter((s) => s.isRight);

    leftGroup.sort((a, b) => a.p0.y - b.p0.y);
    rightGroup.sort((a, b) => a.p0.y - b.p0.y);

    const adjustGroupY = (group: typeof rawSlices) => {
      if (group.length === 0) return;
      const n = group.length;
      const minY = 16;
      const maxY = Math.max(minY + 20, containerHeight - 16);
      const availH = maxY - minY;
      const idealGap = 22;
      const minGap = 16;
      const gap = n > 1 ? Math.min(idealGap, Math.max(minGap, availH / n)) : idealGap;

      const targets = group.map((s) => s.idealY);

      for (let i = 0; i < n; i++) {
        if (i === 0) {
          targets[i] = Math.max(minY, targets[i]);
        } else if (targets[i] < targets[i - 1] + gap) {
          targets[i] = targets[i - 1] + gap;
        }
      }

      for (let i = n - 1; i >= 0; i--) {
        if (i === n - 1) {
          targets[i] = Math.min(maxY, targets[i]);
        } else if (targets[i] > targets[i + 1] - gap) {
          targets[i] = targets[i + 1] - gap;
        }
      }

      if (targets[0] < minY) {
        const shift = minY - targets[0];
        for (let i = 0; i < n; i++) {
          targets[i] += shift;
        }
      }

      for (let i = 0; i < n; i++) {
        group[i].targetY = targets[i];
      }
    };

    adjustGroupY(leftGroup);
    adjustGroupY(rightGroup);

    const bendLen = 14;
    const textGap = 5;

    return rawSlices.map((s) => {
      const isRight = s.isRight;
      const targetY = s.targetY;
      const displayName = s.name.length > 7 ? s.name.slice(0, 6) + "…" : s.name;
      const minMargin = 8;
      const elbowX = isRight
        ? Math.max(s.p1.x, cx + pieRadius + minMargin)
        : Math.min(s.p1.x, cx - pieRadius - minMargin);

      // 估算文本像素宽度
      let textW = 0;
      for (let j = 0; j < displayName.length; j++) {
        textW += displayName.charCodeAt(j) > 255 ? 12 : 7;
      }
      textW += 5 + s.pctFormatted.length * 7 + 7;

      let p3X = isRight ? elbowX + bendLen : elbowX - bendLen;
      let textX = isRight ? p3X + textGap : p3X - textGap;

      // 边界绝对安全夹紧约束（Boundary Clamping）
      const paddingH = 8;
      if (isRight) {
        const maxTextX = containerWidth - paddingH - textW;
        if (textX > maxTextX) {
          textX = Math.max(cx + pieRadius + 10, maxTextX);
          p3X = textX - textGap;
        }
      } else {
        const minTextX = paddingH + textW;
        if (textX < minTextX) {
          textX = Math.min(cx - pieRadius - 10, minTextX);
          p3X = textX + textGap;
        }
      }

      const p2X = isRight ? Math.min(elbowX, p3X - 2) : Math.max(elbowX, p3X + 2);
      const textAnchor: "start" | "end" = isRight ? "start" : "end";
      const textY = targetY + 4;

      return {
        ...s,
        displayName,
        linePoints: `${s.p0.x.toFixed(1)},${s.p0.y.toFixed(1)} ${s.p1.x.toFixed(1)},${s.p1.y.toFixed(1)} ${p2X.toFixed(1)},${targetY.toFixed(1)} ${p3X.toFixed(1)},${targetY.toFixed(1)}`,
        textAnchor,
        textX,
        textY,
      };
    });
  }, [list, total, hoveredKey, cx, cy, pieRadius, containerWidth, containerHeight]);

  return (
    <Card
      title={
        <Space size={8}>
          <CompassOutlined style={{ color: "#4C6EF5", fontSize: 16 }} />
          <span style={{ fontWeight: 600 }}>浏览器分布</span>
        </Space>
      }
      extra={
        dominant ? (
          <Tag
            bordered={false}
            color="processing"
            style={{
              borderRadius: 12,
              padding: "2px 10px",
              fontWeight: 500,
              fontSize: 12,
            }}
          >
            <CrownOutlined style={{ marginRight: 4, color: "#faad14" }} />
            {dominant.name} 主导 · {dominantPct}%
          </Tag>
        ) : null
      }
      className={styles.halfCard}
      classNames={{ body: styles.centeredCardBody }}
      loading={loading}
    >
      <div ref={ref} className={styles.pieCardContainer}>
        {total === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="暂无浏览器访问数据"
          />
        ) : (
          <svg
            viewBox={`0 0 ${containerWidth} ${containerHeight}`}
            width="100%"
            height="100%"
            className={styles.pieSvg}
          >
            {/* 实心饼图扇区 */}
            <g className={styles.pieSlicesGroup}>
              {list.length === 1 ? (
                <circle
                  cx={cx}
                  cy={cy}
                  r={pieRadius}
                  fill={list[0].color}
                  stroke="var(--ant-color-bg-container, #ffffff)"
                  strokeWidth="1.5"
                />
              ) : (
                slices.map((slice) => (
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
                      transition: "all 0.2s cubic-bezier(0.4, 0, 0.2, 1)",
                    }}
                    onMouseEnter={() => setHoveredKey(slice.key)}
                    onMouseLeave={() => setHoveredKey(null)}
                  />
                ))
              )}
            </g>

            {/* 外部折线与文字标签（绝对保证在 Card 内） */}
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
                  <title>{`${slice.name}: ${slice.pctFormatted}% (${slice.count.toLocaleString()} 次)`}</title>
                  {/* 折线 */}
                  <polyline
                    points={slice.linePoints}
                    fill="none"
                    stroke={slice.color}
                    strokeWidth="1.2"
                    strokeLinejoin="round"
                    strokeLinecap="round"
                  />

                  {/* 外部标签：名称与百分比 */}
                  <text
                    x={slice.textX}
                    y={slice.textY}
                    textAnchor={slice.textAnchor}
                    className={styles.sliceLabelText}
                  >
                    <tspan className={styles.sliceLabelName}>{slice.displayName}</tspan>
                    <tspan dx="5" fill={slice.color} fontWeight="700">
                      {slice.pctFormatted}%
                    </tspan>
                  </text>
                </g>
              ))}
            </g>
          </svg>
        )}
      </div>
    </Card>
  );
}
