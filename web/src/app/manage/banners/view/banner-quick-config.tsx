"use client";

import React, { useMemo } from "react";
import { InputNumber, Segmented } from "antd";
import styles from "./banner-quick.module.less";

export type QuickStrategy = "hot" | "score" | "latest";

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

const STRATEGY_OPTIONS = [
  { label: "最热排片", value: "hot" },
  { label: "评分优先", value: "score" },
  { label: "最新上线", value: "latest" },
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

  const handleFillAll = (count: number) => {
    onCategoriesChange(categories.map((cat) => ({ ...cat, count })));
  };

  return (
    <div className={styles.stack}>
      {/* 1. 排序策略分段选择器 */}
      <div className={styles.configGroup}>
        <div className={styles.configGroupHeader}>
          <span className={styles.configGroupTitle}>排序策略</span>
        </div>
        <Segmented
          block
          size="middle"
          value={strategy}
          options={STRATEGY_OPTIONS}
          onChange={(val) => onStrategyChange(val as QuickStrategy)}
          disabled={disabled}
        />
      </div>

      {/* 2. 分类数量配额网格 */}
      <div className={styles.configGroup}>
        <div className={styles.configGroupHeader}>
          <span className={styles.configGroupTitle}>
            分类数量配额（共 {totalPlannedCount} 部）
          </span>
          {!disabled && categories.length > 0 && (
            <div className={styles.quickActions}>
              <span className={styles.quickActionBtn} onClick={() => handleFillAll(1)}>
                每类 1 部
              </span>
              <span className={styles.quickActionBtn} onClick={() => handleFillAll(2)}>
                每类 2 部
              </span>
              <span className={styles.quickActionBtn} onClick={() => handleFillAll(3)}>
                每类 3 部
              </span>
              <span className={styles.quickActionBtn} onClick={() => handleFillAll(0)}>
                清零
              </span>
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
                  style={{ width: 58 }}
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
