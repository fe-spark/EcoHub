"use client";

import React, { useId, useState } from "react";
import { Button, Empty, Space, Tag, Tooltip } from "antd";
import { PlusOutlined, SyncOutlined } from "@ant-design/icons";
import {
  DndContext,
  DragOverlay,
  closestCenter,
  type DragEndEvent,
  type DragStartEvent,
  type SensorDescriptor,
  type SensorOptions,
} from "@dnd-kit/core";
import { SortableContext, rectSortingStrategy } from "@dnd-kit/sortable";
import { ContentSectionCard } from "@/app/manage/components/content-shell";
import { RecommendItemCard, SortableItemCard } from "./recommend-sortable-items";
import { RecommendAdminGroup } from "./recommend-types";
import styles from "./recommend-manager.module.less";

interface RecommendItemsPanelProps {
  selectedGroup: RecommendAdminGroup | null;
  syncingGroupId: string | null;
  sensors: SensorDescriptor<SensorOptions>[];
  onSyncGroup: (group: RecommendAdminGroup) => void;
  onAddFilm: () => void;
  onRemoveItem: (itemId: string) => void;
  onItemDragEnd: (event: DragEndEvent) => void;
}

export default function RecommendItemsPanel({
  selectedGroup,
  syncingGroupId,
  sensors,
  onSyncGroup,
  onAddFilm,
  onRemoveItem,
  onItemDragEnd,
}: RecommendItemsPanelProps) {
  const itemDndId = useId();
  const [activeItemId, setActiveItemId] = useState<string | null>(null);

  const isCategoryGroup = Boolean(selectedGroup && selectedGroup.categoryId > 0);
  const activeItemIndex =
    activeItemId && selectedGroup
      ? selectedGroup.items.findIndex((i) => i.itemId === activeItemId)
      : -1;
  const activeItem =
    activeItemIndex >= 0 && selectedGroup ? selectedGroup.items[activeItemIndex] : null;

  return (
    <ContentSectionCard
      className={styles.paneCard}
      bodyClassName={styles.paneCardBody}
      title={selectedGroup ? selectedGroup.title : "推荐影片"}
      count={selectedGroup ? selectedGroup.items.length : 0}
      actions={
        selectedGroup ? (
          isCategoryGroup ? (
            <Tooltip title="片单随分类管理显隐与实时片库联动，此处开关仅控制是否在公开首页展示">
              <Tag
                color={selectedGroup.categoryHidden ? "warning" : "processing"}
                style={{ margin: 0 }}
              >
                {selectedGroup.categoryHidden ? "分类管理已隐藏" : "分类联动 · 只读预览"}
              </Tag>
            </Tooltip>
          ) : (
            <Space size={8}>
              {selectedGroup.syncChartKey ? (
                <Button
                  size="small"
                  icon={<SyncOutlined />}
                  loading={syncingGroupId === selectedGroup.id}
                  disabled={Boolean(syncingGroupId)}
                  onClick={() => onSyncGroup(selectedGroup)}
                >
                  同步此组
                </Button>
              ) : null}
              <Button
                type="primary"
                size="small"
                icon={<PlusOutlined />}
                onClick={onAddFilm}
              >
                添加影片
              </Button>
            </Space>
          )
        ) : null
      }
      bodyPadding
    >
      {!selectedGroup || selectedGroup.items.length === 0 ? (
        <div className={styles.emptyCenter}>
          <Empty
            description={
              isCategoryGroup
                ? selectedGroup?.categoryHidden
                  ? `分类「${selectedGroup.categoryName || selectedGroup.title}」已在分类管理中隐藏，首页不会展示此楼层`
                  : "当前分类暂无可播影片"
                : selectedGroup?.syncChartKey
                  ? "暂无影片，可点击右上角「同步此组」或「添加影片」"
                  : "暂无影片，可点击右上角「添加影片」或左侧「一键排片」"
            }
          />
        </div>
      ) : isCategoryGroup ? (
        <div className={styles.itemGrid}>
          {selectedGroup.items.map((item, idx) => (
            <RecommendItemCard
              key={`${selectedGroup.id}-${item.mid}`}
              item={item}
              index={idx}
              readOnly
            />
          ))}
        </div>
      ) : (
        <DndContext
          id={itemDndId}
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragStart={(e: DragStartEvent) => setActiveItemId(String(e.active.id))}
          onDragEnd={(e: DragEndEvent) => {
            setActiveItemId(null);
            onItemDragEnd(e);
          }}
          onDragCancel={() => setActiveItemId(null)}
        >
          <SortableContext
            items={selectedGroup.items.map((i) => i.itemId)}
            strategy={rectSortingStrategy}
          >
            <div className={styles.itemGrid}>
              {selectedGroup.items.map((item, idx) => (
                <SortableItemCard
                  key={item.itemId}
                  item={item}
                  index={idx}
                  onRemove={() => onRemoveItem(item.itemId)}
                />
              ))}
            </div>
          </SortableContext>
          <DragOverlay
            dropAnimation={{
              duration: 180,
              easing: "cubic-bezier(0.18, 0.67, 0.6, 1.22)",
            }}
          >
            {activeItem ? (
              <div className={styles.dragOverlayCard}>
                <RecommendItemCard
                  item={activeItem}
                  index={activeItemIndex}
                  onRemove={() => {}}
                />
              </div>
            ) : null}
          </DragOverlay>
        </DndContext>
      )}
    </ContentSectionCard>
  );
}
