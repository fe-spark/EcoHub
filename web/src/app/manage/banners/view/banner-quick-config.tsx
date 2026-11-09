"use client";

import React, { useMemo, useRef } from "react";
import { Button, InputNumber } from "antd";
import { FieldTimeOutlined, FireOutlined, StarOutlined } from "@ant-design/icons";
import styles from "./banner-quick.module.less";

export type QuickStrategy = "latest" | "hot" | "score";

export interface CategoryQuotaItem {
  id: number;
  name: string;
  count: number;
}

interface BannerQuickConfigProps {
  strategy: QuickStrategy;
  onStrategyChange: (value: QuickStrategy) => void;
  categories: CategoryQuotaItem[];
  onCategoriesChange: (next: CategoryQuotaItem[]) => void;
  disabled?: boolean;
}

const STRATEGY_OPTIONS: {
  value: QuickStrategy;
  title: string;
  desc: string;
  icon: React.ReactNode;
}[] = [
  { value: "latest", title: "最近更新", desc: "刚更新的影片优先", icon: <FieldTimeOutlined /> },
  { value: "hot", title: "最热排片", desc: "播放热度高的优先", icon: <FireOutlined /> },
  { value: "score", title: "评分优先", desc: "评分高的影片优先", icon: <StarOutlined /> },
];

const FILL_PRESETS = [
  { label: "每类 1 部", count: 1 },
  { label: "每类 2 部", count: 2 },
  { label: "每类 3 部", count: 3 },
  { label: "清零", count: 0 },
];

export default function BannerQuickConfig({
  strategy,
  onStrategyChange,
  categories,
  onCategoriesChange,
  disabled = false,
}: BannerQuickConfigProps) {
  const totalPlannedCount = useMemo(
    () => categories.reduce((sum, item) => sum + (item.count || 0), 0),
    [categories],
  );

  const optionRefs = useRef<(HTMLButtonElement | null)[]>([]);

  const handleStrategyKeyDown = (e: React.KeyboardEvent<HTMLDivElement>) => {
    const step =
      e.key === "ArrowRight" || e.key === "ArrowDown"
        ? 1
        : e.key === "ArrowLeft" || e.key === "ArrowUp"
          ? -1
          : 0;
    if (!step || disabled) return;
    e.preventDefault();
    const current = STRATEGY_OPTIONS.findIndex((opt) => opt.value === strategy);
    const next = (current + step + STRATEGY_OPTIONS.length) % STRATEGY_OPTIONS.length;
    onStrategyChange(STRATEGY_OPTIONS[next].value);
    optionRefs.current[next]?.focus();
  };

  const handleFillAll = (count: number) => {
    onCategoriesChange(categories.map((cat) => ({ ...cat, count })));
  };

  return (
    <div className={styles.stack}>
      {/* 1. 排序策略：三张单选卡片，方向键切换 */}
      <div className={styles.configGroup}>
        <div className={styles.configGroupHeader}>
          <span className={styles.configGroupTitle} id="banner-quick-strategy-label">
            排序策略
          </span>
        </div>
        <div
          role="radiogroup"
          aria-labelledby="banner-quick-strategy-label"
          className={styles.strategyGrid}
          onKeyDown={handleStrategyKeyDown}
        >
          {STRATEGY_OPTIONS.map((opt, idx) => {
            const checked = opt.value === strategy;
            return (
              <button
                key={opt.value}
                ref={(el) => {
                  optionRefs.current[idx] = el;
                }}
                type="button"
                role="radio"
                aria-checked={checked}
                tabIndex={checked ? 0 : -1}
                disabled={disabled}
                className={`${styles.strategyCard} ${checked ? styles.strategyCardActive : ""}`}
                onClick={() => onStrategyChange(opt.value)}
              >
                <span className={styles.strategyIcon}>{opt.icon}</span>
                <span className={styles.strategyText}>
                  <span className={styles.strategyTitle}>{opt.title}</span>
                  <span className={styles.strategyDesc}>{opt.desc}</span>
                </span>
              </button>
            );
          })}
        </div>
      </div>

      {/* 2. 分类数量配额网格 */}
      <div className={styles.configGroup}>
        <div className={styles.configGroupHeader}>
          <span className={styles.configGroupTitle}>
            分类数量配额（共 {totalPlannedCount} 部）
          </span>
          {categories.length > 0 && (
            <div className={styles.quickActions}>
              {FILL_PRESETS.map((preset) => (
                <Button
                  key={preset.count}
                  size="small"
                  danger={preset.count === 0}
                  disabled={disabled}
                  onClick={() => handleFillAll(preset.count)}
                >
                  {preset.label}
                </Button>
              ))}
            </div>
          )}
        </div>

        <div className={styles.quotaGrid}>
          {categories.map((cat) => {
            const active = (cat.count || 0) > 0;
            return (
              <div
                key={cat.id}
                className={`${styles.quotaItem} ${active ? styles.quotaItemActive : ""}`}
              >
                <span className={`${styles.quotaName} ${active ? styles.quotaNameActive : ""}`}>
                  {cat.name}
                </span>
                <InputNumber
                  min={0}
                  max={99}
                  size="small"
                  value={cat.count}
                  disabled={disabled}
                  className={styles.quotaInput}
                  onChange={(val) => {
                    const nextCount = Math.max(0, Number(val) || 0);
                    onCategoriesChange(
                      categories.map((item) =>
                        item.id === cat.id ? { ...item, count: nextCount } : item,
                      ),
                    );
                  }}
                />
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}
