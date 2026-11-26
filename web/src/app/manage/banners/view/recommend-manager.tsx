"use client";

import React, { useCallback, useEffect, useId, useState } from "react";
import {
  Button,
  Dropdown,
  Empty,
  Form,
  Input,
  InputNumber,
  Modal,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
} from "antd";
import type { MenuProps } from "antd";
import {
  DeleteOutlined,
  HolderOutlined,
  MoreOutlined,
  PlusOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
  rectSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";

import { useAppMessage } from "@/lib/useAppMessage";
import { ApiDelete, ApiGet, ApiPatch, ApiPost, ApiPut } from "@/lib/client-api";
import {
  ContentSectionCard,
  ContentSourceSelect,
} from "@/app/manage/components/content-shell";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import FilmPicker from "@/app/manage/components/film-picker";
import { FilmOption } from "./types";
import { QuickStrategy } from "./banner-quick-config";
import RecommendQuickModal, { RecommendPreviewGroup } from "./recommend-quick-modal";
import RecommendQuickDrawer from "./recommend-quick-drawer";
import styles from "./recommend-manager.module.less";

interface ChartOption {
  key: string;
  title: string;
}

interface RecommendAdminItem {
  itemId: string;
  groupId: string;
  sourceId: string;
  mid: number;
  sort: number;
  doubanScore?: string;
  name: string;
  picture: string;
  year: string;
  cName: string;
  area: string;
  remarks: string;
  score?: number;
  unavailable?: boolean;
}

interface RecommendAdminGroup {
  id: string;
  sourceId: string;
  title: string;
  sort: number;
  enabled: boolean;
  builtin?: boolean;
  builtinKey?: string;
  autoSync: boolean;
  syncChartKey: string;
  syncLimit: number;
  lastSyncedAt?: string;
  items: RecommendAdminItem[];
}

interface SourceOption {
  id: string;
  name: string;
}

function SortableGroupRow({
  group,
  selected,
  chartTitle,
  onSelect,
  onToggleEnabled,
  onMenuAction,
}: {
  group: RecommendAdminGroup;
  selected: boolean;
  chartTitle: string;
  onSelect: () => void;
  onToggleEnabled: (checked: boolean) => void;
  onMenuAction: (key: string) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: group.id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.6 : 1,
  };

  const menuItems: MenuProps["items"] = [
    { key: "edit", label: "编辑分组" },
    ...(group.syncChartKey ? [{ key: "sync", label: "同步豆瓣" }] : []),
    { key: "clear", label: "清空影片", disabled: group.items.length === 0 },
    { type: "divider" },
    { key: "delete", label: "删除分组", danger: true },
  ];

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={`${styles.groupRow} ${selected ? styles.groupRowActive : ""}`}
      onClick={onSelect}
    >
      <div className={styles.groupLeft}>
        <span
          className={styles.dragHandle}
          {...attributes}
          {...listeners}
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
            {group.builtin ? (
              <Tag style={{ margin: 0, fontSize: 11, lineHeight: "16px", padding: "0 5px" }}>
                内置
              </Tag>
            ) : null}
          </div>
          <div className={styles.groupMeta}>
            {group.builtin
              ? "系统内置板块（仅开关与排序）"
              : `${group.items.length} 部 · ${group.autoSync ? `跟随 ${chartTitle}` : "手动排片"}`}
          </div>
        </div>
      </div>

      <div className={styles.groupRight} onClick={(e) => e.stopPropagation()}>
        <Switch
          size="small"
          checked={group.enabled}
          onChange={onToggleEnabled}
        />
        {!group.builtin ? (
          <Dropdown
            trigger={["click"]}
            menu={{
              items: menuItems,
              onClick: ({ key }) => onMenuAction(key),
            }}
          >
            <Button size="small" type="text" icon={<MoreOutlined />} aria-label="更多操作" />
          </Dropdown>
        ) : null}
      </div>
    </div>
  );
}

function SortableItemCard({
  item,
  index,
  onRemove,
}: {
  item: RecommendAdminItem;
  index: number;
  onRemove: () => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: item.itemId });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    opacity: isDragging ? 0.6 : 1,
  };

  return (
    <div ref={setNodeRef} style={style} className={styles.itemCard}>
      <div className={styles.posterBox} {...attributes} {...listeners} title="拖拽调整顺序">
        {item.picture ? (
          /* eslint-disable-next-line @next/next/no-img-element */
          <img src={item.picture} alt={item.name} className={styles.posterImg} />
        ) : null}
        <Tag color={index < 3 ? "red" : "default"} className={styles.rankTag}>
          #{index + 1}
        </Tag>
        {item.doubanScore ? (
          <Tag color="gold" className={styles.scoreTag}>
            ★ {item.doubanScore}
          </Tag>
        ) : null}
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
        <Button
          size="small"
          type="text"
          danger
          icon={<DeleteOutlined />}
          onClick={onRemove}
          title="移出推荐"
        />
      </div>
    </div>
  );
}

export default function RecommendManager() {
  const { message, modal } = useAppMessage();
  const groupDndId = useId();
  const itemDndId = useId();

  const [loading, setLoading] = useState(true);
  const [sources, setSources] = useState<SourceOption[]>([]);
  const [sourceId, setSourceId] = useState("");
  const [primaryId, setPrimaryId] = useState("");
  const [chartOptions, setChartOptions] = useState<ChartOption[]>([]);
  const [serverGroups, setServerGroups] = useState<RecommendAdminGroup[]>([]);
  const [groups, setGroups] = useState<RecommendAdminGroup[]>([]);
  const [selectedGroupId, setSelectedGroupId] = useState("");
  const [savingChanges, setSavingChanges] = useState(false);

  // 新建/编辑分组弹窗
  const [groupModalOpen, setGroupModalOpen] = useState(false);
  const [editingGroup, setEditingGroup] = useState<RecommendAdminGroup | null>(null);
  const [groupSaving, setGroupSaving] = useState(false);
  const [groupForm] = Form.useForm();

  // 选片弹窗（与首页轮播复用同一套 FilmPicker）
  const [filmPickerOpen, setFilmPickerOpen] = useState(false);

  // 两段式一键排片（配置 Modal -> 预览 Drawer）
  const [quickModalOpen, setQuickModalOpen] = useState(false);
  const [quickDrawerOpen, setQuickDrawerOpen] = useState(false);
  const [quickStrategy, setQuickStrategy] = useState<QuickStrategy>("douban");
  const [quickPreview, setQuickPreview] = useState<RecommendPreviewGroup[]>([]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 6 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const loadData = useCallback(
    async (targetSourceId?: string) => {
      setLoading(true);
      try {
        const sid = targetSourceId ?? sourceId;
        const [recRes, srcRes] = await Promise.all([
          ApiGet<{
            sourceId: string;
            primarySourceId: string;
            chartOptions: ChartOption[];
            groups: RecommendAdminGroup[];
          }>(sid ? `/manage/recommends?sourceId=${encodeURIComponent(sid)}` : "/manage/recommends"),
          sources.length === 0
            ? ApiGet<{ sources?: SourceOption[] }>("/manage/banners").catch(() => null)
            : Promise.resolve(null),
        ]);
        if (recRes.code === 0 && recRes.data) {
          const nextGroups = recRes.data.groups || [];
          setSourceId(recRes.data.sourceId || "");
          setPrimaryId(recRes.data.primarySourceId || "");
          setChartOptions(recRes.data.chartOptions || []);
          setServerGroups(nextGroups);
          setGroups(nextGroups);
          setSelectedGroupId((prev) =>
            nextGroups.some((g) => g.id === prev) ? prev : nextGroups[0]?.id || "",
          );
        } else {
          message.error(recRes.msg || "加载首页推荐失败");
        }
        if (srcRes && srcRes.code === 0 && srcRes.data?.sources) {
          setSources(srcRes.data.sources);
        }
      } catch {
        message.error("加载首页推荐失败");
      } finally {
        setLoading(false);
      }
    },
    [message, sourceId, sources.length],
  );

  useEffect(() => {
    void loadData("");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const selectedGroup = groups.find((g) => g.id === selectedGroupId) || groups[0] || null;
  const customGroups = groups.filter((g) => !g.builtin);

  const openCreateGroup = () => {
    setEditingGroup(null);
    groupForm.setFieldsValue({
      title: "",
      enabled: true,
      autoSync: false,
      syncChartKey: chartOptions[0]?.key || "movie_showing",
      syncLimit: 12,
    });
    setGroupModalOpen(true);
  };

  const openEditGroup = (g: RecommendAdminGroup) => {
    setEditingGroup(g);
    groupForm.setFieldsValue({
      title: g.title,
      enabled: g.enabled,
      autoSync: g.autoSync,
      syncChartKey: g.syncChartKey || chartOptions[0]?.key || "movie_showing",
      syncLimit: g.syncLimit || 12,
    });
    setGroupModalOpen(true);
  };

  const handleSaveGroup = async () => {
    try {
      const vals = await groupForm.validateFields();
      setGroupSaving(true);
      const payload = {
        sourceId,
        title: vals.title,
        enabled: Boolean(vals.enabled),
        autoSync: Boolean(vals.autoSync),
        syncChartKey: vals.autoSync ? vals.syncChartKey : "",
        syncLimit: Number(vals.syncLimit) || 12,
      };
      const res = editingGroup
        ? await ApiPatch(`/manage/recommends/groups/${editingGroup.id}`, payload)
        : await ApiPost("/manage/recommends/groups", payload);
      if (res.code === 0) {
        message.success(editingGroup ? "分组已更新" : "分组已创建");
        setGroupModalOpen(false);
        await loadData(sourceId);
      } else {
        message.error(res.msg || "保存分组失败");
      }
    } catch {
      // 表单校验失败不弹窗
    } finally {
      setGroupSaving(false);
    }
  };

  const handleToggleEnabled = (g: RecommendAdminGroup, checked: boolean) => {
    setGroups((prev) =>
      prev.map((item) => (item.id === g.id ? { ...item, enabled: checked } : item)),
    );
  };

  const handleGroupDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = groups.findIndex((g) => g.id === active.id);
    const newIndex = groups.findIndex((g) => g.id === over.id);
    if (oldIndex === -1 || newIndex === -1) return;
    setGroups((prev) => arrayMove(prev, oldIndex, newIndex));
  };

  const handleItemDragEnd = (event: DragEndEvent) => {
    if (!selectedGroup) return;
    const { active, over } = event;
    if (!over || active.id === over.id) return;
    const oldIndex = selectedGroup.items.findIndex((i) => i.itemId === active.id);
    const newIndex = selectedGroup.items.findIndex((i) => i.itemId === over.id);
    if (oldIndex === -1 || newIndex === -1) return;
    const nextItems = arrayMove(selectedGroup.items, oldIndex, newIndex);
    setGroups((prev) =>
      prev.map((g) => (g.id === selectedGroup.id ? { ...g, items: nextItems } : g)),
    );
  };

  const groupOrderDirty =
    groups.length === serverGroups.length &&
    groups.some((g, idx) => g.id !== serverGroups[idx]?.id);

  const serverById = new Map(serverGroups.map((g) => [g.id, g]));
  const toggledGroups = groups.filter((g) => {
    const orig = serverById.get(g.id);
    return orig && orig.enabled !== g.enabled;
  });
  const itemOrderDirtyGroups = groups.filter((g) => {
    const orig = serverById.get(g.id);
    if (!orig || orig.items.length !== g.items.length) return false;
    return g.items.some((it, idx) => it.itemId !== orig.items[idx]?.itemId);
  });

  const hasUnsavedChanges =
    groupOrderDirty || toggledGroups.length > 0 || itemOrderDirtyGroups.length > 0;

  const handleDiscardChanges = () => {
    setGroups(serverGroups);
  };

  const handleSaveAllChanges = async () => {
    if (!hasUnsavedChanges) return;
    setSavingChanges(true);
    try {
      if (groupOrderDirty) {
        const res = await ApiPut("/manage/recommends/groups/order", {
          sourceId,
          ids: groups.map((g) => g.id),
        });
        if (res.code !== 0) {
          message.error(res.msg || "分组排序保存失败");
          return;
        }
      }
      for (const g of toggledGroups) {
        const res = await ApiPatch(`/manage/recommends/groups/${g.id}`, {
          enabled: g.enabled,
        });
        if (res.code !== 0) {
          message.error(res.msg || `更新「${g.title}」开关失败`);
          return;
        }
      }
      for (const g of itemOrderDirtyGroups) {
        const res = await ApiPut(`/manage/recommends/groups/${g.id}/items/order`, {
          ids: g.items.map((i) => i.itemId),
        });
        if (res.code !== 0) {
          message.error(res.msg || `保存「${g.title}」影片排序失败`);
          return;
        }
      }
      message.success("已保存首页推荐配置");
      await loadData(sourceId);
    } finally {
      setSavingChanges(false);
    }
  };

  const handleSourceChange = (nextSourceId: string) => {
    if (!nextSourceId || nextSourceId === sourceId || savingChanges) return;
    if (!hasUnsavedChanges) {
      void loadData(nextSourceId);
      return;
    }
    modal.confirm({
      title: "切换采集源？",
      content: "当前调整还没保存，继续后会丢掉这次修改。",
      okText: "切换",
      cancelText: "留下",
      onOk: () => void loadData(nextSourceId),
    });
  };

  const handleSyncGroup = async (g: RecommendAdminGroup) => {
    const res = await ApiPost(`/manage/recommends/groups/${g.id}/sync`, {});
    if (res.code === 0) {
      message.success(`「${g.title}」已同步最新豆瓣榜单`);
      await loadData(sourceId);
    } else {
      message.error(res.msg || "同步失败");
    }
  };

  const handleClearGroup = (g: RecommendAdminGroup) => {
    modal.confirm({
      title: `清空「${g.title}」下的全部影片？`,
      okText: "清空",
      okButtonProps: { danger: true },
      onOk: async () => {
        const res = await ApiDelete(`/manage/recommends/groups/${g.id}/items`);
        if (res.code === 0) {
          message.success("已清空");
          await loadData(sourceId);
        } else {
          message.error(res.msg || "清空失败");
        }
      },
    });
  };

  const handleDeleteGroup = (g: RecommendAdminGroup) => {
    modal.confirm({
      title: `删除分组「${g.title}」？`,
      content: "删除后该分组及内部已排影片将一并移除。",
      okText: "删除",
      okButtonProps: { danger: true },
      onOk: async () => {
        const res = await ApiDelete(`/manage/recommends/groups/${g.id}`);
        if (res.code === 0) {
          message.success("已删除分组");
          await loadData(sourceId);
        } else {
          message.error(res.msg || "删除失败");
        }
      },
    });
  };

  const handleGroupMenuAction = (g: RecommendAdminGroup, key: string) => {
    switch (key) {
      case "edit":
        openEditGroup(g);
        break;
      case "sync":
        void handleSyncGroup(g);
        break;
      case "clear":
        handleClearGroup(g);
        break;
      case "delete":
        handleDeleteGroup(g);
        break;
    }
  };

  const handlePickFilm = async (film: FilmOption) => {
    if (!selectedGroup) return;
    const res = await ApiPost(`/manage/recommends/groups/${selectedGroup.id}/items`, {
      mid: film.id,
    });
    if (res.code === 0) {
      message.success(`已将「${film.name}」加入「${selectedGroup.title}」`);
      setFilmPickerOpen(false);
      await loadData(sourceId);
    } else {
      message.error(res.msg || "添加失败");
    }
  };

  const handleRemoveItem = async (itemId: string) => {
    const res = await ApiDelete(`/manage/recommends/items/${itemId}`);
    if (res.code === 0) {
      message.success("已移出");
      await loadData(sourceId);
    } else {
      message.error(res.msg || "移出失败");
    }
  };

  return (
    <Spin spinning={loading}>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 12, marginBottom: 16 }}>
        <ContentSourceSelect
          sources={sources}
          sourceId={sourceId}
          primaryId={primaryId}
          disabled={loading || savingChanges}
          onChange={handleSourceChange}
        />
        <Space wrap>
          {customGroups.length > 0 && (
            <Button
              type="primary"
              icon={<ThunderboltOutlined />}
              onClick={() => setQuickModalOpen(true)}
            >
              一键排片
            </Button>
          )}
          <Button icon={<PlusOutlined />} onClick={openCreateGroup}>
            新建分组
          </Button>
        </Space>
      </div>

      {groups.length === 0 ? (
        <ContentSectionCard title="推荐分组" bodyPadding>
          <Empty
            description="当前站点暂未配置首页推荐分组"
            style={{ padding: "36px 0" }}
          >
            <Button type="primary" icon={<PlusOutlined />} onClick={openCreateGroup}>
              新建分组
            </Button>
          </Empty>
        </ContentSectionCard>
      ) : (
        <div className={styles.splitLayout}>
          {/* 左侧：推荐分组列表（拖拽排序 + 开关 + 三点文字菜单） */}
          <ContentSectionCard title="推荐分组" count={groups.length} bodyPadding>
            <DndContext
              id={groupDndId}
              sensors={sensors}
              collisionDetection={closestCenter}
              onDragEnd={(e) => void handleGroupDragEnd(e)}
            >
              <SortableContext
                items={groups.map((g) => g.id)}
                strategy={verticalListSortingStrategy}
              >
                <div className={styles.groupList}>
                  {groups.map((g) => {
                    const chartTitle =
                      chartOptions.find((o) => o.key === g.syncChartKey)?.title || g.syncChartKey;
                    return (
                      <SortableGroupRow
                        key={g.id}
                        group={g}
                        selected={selectedGroup?.id === g.id}
                        chartTitle={chartTitle}
                        onSelect={() => setSelectedGroupId(g.id)}
                        onToggleEnabled={(checked) => void handleToggleEnabled(g, checked)}
                        onMenuAction={(key) => handleGroupMenuAction(g, key)}
                      />
                    );
                  })}
                </div>
              </SortableContext>
            </DndContext>
          </ContentSectionCard>

          {/* 右侧：当前选中分组的内容 */}
          <ContentSectionCard
            title={selectedGroup ? selectedGroup.title : "分组内容"}
            count={selectedGroup && !selectedGroup.builtin ? selectedGroup.items.length : undefined}
            extra={
              selectedGroup ? (
                <Space size={6}>
                  {selectedGroup.builtin ? (
                    <Tag style={{ margin: 0 }}>系统内置板块</Tag>
                  ) : null}
                  <Tag color={selectedGroup.enabled ? "green" : "default"} style={{ margin: 0 }}>
                    {selectedGroup.enabled ? "前台展示中" : "已隐藏"}
                  </Tag>
                  {!selectedGroup.builtin && selectedGroup.autoSync ? (
                    <Tag color="blue" style={{ margin: 0 }}>
                      自动跟随豆瓣
                    </Tag>
                  ) : null}
                </Space>
              ) : null
            }
            actions={
              selectedGroup && !selectedGroup.builtin ? (
                <Button
                  type="primary"
                  icon={<PlusOutlined />}
                  onClick={() => setFilmPickerOpen(true)}
                >
                  添加影片
                </Button>
              ) : null
            }
            bodyPadding
          >
            {selectedGroup?.builtin ? (
              <Empty
                description="此为系统内置首页板块，内容由系统自动生成；可在左侧通过开关控制显示/隐藏，或拖拽把手调整首页排序位置。"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                style={{ padding: "48px 16px" }}
              />
            ) : !selectedGroup || selectedGroup.items.length === 0 ? (
              <Empty
                description="该分组暂无影片"
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                style={{ padding: "36px 0" }}
              >
                {selectedGroup && (
                  <Button type="primary" icon={<PlusOutlined />} onClick={() => setFilmPickerOpen(true)}>
                    添加影片
                  </Button>
                )}
              </Empty>
            ) : (
              <DndContext
                id={itemDndId}
                sensors={sensors}
                collisionDetection={closestCenter}
                onDragEnd={(e) => void handleItemDragEnd(e)}
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
                        onRemove={() => void handleRemoveItem(item.itemId)}
                      />
                    ))}
                  </div>
                </SortableContext>
              </DndContext>
            )}
          </ContentSectionCard>
        </div>
      )}

      {/* 新建 / 编辑分组弹窗 */}
      <Modal
        title={editingGroup ? "编辑推荐分组" : "新建推荐分组"}
        open={groupModalOpen}
        onCancel={() => setGroupModalOpen(false)}
        onOk={() => void handleSaveGroup()}
        confirmLoading={groupSaving}
        destroyOnHidden
      >
        <Form form={groupForm} layout="vertical">
          <Form.Item
            name="title"
            label="分组名称"
            rules={[{ required: true, message: "请输入分组名称" }]}
          >
            <Input placeholder="例如：院线热映、站长精选" maxLength={32} />
          </Form.Item>
          <Form.Item name="enabled" label="前台显示" valuePropName="checked">
            <Switch checkedChildren="显示" unCheckedChildren="隐藏" />
          </Form.Item>
          <Form.Item name="autoSync" label="自动跟随豆瓣榜单（每 6 小时自动刷新）" valuePropName="checked">
            <Switch checkedChildren="开启" unCheckedChildren="关闭" />
          </Form.Item>
          <Form.Item
            noStyle
            shouldUpdate={(prev, cur) => prev.autoSync !== cur.autoSync}
          >
            {({ getFieldValue }) =>
              getFieldValue("autoSync") ? (
                <>
                  <Form.Item name="syncChartKey" label="绑定豆瓣/Bangumi 榜单">
                    <Select
                      options={chartOptions.map((o) => ({ value: o.key, label: o.title }))}
                    />
                  </Form.Item>
                  <Form.Item name="syncLimit" label="自动匹配数量上限（1-24）">
                    <InputNumber min={1} max={24} style={{ width: "100%" }} />
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
        </Form>
      </Modal>

      {/* 与首页轮播完全一致的选片弹窗 */}
      <FilmPicker
        open={filmPickerOpen}
        title={selectedGroup ? `添加影片到「${selectedGroup.title}」` : "选择影片"}
        sourceId={sourceId}
        onCancel={() => setFilmPickerOpen(false)}
        onSelect={(film) => void handlePickFilm(film)}
      />

      {/* 两段式一键排片：配置弹窗 -> 预览抽屉 */}
      <RecommendQuickModal
        open={quickModalOpen}
        sourceId={sourceId}
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
        sourceId={sourceId}
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
          void loadData(sourceId);
        }}
      />

      <UnsavedChangesBar
        visible={hasUnsavedChanges}
        saving={savingChanges}
        message="排片顺序或开关未保存"
        onDiscard={handleDiscardChanges}
        onSave={() => void handleSaveAllChanges()}
      />
    </Spin>
  );
}
