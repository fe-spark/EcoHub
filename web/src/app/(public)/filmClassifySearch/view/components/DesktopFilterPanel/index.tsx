"use client";

import React, { useState } from "react";
import { DownOutlined, UpOutlined, CloseOutlined, DeleteOutlined } from "@ant-design/icons";
import { hasVisibleCategoryTags, resolveActiveTagValue } from "../../filter-params";
import styles from "./index.module.less";

interface TagItem {
  Name: string;
  Value: string;
}

export interface ActiveChipItem {
  key: string;
  label: string;
  name: string;
  value: string;
}

interface DesktopFilterPanelProps {
  sortList: string[];
  titles: Record<string, string>;
  tagsMap: Record<string, TagItem[]>;
  activeParams: Record<string, string>;
  activeChips: ActiveChipItem[];
  total?: number;
  isPending: boolean;
  onTagClick: (key: string, value: string) => void;
  onRemoveChip: (key: string) => void;
  onReset: () => void;
  normalizeTagValue: (v: unknown) => string;
}

const COLLAPSE_THRESHOLD = 14;

export default function DesktopFilterPanel({
  sortList,
  titles,
  tagsMap,
  activeParams,
  activeChips,
  total,
  isPending,
  onTagClick,
  onRemoveChip,
  onReset,
  normalizeTagValue,
}: DesktopFilterPanelProps) {
  return (
    <div className={styles.desktopPanel} aria-label="PC端多维筛选面板">
      {/* 多维筛选行 */}
      <div className={styles.filterRows}>
        {sortList.map((key) => {
          const label = titles[key] || key;
          const tags = tagsMap[key] || [];
          if (tags.length === 0) return null;
          if (key === "Category" && !hasVisibleCategoryTags(tags)) return null;
          const activeValue = resolveActiveTagValue(
            key,
            normalizeTagValue(activeParams[key]),
          );

          return (
            <DesktopRow
              key={`desktop-row-${key}`}
              filterKey={key}
              label={label}
              tags={tags}
              activeValue={activeValue}
              isPending={isPending}
              onTagClick={onTagClick}
              normalizeTagValue={normalizeTagValue}
            />
          );
        })}
      </div>

      {/* 已选条件面包屑条 */}
      {activeChips.length > 0 && (
        <div className={styles.activeBar}>
          <div className={styles.activeLabel}>
            <span>已选:</span>
            {typeof total === "number" && (
              <span className={styles.totalBadge}>({total}部)</span>
            )}
          </div>

          <div className={styles.chipsWrap}>
            {activeChips.map((chip) => (
              <button
                key={`chip-${chip.key}`}
                type="button"
                className={styles.activeChip}
                onClick={() => onRemoveChip(chip.key)}
                disabled={isPending}
                title={`取消 ${chip.label}: ${chip.name}`}
              >
                <span className={styles.chipCategory}>{chip.label}:</span>
                <span className={styles.chipValue}>{chip.name}</span>
                <CloseOutlined className={styles.chipClose} />
              </button>
            ))}

            <button
              type="button"
              className={styles.clearBtn}
              onClick={onReset}
              disabled={isPending}
            >
              <DeleteOutlined />
              <span>清空全部</span>
            </button>
          </div>
        </div>
      )}
    </div>
  );
}

function DesktopRow({
  filterKey,
  label,
  tags,
  activeValue,
  isPending,
  onTagClick,
  normalizeTagValue,
}: {
  filterKey: string;
  label: string;
  tags: TagItem[];
  activeValue: string;
  isPending: boolean;
  onTagClick: (key: string, value: string) => void;
  normalizeTagValue: (v: unknown) => string;
}) {
  const [expanded, setExpanded] = useState(false);
  const isCollapsible = tags.length > COLLAPSE_THRESHOLD;
  const visibleTags = isCollapsible && !expanded ? tags.slice(0, COLLAPSE_THRESHOLD) : tags;

  return (
    <div className={styles.row}>
      <div className={styles.label}>{label}</div>
      <div className={styles.optionsArea}>
        <div className={styles.optionsWrap}>
          {visibleTags.map((tag, idx) => {
            const val = normalizeTagValue(tag.Value);
            const isActive = activeValue === val;

            return (
              <button
                key={`${filterKey}-${tag.Value}-${idx}`}
                type="button"
                className={`${styles.tagOption} ${isActive ? styles.active : ""}`}
                disabled={isPending}
                onClick={() => onTagClick(filterKey, tag.Value)}
                aria-pressed={isActive}
              >
                {tag.Name}
              </button>
            );
          })}
        </div>

        {isCollapsible && (
          <button
            type="button"
            className={styles.expandBtn}
            onClick={() => setExpanded((p) => !p)}
            aria-label={expanded ? "收起部分选项" : "展开更多选项"}
          >
            <span>{expanded ? "收起" : "更多"}</span>
            {expanded ? <UpOutlined /> : <DownOutlined />}
          </button>
        )}
      </div>
    </div>
  );
}
