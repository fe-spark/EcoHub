"use client";

import React, { useMemo } from "react";
import { Button, InputNumber, Radio, Space, Switch, Tag, Tooltip } from "antd";
import {
  AppstoreOutlined,
  ClockCircleOutlined,
  FireOutlined,
  RedoOutlined,
  StarOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
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
  tmdbEnabled: boolean;
  autoScrape: boolean;
  onAutoScrapeChange: (value: boolean) => void;
  categories: CategoryQuotaItem[];
  onCategoriesChange: (next: CategoryQuotaItem[]) => void;
  disabled?: boolean;
}

export default function BannerQuickConfig({
  strategy,
  onStrategyChange,
  tmdbEnabled,
  autoScrape,
  onAutoScrapeChange,
  categories,
  onCategoriesChange,
  disabled = false,
}: BannerQuickConfigProps) {
  const totalPlannedCount = useMemo(
    () => categories.reduce((sum, item) => sum + (item.count || 0), 0),
    [categories],
  );
  const isZero = totalPlannedCount <= 0;

  const handleFillAll = (count: number) => {
    onCategoriesChange(categories.map((cat) => ({ ...cat, count })));
  };

  return (
    <div className={styles.stack}>
      <div className={styles.sectionCard}>
        <div className={styles.sectionTitle}>
          <span>排序策略</span>
        </div>
        <Radio.Group
          value={strategy}
          onChange={(e) => onStrategyChange(e.target.value)}
          buttonStyle="solid"
          className={styles.strategyGroup}
          disabled={disabled}
        >
          <Radio.Button value="hot" className={styles.strategyBtn}>
            <FireOutlined /> 最热
          </Radio.Button>
          <Radio.Button value="score" className={styles.strategyBtn}>
            <StarOutlined /> 评分
          </Radio.Button>
          <Radio.Button value="latest" className={styles.strategyBtn}>
            <ClockCircleOutlined /> 最新
          </Radio.Button>
        </Radio.Group>
      </div>

      <div className={styles.scrapeToggle}>
        <Space size={10} align="center">
          <ThunderboltOutlined
            style={{
              color: tmdbEnabled ? "#722ed1" : "var(--ant-color-text-quaternary)",
              fontSize: 20,
            }}
          />
          <div>
            <div className={styles.scrapeToggleTitle}>TMDB 刮削</div>
            <div className={styles.scrapeToggleDesc}>
              {tmdbEnabled ? "确认替换后写入" : "未启用"}
            </div>
          </div>
        </Space>
        <Tooltip title={!tmdbEnabled ? "未启用" : undefined}>
          <Switch
            checked={tmdbEnabled && autoScrape}
            onChange={onAutoScrapeChange}
            disabled={!tmdbEnabled || disabled}
          />
        </Tooltip>
      </div>

      <div className={styles.sectionCard}>
        <div className={styles.sectionTitle}>
          <Space size={6} align="center">
            <AppstoreOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>分类数量</span>
          </Space>
          <Space size={8} align="center" wrap>
            <Tag
              color={isZero ? "warning" : "processing"}
              style={{ margin: 0, borderRadius: 4 }}
            >
              总数: {totalPlannedCount}
            </Tag>
            {!disabled && categories.length > 0 && (
              <Space size={4} align="center">
                <Button size="small" onClick={() => handleFillAll(1)}>
                  每类 1 部
                </Button>
                <Button size="small" onClick={() => handleFillAll(2)}>
                  每类 2 部
                </Button>
                <Button size="small" onClick={() => handleFillAll(3)}>
                  每类 3 部
                </Button>
                <Tooltip title="全部清零">
                  <Button
                    size="small"
                    icon={<RedoOutlined />}
                    onClick={() => handleFillAll(0)}
                  />
                </Tooltip>
              </Space>
            )}
          </Space>
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
                  size="small"
                  value={cat.count}
                  disabled={disabled}
                  style={{ width: 62 }}
                  onChange={(val) => {
                    const nextCount = Math.max(0, Number(val) || 0);
                    onCategoriesChange(
                      categories.map((item) => (item.id === cat.id ? { ...item, count: nextCount } : item)),
                    );
                  }}
                />
              </div>
            );
          })}
        </div>

        {isZero && <div className={styles.quotaHint}>未配置排片数量</div>}
      </div>
    </div>
  );
}
