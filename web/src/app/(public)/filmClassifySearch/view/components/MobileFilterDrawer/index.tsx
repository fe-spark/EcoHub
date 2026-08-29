"use client";

import React, { useState } from "react";
import { Drawer } from "antd";
import {
  CloseOutlined,
  ControlOutlined,
  DeleteOutlined,
  RedoOutlined,
  CheckOutlined,
} from "@ant-design/icons";
import { ActiveChipItem } from "../DesktopFilterPanel";
import {
  hasVisibleCategoryTags,
  isDefaultSortValue,
  resolveActiveTagValue,
} from "../../filter-params";
import styles from "./index.module.less";

interface TagItem {
  Name: string;
  Value: string;
}

interface MobileFilterDrawerProps {
  sortList: string[];
  titles: Record<string, string>;
  tagsMap: Record<string, TagItem[]>;
  activeParams: Record<string, string>;
  activeChips: ActiveChipItem[];
  total?: number;
  isPending: boolean;
  onApplyFilters: (nextParams: Record<string, string>) => void;
  onQuickSelect?: (key: string, value: string) => void;
  onRemoveChip: (key: string) => void;
  onReset: () => void;
  normalizeTagValue: (v: unknown) => string;
}

export default function MobileFilterDrawer({
  sortList,
  titles,
  tagsMap,
  activeParams,
  activeChips,
  total,
  isPending,
  onApplyFilters,
  onQuickSelect,
  onRemoveChip,
  onReset,
  normalizeTagValue,
}: MobileFilterDrawerProps) {
  const [drawerOpen, setDrawerOpen] = useState(false);
  const [draftParams, setDraftParams] = useState<Record<string, string>>(activeParams);

  const handleOpenDrawer = () => {
    setDraftParams(activeParams);
    setDrawerOpen(true);
  };

  const activeCount = activeChips.length;

  // 有子分类时主栏快滑分类；无 Category 时回退到排序
  const hasCategory = hasVisibleCategoryTags(tagsMap["Category"]);
  const primaryKey = hasCategory ? "Category" : "Sort";
  const primaryTags = tagsMap[primaryKey] || [];
  const currentPrimaryVal = resolveActiveTagValue(
    primaryKey,
    normalizeTagValue(activeParams[primaryKey]),
  );

  const handleDraftTagClick = (key: string, val: string) => {
    const nextVal = normalizeTagValue(val);
    setDraftParams((prev) => {
      const next = { ...prev };
      if (nextVal === "" || isDefaultSortValue(key, nextVal)) {
        delete next[key];
      } else {
        next[key] = nextVal;
      }
      return next;
    });
  };

  const handleResetDraft = () => {
    const next: Record<string, string> = {};
    if (activeParams.Pid) {
      next.Pid = activeParams.Pid;
    }
    setDraftParams(next);
  };

  const handleApplyDraft = () => {
    setDrawerOpen(false);
    onApplyFilters(draftParams);
  };

  return (
    <div className={styles.mobileContainer}>
      {/* 第 1 行：主操作栏（分类快滑，无子分类时回退到排序） */}
      <div className={styles.mainBar}>
        <div className={styles.quickScroll}>
          {primaryTags.map((tag, index) => {
            const val = normalizeTagValue(tag.Value);
            const isActive = currentPrimaryVal === val;

            return (
              <button
                key={`quick-${primaryKey}-${tag.Value}-${index}`}
                type="button"
                className={`${styles.quickTag} ${isActive ? styles.active : ""}`}
                disabled={isPending}
                onClick={() => onQuickSelect && onQuickSelect(primaryKey, tag.Value)}
                aria-pressed={isActive}
              >
                {tag.Name}
              </button>
            );
          })}
        </div>

        {/* 唤起抽屉按钮 */}
        <button
          type="button"
          className={`${styles.drawerTriggerBtn} ${
            activeCount > 0 ? styles.hasActive : ""
          }`}
          onClick={handleOpenDrawer}
          aria-label={`打开筛选面板，当前已选 ${activeCount} 项`}
        >
          <ControlOutlined />
          <span>筛选</span>
          {activeCount > 0 && <span className={styles.badge}>{activeCount}</span>}
        </button>
      </div>

      {/* 第 2 行：已选条件状态栏（仅在有已选条件时展开，整行展示，右侧固定清空按钮） */}
      {activeCount > 0 && (
        <div className={styles.activeBar}>
          <div className={styles.chipsScroll}>
            {activeChips.map((chip) => (
              <button
                key={`chip-mob-${chip.key}`}
                type="button"
                className={styles.activeChip}
                onClick={() => onRemoveChip(chip.key)}
                disabled={isPending}
                title={`取消 ${chip.label}: ${chip.name}`}
              >
                <span className={styles.chipText}>
                  {chip.label}: {chip.name}
                </span>
                <CloseOutlined className={styles.chipClose} />
              </button>
            ))}
          </div>

          <button
            type="button"
            className={styles.clearBtn}
            onClick={onReset}
            disabled={isPending}
            aria-label="清空所有筛选"
          >
            <DeleteOutlined />
            <span>清空</span>
          </button>
        </div>
      )}

      {/* 底部滑出的筛选抽屉 */}
      <Drawer
        open={drawerOpen}
        onClose={() => setDrawerOpen(false)}
        placement="bottom"
        size={540}
        zIndex={1350}
        styles={{
          header: { display: "none" },
          body: { padding: 0 },
          content: {
            background: "var(--public-surface-2)",
            borderRadius: "20px 20px 0 0",
          },
          wrapper: { height: "78vh", maxHeight: "640px" },
        }}
      >
        <div className={styles.drawerContainer}>
          {/* 抽屉头部 */}
          <div className={styles.drawerHeader}>
            <div className={styles.drawerTitleArea}>
              <span className={styles.drawerTitle}>筛选片库</span>
              {activeCount > 0 && (
                <span className={styles.drawerSub}>已选 {activeCount} 个条件</span>
              )}
            </div>

            <button
              type="button"
              className={styles.closeBtn}
              onClick={() => setDrawerOpen(false)}
              aria-label="关闭筛选抽屉"
            >
              <CloseOutlined />
            </button>
          </div>

          {/* 抽屉内容区 */}
          <div className={styles.drawerBody}>
            {sortList.map((key) => {
              const label = titles[key] || key;
              const tags = tagsMap[key] || [];
              if (tags.length === 0) return null;
              if (key === "Category" && !hasVisibleCategoryTags(tags)) return null;
              const activeVal = resolveActiveTagValue(
                key,
                normalizeTagValue(draftParams[key]),
              );

              return (
                <div key={`drawer-row-${key}`} className={styles.filterGroup}>
                  <div className={styles.groupTitle}>{label}</div>
                  <div className={styles.groupGrid}>
                    {tags.map((tag, idx) => {
                      const val = normalizeTagValue(tag.Value);
                      const isActive = activeVal === val;

                      return (
                        <button
                          key={`drawer-${key}-${tag.Value}-${idx}`}
                          type="button"
                          className={`${styles.groupTag} ${
                            isActive ? styles.active : ""
                          }`}
                          onClick={() => handleDraftTagClick(key, tag.Value)}
                        >
                          {tag.Name}
                        </button>
                      );
                    })}
                  </div>
                </div>
              );
            })}
          </div>

          {/* 抽屉底部操作栏 */}
          <div className={styles.drawerFooter}>
            <button
              type="button"
              className={styles.footerResetBtn}
              onClick={handleResetDraft}
            >
              <RedoOutlined />
              <span>重置</span>
            </button>

            <button
              type="button"
              className={styles.footerApplyBtn}
              onClick={handleApplyDraft}
            >
              <CheckOutlined />
              <span>确定 {typeof total === "number" ? `(${total}部)` : ""}</span>
            </button>
          </div>
        </div>
      </Drawer>
    </div>
  );
}
