"use client";

import React, { useId, useState } from "react";
import { Button, Dropdown, Empty, Form, Space, Spin, Tooltip } from "antd";
import type { MenuProps } from "antd";
import {
  ApartmentOutlined,
  CompassOutlined,
  MoreOutlined,
  PlusOutlined,
  SyncOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import {
  DndContext,
  DragOverlay,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
  type DragStartEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";

import { useAppMessage } from "@/lib/useAppMessage";
import { ApiPatch, ApiPost } from "@/lib/client-api";
import { ContentSectionCard } from "@/app/manage/components/content-shell";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import FilmPicker from "@/app/manage/components/film-picker";
import { QuickStrategy } from "./banner-quick-config";
import RecommendQuickModal, { RecommendPreviewGroup } from "./recommend-quick-modal";
import RecommendQuickDrawer from "./recommend-quick-drawer";
import RecommendGroupModal from "./recommend-group-modal";
import RecommendItemsPanel from "./recommend-items-panel";
import { GroupRowCard, SortableGroupRow } from "./recommend-sortable-items";
import { RecommendAdminGroup } from "./recommend-types";
import { BannerSourceOption } from "./types";
import { useRecommendWorkspace } from "./use-recommend-workspace";
import styles from "./recommend-manager.module.less";

interface RecommendManagerProps {
  sourceId: string;
  primaryLoading: boolean;
  onSourcesSync?: (
    sources: BannerSourceOption[] | undefined,
    resolvedSourceId: string,
    primarySourceId?: string,
  ) => void;
  onDirtyChange?: (dirty: boolean) => void;
  onBusyChange?: (busy: boolean) => void;
}

export default function RecommendManager({
  sourceId,
  primaryLoading,
  onSourcesSync,
  onDirtyChange,
  onBusyChange,
}: RecommendManagerProps) {
  const { message } = useAppMessage();
  const groupDndId = useId();

  const {
    loading,
    activeSourceId,
    chartOptions,
    categoryOptions,
    groups,
    selectedGroup,
    hasUnsavedChanges,
    savingChanges,
    presetting,
    importingCategories,
    syncingGroupId,
    loadData,
    setSelectedGroupId,
    discardChanges,
    handleGroupDragEnd,
    handleItemDragEnd,
    handleToggleEnabled,
    handleSaveAllChanges,
    syncGroup,
    syncAllGroups,
    clearGroup,
    deleteGroup,
    pickFilm,
    removeItem,
    initPresets,
    importCategories,
  } = useRecommendWorkspace({
    sourceId,
    primaryLoading,
    onSourcesSync,
    onDirtyChange,
    onBusyChange,
  });

  const [groupModalOpen, setGroupModalOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<RecommendAdminGroup | null>(null);
  const [groupSaving, setGroupSaving] = useState(false);
  const [groupForm] = Form.useForm();

  const [filmPickerOpen, setFilmPickerOpen] = useState(false);

  const [quickModalOpen, setQuickModalOpen] = useState(false);
  const [quickDrawerOpen, setQuickDrawerOpen] = useState(false);
  const [quickStrategy, setQuickStrategy] = useState<QuickStrategy>("douban");
  const [quickPreview, setQuickPreview] = useState<RecommendPreviewGroup[]>([]);
  const [activeGroupId, setActiveGroupId] = useState<string | null>(null);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const activeGroup = activeGroupId
    ? groups.find((g) => g.id === activeGroupId) || null
    : null;

  const openCreateGroup = () => {
    setEditingGroup(null);
    groupForm.setFieldsValue({
      title: "",
      enabled: true,
      sourceMode: "manual",
      autoSync: true,
      syncChartKey: chartOptions[0]?.key || "movie_showing",
      categoryId: categoryOptions[0]?.id,
      syncLimit: 12,
    });
    setGroupModalOpen(true);
  };

  const openEditGroup = (g: RecommendAdminGroup) => {
    setEditingGroup(g);
    const mode = g.categoryId > 0 ? "category" : g.syncChartKey ? "chart" : "manual";
    groupForm.setFieldsValue({
      title: g.title,
      enabled: g.enabled,
      sourceMode: mode,
      autoSync: mode === "chart" ? g.autoSync : true,
      syncChartKey: g.syncChartKey || chartOptions[0]?.key || "movie_showing",
      categoryId: g.categoryId > 0 ? g.categoryId : categoryOptions[0]?.id,
      syncLimit: g.syncLimit || 12,
    });
    setGroupModalOpen(true);
  };

  const handleSaveGroup = async () => {
    try {
      const vals = await groupForm.validateFields();
      setGroupSaving(true);
      const isChart = vals.sourceMode === "chart";
      const isCategory = vals.sourceMode === "category";
      const payload = {
        sourceId: activeSourceId,
        title: vals.title,
        enabled: Boolean(vals.enabled),
        autoSync: isChart ? Boolean(vals.autoSync) : false,
        syncChartKey: isChart ? vals.syncChartKey : "",
        categoryId: isCategory ? Number(vals.categoryId) || 0 : 0,
        syncLimit: Number(vals.syncLimit) || 12,
      };
      const res = editingGroup
        ? await ApiPatch(`/manage/recommends/groups/${editingGroup.id}`, payload)
        : await ApiPost("/manage/recommends/groups", payload);
      if (res.code === 0) {
        message.success(editingGroup ? "分组已更新" : "分组已创建");
        setGroupModalOpen(false);
        await loadData(activeSourceId);
      } else {
        message.error(res.msg || "保存分组失败");
      }
    } catch {
      // 表单校验失败不弹窗
    } finally {
      setGroupSaving(false);
    }
  };

  const handleGroupMenuAction = (g: RecommendAdminGroup, key: string) => {
    if (key === "edit") openEditGroup(g);
    else if (key === "clear") clearGroup(g);
    else if (key === "delete") deleteGroup(g);
  };

  const chartTitleOf = (key: string) =>
    chartOptions.find((c) => c.key === key)?.title || key;
  const chartGroups = groups.filter((g) => Boolean(g.syncChartKey) && g.categoryId <= 0);
  const customGroups = groups.filter((g) => !g.syncChartKey && g.categoryId <= 0);
  const moreMenuItems: MenuProps["items"] = [
    {
      key: "import-categories",
      icon: <ApartmentOutlined />,
      label: "导入站点分类",
      disabled: !activeSourceId || importingCategories || categoryOptions.length === 0,
    },
    {
      key: "init-presets",
      icon: <CompassOutlined />,
      label: "补全热门榜单预设",
      disabled: !activeSourceId || presetting,
    },
  ];

  return (
    <div className={styles.workspaceRoot}>
      {loading ? (
        <div className={styles.loadingOverlay} aria-hidden>
          <Spin />
        </div>
      ) : null}
      {groups.length === 0 ? (
        <ContentSectionCard
          className={styles.paneCard}
          bodyClassName={styles.paneCardBody}
          title="推荐分组"
          bodyPadding
        >
          <div className={styles.emptyCenter}>
            <Empty description="当前站点暂未配置首页推荐分组">
              <Space wrap>
                <Button
                  type="primary"
                  icon={<CompassOutlined />}
                  loading={presetting}
                  disabled={!activeSourceId}
                  onClick={() => void initPresets()}
                >
                  导入热门榜单
                </Button>
                <Button
                  icon={<ApartmentOutlined />}
                  loading={importingCategories}
                  disabled={!activeSourceId || categoryOptions.length === 0}
                  onClick={() => void importCategories()}
                >
                  导入站点分类
                </Button>
                <Button icon={<PlusOutlined />} disabled={!activeSourceId} onClick={openCreateGroup}>
                  新建分组
                </Button>
              </Space>
            </Empty>
          </div>
        </ContentSectionCard>
      ) : (
        <div className={styles.splitLayout}>
          <ContentSectionCard
            className={styles.paneCard}
            bodyClassName={styles.paneCardBody}
            title="推荐分组"
            count={groups.length}
              actions={
                <Space size={6}>
                  {chartGroups.length > 0 ? (
                    <Tooltip title="同步全部榜单分组">
                      <Button
                        size="small"
                        icon={<SyncOutlined />}
                        loading={syncingGroupId === "all"}
                        disabled={!activeSourceId || Boolean(syncingGroupId)}
                        onClick={() => void syncAllGroups()}
                      >
                        同步
                      </Button>
                    </Tooltip>
                  ) : null}
                  {customGroups.length > 0 ? (
                    <Tooltip title="一键排片（自选专题）">
                      <Button
                        size="small"
                        icon={<ThunderboltOutlined />}
                        disabled={!activeSourceId}
                        aria-label="一键排片（自选专题）"
                        onClick={() => setQuickModalOpen(true)}
                      >
                        {chartGroups.length === 0 ? "一键排片" : null}
                      </Button>
                    </Tooltip>
                  ) : null}
                  <Button
                    size="small"
                    type="primary"
                    icon={<PlusOutlined />}
                    disabled={!activeSourceId}
                    onClick={openCreateGroup}
                  >
                    新建
                  </Button>
                  <Dropdown
                    trigger={["click"]}
                    menu={{
                      items: moreMenuItems,
                      onClick: ({ key }) => {
                        if (key === "import-categories") void importCategories();
                        else if (key === "init-presets") void initPresets();
                      },
                    }}
                  >
                    <Button size="small" icon={<MoreOutlined />} aria-label="更多操作" />
                  </Dropdown>
                </Space>
              }
              bodyPadding
            >
              <DndContext
                id={groupDndId}
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragStart={(e: DragStartEvent) => setActiveGroupId(String(e.active.id))}
                onDragEnd={(e: DragEndEvent) => {
                  setActiveGroupId(null);
                  handleGroupDragEnd(e);
                }}
                onDragCancel={() => setActiveGroupId(null)}
              >
                <SortableContext
                  items={groups.map((g) => g.id)}
                  strategy={verticalListSortingStrategy}
                >
                  <div className={styles.groupList}>
                    {groups.map((g) => (
                      <SortableGroupRow
                        key={g.id}
                        group={g}
                        selected={g.id === selectedGroup?.id}
                        chartTitle={chartTitleOf(g.syncChartKey)}
                        onSelect={() => setSelectedGroupId(g.id)}
                        onToggleEnabled={(checked) => handleToggleEnabled(g.id, checked)}
                        onMenuAction={(key) => handleGroupMenuAction(g, key)}
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
                  {activeGroup ? (
                    <div className={styles.dragOverlayCard}>
                      <GroupRowCard
                        group={activeGroup}
                        selected={activeGroup.id === selectedGroup?.id}
                        chartTitle={chartTitleOf(activeGroup.syncChartKey)}
                        onSelect={() => {}}
                        onToggleEnabled={() => {}}
                        onMenuAction={() => {}}
                      />
                    </div>
                  ) : null}
                </DragOverlay>
              </DndContext>
            </ContentSectionCard>

            <RecommendItemsPanel
              selectedGroup={selectedGroup}
              syncingGroupId={syncingGroupId}
              sensors={sensors}
              onSyncGroup={(group) => void syncGroup(group)}
              onAddFilm={() => setFilmPickerOpen(true)}
              onRemoveItem={(itemId) => void removeItem(itemId)}
              onItemDragEnd={handleItemDragEnd}
            />
          </div>
        )}

      <RecommendGroupModal
        open={groupModalOpen}
        editingGroup={editingGroup}
        saving={groupSaving}
        form={groupForm}
        chartOptions={chartOptions}
        categoryOptions={categoryOptions}
        onCancel={() => setGroupModalOpen(false)}
        onOk={() => void handleSaveGroup()}
      />

      <FilmPicker
        open={filmPickerOpen}
        title={selectedGroup ? `添加影片到「${selectedGroup.title}」` : "选择影片"}
        sourceId={activeSourceId}
        onCancel={() => setFilmPickerOpen(false)}
        onSelect={(film) => void pickFilm(film, () => setFilmPickerOpen(false))}
      />

      <RecommendQuickModal
        open={quickModalOpen}
        sourceId={activeSourceId}
        groups={customGroups.map((g) => ({
          id: g.id,
          title: g.title,
          syncLimit: g.syncLimit || 12,
        }))}
        onClose={() => setQuickModalOpen(false)}
        onGenerated={(previewGroups, strategy) => {
          setQuickPreview(previewGroups);
          setQuickStrategy(strategy);
          setQuickModalOpen(false);
          setQuickDrawerOpen(true);
        }}
      />

      <RecommendQuickDrawer
        open={quickDrawerOpen}
        sourceId={activeSourceId}
        strategy={quickStrategy}
        previewGroups={quickPreview}
        onChange={setQuickPreview}
        onRedo={() => {
          setQuickDrawerOpen(false);
          setQuickModalOpen(true);
        }}
        onClose={() => {
          setQuickDrawerOpen(false);
          setQuickPreview([]);
        }}
        onSuccess={() => {
          setQuickDrawerOpen(false);
          setQuickPreview([]);
          void loadData(activeSourceId);
        }}
      />

      <UnsavedChangesBar
        visible={hasUnsavedChanges}
        saving={savingChanges}
        message="排片顺序或开关未保存"
        onDiscard={discardChanges}
        onSave={() => void handleSaveAllChanges()}
      />
    </div>
  );
}
