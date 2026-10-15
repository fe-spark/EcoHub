"use client";

import React, { useId, useState } from "react";
import { PlusOutlined } from "@ant-design/icons";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
  DragOverlay,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  rectSortingStrategy,
} from "@dnd-kit/sortable";
import { BannerRecord } from "./types";
import { BannerCard, SortableBannerCard } from "./banner-card";
import styles from "./index.module.less";

interface BannerGridProps {
  banners: BannerRecord[];
  canWrite: boolean;
  onSortList: (nextList: BannerRecord[]) => void;
  onEdit: (record: BannerRecord) => void;
  onDelete: (id: string) => void;
  onMoveOrder: (index: number, direction: "up" | "down") => void;
  onOpenCreate: () => void;
}

export default function BannerGrid({
  banners,
  canWrite,
  onSortList,
  onEdit,
  onDelete,
  onMoveOrder,
  onOpenCreate,
}: BannerGridProps) {
  const dndId = useId();
  const [activeId, setActiveId] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 8,
      },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const handleDragStart = (event: DragStartEvent) => {
    setActiveId(String(event.active.id));
  };

  const handleDragEnd = (event: DragEndEvent) => {
    setActiveId(null);
    const { active, over } = event;
    if (!over || active.id === over.id) {
      return;
    }
    const oldIndex = banners.findIndex((b) => b.id === active.id);
    const newIndex = banners.findIndex((b) => b.id === over.id);
    if (oldIndex === -1 || newIndex === -1) {
      return;
    }
    const nextList = arrayMove(banners, oldIndex, newIndex);
    onSortList(nextList);
  };

  const handleDragCancel = () => {
    setActiveId(null);
  };

  const activeBanner = activeId
    ? banners.find((b) => b.id === activeId)
    : null;
  const activeIndex = activeBanner
    ? banners.findIndex((b) => b.id === activeBanner.id)
    : 0;

  const canAdd = canWrite;

  return (
    <DndContext
      id={dndId}
      sensors={canWrite ? sensors : undefined}
      collisionDetection={closestCenter}
      onDragStart={handleDragStart}
      onDragEnd={handleDragEnd}
      onDragCancel={handleDragCancel}
    >
      <SortableContext
        items={banners.map((b) => b.id)}
        strategy={rectSortingStrategy}
        disabled={!canWrite}
      >
        <div className={styles.cardGrid}>
          {banners.map((banner, index) => (
            <SortableBannerCard
              key={banner.id}
              record={banner}
              index={index}
              totalCount={banners.length}
              canWrite={canWrite}
              onEdit={onEdit}
              onDelete={onDelete}
              onMoveOrder={onMoveOrder}
            />
          ))}

          {canAdd && (
            <button
              type="button"
              className={styles.addBannerTile}
              onClick={onOpenCreate}
            >
              <PlusOutlined className={styles.addBannerIcon} />
              <span className={styles.addBannerLabel}>添加排片影片</span>
              <span className={styles.addBannerHint}>
                已排片 {banners.length} 部
              </span>
            </button>
          )}
        </div>
      </SortableContext>

      <DragOverlay
        dropAnimation={{
          duration: 180,
          easing: "cubic-bezier(0.18, 0.67, 0.6, 1.22)",
        }}
      >
        {activeBanner ? (
          <div className={styles.dragOverlayCard}>
            <BannerCard
              record={activeBanner}
              index={activeIndex}
              totalCount={banners.length}
              canWrite={canWrite}
              onEdit={() => {}}
              onDelete={() => {}}
              onMoveOrder={() => {}}
            />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
