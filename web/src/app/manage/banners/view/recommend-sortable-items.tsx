"use client";

import React, { useState } from "react";
import { Button, Dropdown, Switch } from "antd";
import type { MenuProps } from "antd";
import {
  DeleteOutlined,
  HolderOutlined,
  MoreOutlined,
  StarFilled,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { RecommendAdminGroup, RecommendAdminItem } from "./recommend-types";
import styles from "./recommend-manager.module.less";

interface GroupRowProps {
  group: RecommendAdminGroup;
  selected: boolean;
  chartTitle: string;
  dragHandleProps?: React.HTMLAttributes<HTMLSpanElement>;
  onSelect: () => void;
  onToggleEnabled: (checked: boolean) => void;
  onMenuAction: (key: string) => void;
}

export function GroupRowCard({
  group,
  selected,
  chartTitle,
  dragHandleProps,
  onSelect,
  onToggleEnabled,
  onMenuAction,
}: GroupRowProps) {
  const isCategoryGroup = group.categoryId > 0;
  const menuItems: MenuProps["items"] = [
    { key: "edit", label: "编辑分组" },
    ...(!isCategoryGroup
      ? [{ key: "clear", label: "清空影片", disabled: group.items.length === 0 }]
      : []),
    { type: "divider" },
    { key: "delete", label: "删除分组", danger: true },
  ];
  const modeLabel = isCategoryGroup
    ? `站点分类「${group.categoryName || group.title}」`
    : group.syncChartKey
      ? group.autoSync
        ? `自动榜单「${chartTitle}」`
        : `手动榜单「${chartTitle}」`
      : "自选专题";
  const hiddenWarn = isCategoryGroup && Boolean(group.categoryHidden);

  return (
    <div
      className={`${styles.groupRow} ${selected ? styles.groupRowActive : ""}`}
      onClick={onSelect}
    >
      <div className={styles.groupLeft}>
        <span
          className={styles.dragHandle}
          {...dragHandleProps}
          onClick={(e) => e.stopPropagation()}
          title="拖拽排序"
        >
          <HolderOutlined />
        </span>
        <div className={styles.groupInfo}>
          <div className={styles.groupTitleRow}>
            <span className={styles.groupTitle} title={group.title}>
              {group.title}
            </span>
          </div>
          <div className={`${styles.groupMeta} ${hiddenWarn ? styles.groupMetaWarn : ""}`}>
            {hiddenWarn
              ? `已在分类管理隐藏 · ${modeLabel}`
              : `${group.items.length} 部 · ${modeLabel}`}
          </div>
        </div>
      </div>

      <div className={styles.groupRight} onClick={(e) => e.stopPropagation()}>
        <Switch size="small" checked={group.enabled} onChange={onToggleEnabled} />
        <Dropdown
          trigger={["click"]}
          menu={{
            items: menuItems,
            onClick: ({ key }) => onMenuAction(key),
          }}
        >
          <Button size="small" type="text" icon={<MoreOutlined />} aria-label="更多操作" />
        </Dropdown>
      </div>
    </div>
  );
}

export function SortableGroupRow(props: Omit<GroupRowProps, "dragHandleProps">) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: props.group.id });

  const style: React.CSSProperties = {
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 10 : undefined,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={isDragging ? styles.dragPlaceholder : undefined}
    >
      <GroupRowCard {...props} dragHandleProps={{ ...attributes, ...listeners }} />
    </div>
  );
}

interface ItemCardProps {
  item: RecommendAdminItem;
  index: number;
  readOnly?: boolean;
  dragHandleProps?: React.HTMLAttributes<HTMLDivElement>;
  onRemove?: () => void;
}

export function RecommendItemCard({
  item,
  index,
  readOnly,
  dragHandleProps,
  onRemove,
}: ItemCardProps) {
  const [failedSrc, setFailedSrc] = useState<string | null>(null);
  const showFallback = !item.picture || failedSrc === item.picture;

  return (
    <div className={`${styles.itemCard} ${item.unavailable ? styles.unavailableCard : ""}`}>
      <div
        className={`${styles.posterBox} ${readOnly ? styles.posterBoxReadOnly : ""}`}
        {...(readOnly ? undefined : dragHandleProps)}
        title={readOnly ? item.name : "拖拽调整顺序"}
      >
        {showFallback ? (
          <div className={styles.posterFallback}>
            <VideoCameraOutlined style={{ fontSize: 22 }} />
            <span>{item.name}</span>
          </div>
        ) : (
          /* eslint-disable-next-line @next/next/no-img-element */
          <img
            src={item.picture}
            alt={item.name}
            draggable={false}
            className={styles.posterImg}
            onError={() => setFailedSrc(item.picture)}
          />
        )}
        <div className={styles.posterScrim} />
        <span className={`${styles.rankBadge} ${index < 3 ? styles.rankBadgeTop : ""}`}>
          #{index + 1}
        </span>
        {item.doubanScore ? (
          <span className={styles.scoreBadge}>
            <StarFilled style={{ fontSize: 10 }} />
            {item.doubanScore}
          </span>
        ) : null}
        {item.unavailable ? (
          <span className={styles.unavailableBadge}>
            {item.categoryHidden ? "分类已隐藏" : "已下线"}
          </span>
        ) : null}
        {item.remarks ? <div className={styles.remarksBar}>{item.remarks}</div> : null}
      </div>
      <div className={styles.itemFooter}>
        <div className={styles.itemText}>
          <div className={styles.itemName} title={item.name}>
            {item.name}
          </div>
          <div className={styles.itemSub}>
            {[item.year, item.cName].filter(Boolean).join(" · ") || "-"}
          </div>
        </div>
        {!readOnly && onRemove ? (
          <Button
            size="small"
            type="text"
            danger
            icon={<DeleteOutlined />}
            onClick={onRemove}
            title="移出推荐"
          />
        ) : null}
      </div>
    </div>
  );
}

export function SortableItemCard(props: Omit<ItemCardProps, "dragHandleProps">) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: props.item.itemId });

  const style: React.CSSProperties = {
    transform: CSS.Translate.toString(transform),
    transition,
    zIndex: isDragging ? 10 : undefined,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={isDragging ? styles.dragPlaceholder : undefined}
    >
      <RecommendItemCard {...props} dragHandleProps={{ ...attributes, ...listeners }} />
    </div>
  );
}
