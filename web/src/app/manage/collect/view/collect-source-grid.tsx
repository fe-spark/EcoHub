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
import CollectSourceCard, { SortableCollectSourceCard } from "./collect-source-card";
import {
  isActiveCollectStatus,
  COLLECT_SOURCE_WARN_COUNT,
  type FilmSource,
} from "./types";
import styles from "./index.module.less";

interface CollectSourceGridProps {
  siteList: FilmSource[];
  selectedSourceIds: string[];
  activeCollectIds: string[];
  hiddenDoneIds: string[];
  canWrite: boolean;
  canAddSource: boolean;
  onSelect: (id: string, checked: boolean) => void;
  onChangeCollectDuration: (id: string, value: number) => void;
  onStartTask: (record: FilmSource) => void;
  onTerminateTask: (id: string) => void;
  onEditSource: (id: string) => void;
  onDeleteSource: (id: string) => void;
  onOpenAddDialog: () => void;
  onSortList: (nextList: FilmSource[]) => void;
}

export default function CollectSourceGrid({
  siteList,
  selectedSourceIds,
  activeCollectIds,
  hiddenDoneIds,
  canWrite,
  canAddSource,
  onSelect,
  onChangeCollectDuration,
  onStartTask,
  onTerminateTask,
  onEditSource,
  onDeleteSource,
  onOpenAddDialog,
  onSortList,
}: CollectSourceGridProps) {
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
    const oldIndex = siteList.findIndex((item) => item.id === active.id);
    const newIndex = siteList.findIndex((item) => item.id === over.id);
    if (oldIndex === -1 || newIndex === -1) {
      return;
    }
    const nextList = arrayMove(siteList, oldIndex, newIndex);
    onSortList(nextList);
  };

  const handleDragCancel = () => {
    setActiveId(null);
  };

  const activeSite = activeId ? siteList.find((item) => item.id === activeId) : null;

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
        items={siteList.map((s) => s.id)}
        strategy={rectSortingStrategy}
        disabled={!canWrite}
      >
        <div className={styles.cardGrid}>
          {siteList.map((site) => {
            const hiddenDone =
              hiddenDoneIds.includes(site.id) &&
              site.progress != null &&
              !isActiveCollectStatus(site.progress.status);
            return (
              <SortableCollectSourceCard
                key={site.id}
                record={hiddenDone ? { ...site, progress: null } : site}
                selected={selectedSourceIds.includes(site.id)}
                active={activeCollectIds.includes(site.id)}
                onSelect={onSelect}
                onChangeCollectDuration={onChangeCollectDuration}
                onStartTask={onStartTask}
                onTerminateTask={onTerminateTask}
                onEditSource={onEditSource}
                onDeleteSource={onDeleteSource}
              />
            );
          })}
          {canAddSource && canWrite ? (
            <button
              type="button"
              className={styles.addSourceTile}
              onClick={onOpenAddDialog}
            >
              <PlusOutlined className={styles.addSourceIcon} />
              <span className={styles.addSourceLabel}>新增采集站</span>
              <span className={styles.addSourceHint}>
                {siteList.length >= COLLECT_SOURCE_WARN_COUNT
                  ? `已超过建议数量（${COLLECT_SOURCE_WARN_COUNT}）`
                  : "添加自定义影视资源接口"}
              </span>
            </button>
          ) : null}
        </div>
      </SortableContext>
      <DragOverlay
        dropAnimation={{
          duration: 180,
          easing: "cubic-bezier(0.18, 0.67, 0.6, 1.22)",
        }}
      >
        {activeSite ? (
          <div className={styles.dragOverlayCard}>
            <CollectSourceCard
              record={activeSite}
              selected={selectedSourceIds.includes(activeSite.id)}
              active={activeCollectIds.includes(activeSite.id)}
              onSelect={() => {}}
              onChangeCollectDuration={() => {}}
              onStartTask={() => {}}
              onTerminateTask={() => {}}
              onEditSource={() => {}}
              onDeleteSource={() => {}}
            />
          </div>
        ) : null}
      </DragOverlay>
    </DndContext>
  );
}
