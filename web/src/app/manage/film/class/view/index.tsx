"use client";

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  closestCenter,
  DndContext,
  DragEndEvent,
  DragOverlay,
  DragOverEvent,
  DragStartEvent,
  KeyboardSensor,
  MouseSensor,
  TouchSensor,
  useSensor,
  useSensors,
} from "@dnd-kit/core";
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import {
  Alert,
  Button,
  Card,
  Empty,
  Form,
  Input,
  Modal,
  Popconfirm,
  Space,
  Switch,
  Tag,
  Tooltip,
} from "antd";
import {
  DeleteOutlined,
  DownOutlined,
  EditOutlined,
  EyeInvisibleOutlined,
  EyeOutlined,
  ReloadOutlined,
  RightOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageShell from "../../../components/page-shell";
import styles from "./index.module.less";

interface FilmClassNode {
  id: number;
  pid: number;
  name: string;
  alias?: string;
  show: boolean;
  sort?: number;
  children?: FilmClassNode[];
}

function getSortableNodeId(sortableId: string | null) {
  if (!sortableId) {
    return null;
  }

  const [, rawId] = sortableId.split("-");
  const nodeId = Number(rawId);
  return Number.isNaN(nodeId) ? null : nodeId;
}

function DragPreview(props: {
  entity:
    | { type: "root"; node: FilmClassNode; collapsed: boolean }
    | { type: "child"; node: FilmClassNode; rootId: number };
  width?: number | null;
}) {
  const { entity, width } = props;

  if (entity.type === "root") {
    const childNodes = entity.node.children || [];
    return (
      <div
        className={`${styles.dragPreviewGroupCard} ${styles.dragPreviewRoot}`}
        style={{ width: width || undefined }}
      >
        <div className={styles.groupHeaderCard}>
          <div
            className={`${styles.nodeCard} ${styles.nodeCardRoot} ${styles.nodeCardGrouped}`}
          >
            <div className={styles.nodeMain}>
              <div className={styles.rootDragArea}>
                <span
                  className={`${styles.levelBadge} ${styles.levelBadgeRoot}`}
                >
                  一级
                </span>
              </div>
              <div className={styles.nodeTitleBlock}>
                <div className={styles.nodeTitleRow}>
                  <span className={styles.nodeTitle}>{entity.node.name}</span>
                  <Tag color="default" style={{ marginRight: 0 }}>
                    子分类 {childNodes.length}
                  </Tag>
                  {!entity.node.show && (
                    <Tag
                      color="default"
                      icon={<EyeInvisibleOutlined />}
                      style={{ marginRight: 0 }}
                    >
                      已隐藏
                    </Tag>
                  )}
                </div>
                <div className={styles.nodeMeta}>
                  <span>ID {entity.node.id}</span>
                  <span>排序 {entity.node.sort || 0}</span>
                  <span>顶级导航分类</span>
                </div>
              </div>
            </div>
          </div>
        </div>

        {!entity.collapsed && childNodes.length > 0 && (
          <div className={styles.groupBody}>
            <div className={styles.childList}>
              {childNodes.map((child) => (
                <div
                  key={child.id}
                  className={`${styles.nodeCard} ${styles.nodeCardChild} ${styles.nodeCardGrouped}`}
                >
                  <div className={styles.nodeMain}>
                    <span
                      className={`${styles.levelBadge} ${styles.levelBadgeChild}`}
                    >
                      二级
                    </span>
                    <div className={styles.nodeTitleBlock}>
                      <div className={styles.nodeTitleRow}>
                        <span className={styles.nodeTitle}>{child.name}</span>
                        {!child.show && (
                          <Tag
                            color="default"
                            icon={<EyeInvisibleOutlined />}
                            style={{ marginRight: 0 }}
                          >
                            已隐藏
                          </Tag>
                        )}
                      </div>
                      <div className={styles.nodeMeta}>
                        <span>ID {child.id}</span>
                        <span>父级 {entity.node.id}</span>
                        <span>排序 {child.sort || 0}</span>
                        <span>挂在父分类下展示</span>
                      </div>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </div>
        )}

        {!entity.collapsed && childNodes.length === 0 && (
          <div className={styles.groupEmpty}>当前一级分类下暂无二级分类</div>
        )}

        {entity.collapsed && childNodes.length > 0 && (
          <div className={styles.groupCollapsedHint}>
            已折叠 {childNodes.length} 个二级分类
          </div>
        )}
      </div>
    );
  }

  return (
    <div
      className={`${styles.dragPreview} ${styles.dragPreviewChild}`}
      style={{ width: width || undefined }}
    >
      <div className={styles.nodeMain}>
        <span
          className={
            styles.levelBadgeChild
              ? `${styles.levelBadge} ${styles.levelBadgeChild}`
              : styles.levelBadge
          }
        >
          二级
        </span>
        <div className={styles.nodeTitleBlock}>
          <div className={styles.nodeTitleRow}>
            <span className={styles.nodeTitle}>{entity.node.name}</span>
            {!entity.node.show && (
              <Tag
                color="default"
                icon={<EyeInvisibleOutlined />}
                style={{ marginRight: 0 }}
              >
                已隐藏
              </Tag>
            )}
          </div>
          <div className={styles.nodeMeta}>
            <span>ID {entity.node.id}</span>
            <span>父级 {entity.rootId}</span>
            <span>排序 {entity.node.sort || 0}</span>
          </div>
        </div>
      </div>
    </div>
  );
}

function cloneTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.map((node) => ({
    ...node,
    children: cloneTree(node.children || []),
  }));
}

function normalizeTree(nodes: FilmClassNode[], parentId = 0): FilmClassNode[] {
  return nodes.map((node, index) => ({
    ...node,
    pid: parentId,
    sort: index + 1,
    children: normalizeTree(node.children || [], node.id),
  }));
}

function flattenTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.flatMap((node) => [node, ...(node.children || [])]);
}

function collectStats(nodes: FilmClassNode[]) {
  const all = flattenTree(nodes);
  return {
    total: all.length,
    roots: nodes.length,
    children: all.filter((item) => item.pid !== 0).length,
    hidden: all.filter((item) => !item.show).length,
  };
}

function serializeTree(nodes: FilmClassNode[]) {
  return nodes.map((node) => ({
    id: node.id,
    name: node.name,
    children: serializeTree(node.children || []),
  }));
}

function RootCard(props: {
  node: FilmClassNode;
  childCount: number;
  collapsed: boolean;
  dragAttributes: ReturnType<typeof useSortable>["attributes"];
  dragListeners: ReturnType<typeof useSortable>["listeners"];
  onToggleCollapse: (node: FilmClassNode) => void;
  onToggle: (node: FilmClassNode, checked: boolean) => void;
  onEdit: (node: FilmClassNode) => void;
  onDelete: (node: FilmClassNode) => void;
}) {
  const {
    node,
    childCount,
    collapsed,
    dragAttributes,
    dragListeners,
    onToggleCollapse,
    onToggle,
    onEdit,
    onDelete,
  } = props;

  return (
    <div
      className={`${styles.nodeCard} ${styles.nodeCardRoot} ${styles.nodeCardGrouped} ${styles.rootCardSurface}`}
      {...dragAttributes}
      {...dragListeners}
    >
      <div className={styles.nodeMain}>
        <div className={styles.rootDragArea}>
          <span className={`${styles.levelBadge} ${styles.levelBadgeRoot}`}>
            一级
          </span>
        </div>
        <div className={styles.nodeTitleBlock}>
          <div className={styles.nodeTitleRow}>
            <span className={styles.nodeTitle}>{node.name}</span>
            <Tag color="default" style={{ marginRight: 0 }}>
              子分类 {childCount}
            </Tag>
            {!node.show && (
              <Tag
                color="default"
                icon={<EyeInvisibleOutlined />}
                style={{ marginRight: 0 }}
              >
                已隐藏
              </Tag>
            )}
          </div>
          <div className={styles.nodeMeta}>
            <span>ID {node.id}</span>
            <span>排序 {node.sort || 0}</span>
            <span>顶级导航分类</span>
          </div>
        </div>
      </div>

      <div className={styles.nodeActions}>
        <Tooltip title={collapsed ? "展开子分类" : "折叠子分类"}>
          <Button
            icon={collapsed ? <RightOutlined /> : <DownOutlined />}
            onClick={() => onToggleCollapse(node)}
          >
            {collapsed ? "展开" : "折叠"}
          </Button>
        </Tooltip>
        <Switch
          checked={node.show}
          checkedChildren={<EyeOutlined />}
          unCheckedChildren={<EyeInvisibleOutlined />}
          onChange={(checked) => onToggle(node, checked)}
        />
        <Tooltip title="编辑分类">
          <Button icon={<EditOutlined />} onClick={() => onEdit(node)} />
        </Tooltip>
        <Popconfirm title="确认删除当前分类？" onConfirm={() => onDelete(node)}>
          <Tooltip title="删除分类">
            <Button danger icon={<DeleteOutlined />} />
          </Tooltip>
        </Popconfirm>
      </div>
    </div>
  );
}

function ChildCard(props: {
  node: FilmClassNode;
  rootId: number;
  activeId: number | null;
  activeSortableId: string | null;
  onToggle: (node: FilmClassNode, checked: boolean) => void;
  onEdit: (node: FilmClassNode) => void;
  onDelete: (node: FilmClassNode) => void;
  onMeasure: (nodeId: number, height: number, width: number) => void;
}) {
  const {
    node,
    rootId,
    activeId,
    activeSortableId,
    onToggle,
    onEdit,
    onDelete,
    onMeasure,
  } = props;
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: `child-${node.id}` });
  const cardRef = useRef<HTMLDivElement | null>(null);
  const isActiveChild = activeSortableId === `child-${node.id}`;

  useEffect(() => {
    if (cardRef.current) {
      onMeasure(
        node.id,
        cardRef.current.offsetHeight,
        cardRef.current.offsetWidth,
      );
    }
  }, [node.id, node.name, node.show, onMeasure]);

  return (
    <div
      ref={(element) => {
        cardRef.current = element;
        setNodeRef(element);
      }}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
      }}
      className={`${styles.nodeCard} ${styles.nodeCardChild} ${styles.nodeCardGrouped} ${isDragging ? styles.nodeDragging : ""} ${activeId === node.id ? styles.nodeActive : ""} ${isActiveChild ? styles.nodePlaceholder : ""}`}
      {...attributes}
      {...listeners}
    >
      <div
        className={`${styles.nodeMain} ${isActiveChild ? styles.nodePlaceholderContent : ""}`}
      >
        <span className={`${styles.levelBadge} ${styles.levelBadgeChild}`}>
          二级
        </span>
        <div className={styles.nodeTitleBlock}>
          <div className={styles.nodeTitleRow}>
            <span className={styles.nodeTitle}>{node.name}</span>
            {!node.show && (
              <Tag
                color="default"
                icon={<EyeInvisibleOutlined />}
                style={{ marginRight: 0 }}
              >
                已隐藏
              </Tag>
            )}
          </div>
          <div className={styles.nodeMeta}>
            <span>ID {node.id}</span>
            <span>父级 {rootId}</span>
            <span>排序 {node.sort || 0}</span>
            <span>挂在父分类下展示</span>
          </div>
        </div>
      </div>

      <div
        className={`${styles.nodeActions} ${isActiveChild ? styles.nodePlaceholderContent : ""}`}
      >
        <Switch
          checked={node.show}
          checkedChildren={<EyeOutlined />}
          unCheckedChildren={<EyeInvisibleOutlined />}
          onChange={(checked) => onToggle(node, checked)}
        />
        <Tooltip title="编辑分类">
          <Button icon={<EditOutlined />} onClick={() => onEdit(node)} />
        </Tooltip>
        <Popconfirm title="确认删除当前分类？" onConfirm={() => onDelete(node)}>
          <Tooltip title="删除分类">
            <Button danger icon={<DeleteOutlined />} />
          </Tooltip>
        </Popconfirm>
      </div>
    </div>
  );
}

function RootGroup(props: {
  root: FilmClassNode;
  activeId: number | null;
  activeSortableId: string | null;
  collapsed: boolean;
  sensors: ReturnType<typeof useSensors>;
  onDragStart: (event: DragStartEvent) => void;
  onDragOver: (event: DragOverEvent) => void;
  onChildDragEnd: (rootId: number, event: DragEndEvent) => void;
  onDragCancel: () => void;
  onToggleCollapse: (rootId: number) => void;
  onToggle: (node: FilmClassNode, checked: boolean) => void;
  onEdit: (node: FilmClassNode) => void;
  onDelete: (node: FilmClassNode) => void;
  onMeasure: (nodeId: number, height: number, width: number) => void;
  activePreview: {
    entity:
      | { type: "root"; node: FilmClassNode; collapsed: boolean }
      | { type: "child"; node: FilmClassNode; rootId: number };
    width: number | null;
  } | null;
}) {
  const {
    root,
    activeId,
    activeSortableId,
    collapsed,
    sensors,
    onDragStart,
    onDragOver,
    onChildDragEnd,
    onDragCancel,
    onToggleCollapse,
    onToggle,
    onEdit,
    onDelete,
    onMeasure,
    activePreview,
  } = props;
  const childNodes = root.children || [];
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: `root-${root.id}` });
  const childSortableIds = childNodes.map((node) => `child-${node.id}`);
  const groupRef = useRef<HTMLDivElement | null>(null);
  const isActiveRoot = activeSortableId === `root-${root.id}`;

  useEffect(() => {
    if (groupRef.current) {
      onMeasure(
        root.id,
        groupRef.current.offsetHeight,
        groupRef.current.offsetWidth,
      );
    }
  }, [root.id, root.name, root.show, collapsed, childNodes.length, onMeasure]);

  return (
    <div
      ref={(element) => {
        groupRef.current = element;
        setNodeRef(element);
      }}
      style={{
        transform: CSS.Transform.toString(transform),
        transition,
      }}
      className={`${styles.groupCard} ${isDragging ? styles.nodeDragging : ""} ${activeId === root.id ? styles.nodeActive : ""} ${isActiveRoot ? styles.nodePlaceholder : ""}`}
    >
      <div
        className={`${styles.groupHeaderCard} ${isActiveRoot ? styles.nodePlaceholderContent : ""}`}
      >
        <RootCard
          node={root}
          childCount={childNodes.length}
          collapsed={collapsed}
          dragAttributes={attributes}
          dragListeners={listeners}
          onToggleCollapse={() => onToggleCollapse(root.id)}
          onToggle={onToggle}
          onEdit={onEdit}
          onDelete={onDelete}
        />
      </div>

      {!collapsed && childNodes.length > 0 && (
        <div className={styles.groupBody}>
          <DndContext
            sensors={sensors}
            collisionDetection={closestCenter}
            onDragStart={onDragStart}
            onDragOver={onDragOver}
            onDragEnd={(event) => onChildDragEnd(root.id, event)}
            onDragCancel={onDragCancel}
          >
            <SortableContext
              items={childSortableIds}
              strategy={verticalListSortingStrategy}
            >
              <div className={styles.childList}>
                {childNodes.map((node) => (
                  <ChildCard
                    key={node.id}
                    node={node}
                    rootId={root.id}
                    activeId={activeId}
                    activeSortableId={activeSortableId}
                    onToggle={onToggle}
                    onEdit={onEdit}
                    onDelete={onDelete}
                    onMeasure={onMeasure}
                  />
                ))}
              </div>
            </SortableContext>
            <DragOverlay>
              {activePreview?.entity.type === "child" &&
              activePreview.entity.rootId === root.id ? (
                <DragPreview
                  entity={activePreview.entity}
                  width={activePreview.width}
                />
              ) : null}
            </DragOverlay>
          </DndContext>
        </div>
      )}

      {!collapsed && childNodes.length === 0 && (
        <div className={styles.groupEmpty}>当前一级分类下暂无二级分类</div>
      )}

      {collapsed && childNodes.length > 0 && (
        <div className={styles.groupCollapsedHint}>
          已折叠 {childNodes.length} 个二级分类
        </div>
      )}
    </div>
  );
}

export default function FilmClassPageView() {
  const [classTree, setClassTree] = useState<FilmClassNode[]>([]);
  const [originalTree, setOriginalTree] = useState<FilmClassNode[]>([]);
  const [collapsedRootIds, setCollapsedRootIds] = useState<number[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState(false);
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editForm] = Form.useForm();
  const [editingItem, setEditingItem] = useState<FilmClassNode | null>(null);
  const [activeId, setActiveId] = useState<number | null>(null);
  const [activeSortableId, setActiveSortableId] = useState<string | null>(null);
  const [overId, setOverId] = useState<string | null>(null);
  const [rootWidths, setRootWidths] = useState<Record<number, number>>({});
  const [childWidths, setChildWidths] = useState<Record<number, number>>({});
  const { message } = useAppMessage();

  const sensors = useSensors(
    useSensor(MouseSensor, { activationConstraint: { distance: 6 } }),
    useSensor(TouchSensor, {
      activationConstraint: { delay: 150, tolerance: 8 },
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates,
    }),
  );

  const stats = useMemo(() => collectStats(classTree), [classTree]);
  const childParentMap = useMemo(
    () =>
      classTree.reduce<Record<number, number>>((map, root) => {
        for (const child of root.children || []) {
          map[child.id] = root.id;
        }
        return map;
      }, {}),
    [classTree],
  );
  const activePreview = useMemo(() => {
    if (!activeSortableId) {
      return null;
    }

    const nodeId = getSortableNodeId(activeSortableId);
    if (nodeId === null) {
      return null;
    }

    if (activeSortableId.startsWith("root-")) {
      const root = classTree.find((item) => item.id === nodeId);
      if (!root) {
        return null;
      }
      return {
        entity: {
          type: "root" as const,
          node: root,
          collapsed: collapsedRootIds.includes(nodeId),
        },
        width: rootWidths[nodeId] || null,
      };
    }

    for (const root of classTree) {
      const child = (root.children || []).find((item) => item.id === nodeId);
      if (child) {
        return {
          entity: {
            type: "child" as const,
            node: child,
            rootId: root.id,
          },
          width: childWidths[nodeId] || null,
        };
      }
    }

    return null;
  }, [activeSortableId, childWidths, classTree, collapsedRootIds, rootWidths]);
  const hasPendingChanges = useMemo(
    () =>
      JSON.stringify(serializeTree(classTree)) !==
      JSON.stringify(serializeTree(originalTree)),
    [classTree, originalTree],
  );

  const handleRootMeasure = useCallback(
    (nodeId: number, height: number, width?: number) => {
      if (width) {
        setRootWidths((prev) =>
          prev[nodeId] === width ? prev : { ...prev, [nodeId]: width },
        );
      }
    },
    [],
  );

  const handleChildMeasure = useCallback(
    (nodeId: number, height: number, width?: number) => {
      if (width) {
        setChildWidths((prev) =>
          prev[nodeId] === width ? prev : { ...prev, [nodeId]: width },
        );
      }
    },
    [],
  );

  const getFilmClassTree = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/film/class/tree");
      if (resp.code === 0) {
        const normalized = normalizeTree(resp.data.children || []);
        setClassTree(normalized);
        setOriginalTree(normalized);
        setCollapsedRootIds(normalized.map((item) => item.id));
      } else {
        message.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    getFilmClassTree();
  }, [getFilmClassTree]);

  const changeClassState = async (id: number, show: boolean) => {
    const resp = await ApiPost("/manage/film/class/update", { id, show });
    if (resp.code === 0) {
      message.success(resp.msg);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const delClass = async (id: number) => {
    const resp = await ApiPost("/manage/film/class/del", { id: String(id) });
    if (resp.code === 0) {
      message.success(resp.msg);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const openEditDialog = async (id: number) => {
    const resp = await ApiGet("/manage/film/class/find", { id });
    if (resp.code === 0) {
      setEditingItem(resp.data);
      editForm.setFieldsValue(resp.data);
      setEditOpen(true);
    } else {
      message.error(resp.msg);
    }
  };

  const onEditFinish = async (values: { name: string; show: boolean }) => {
    const resp = await ApiPost("/manage/film/class/update", {
      id: editingItem?.id,
      name: values.name,
      show: values.show,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setEditOpen(false);
      setEditingItem(null);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const resetFilmClass = async () => {
    setResetting(true);
    try {
      const resp = await ApiPost("/manage/film/class/collect", {});
      if (resp.code === 0) {
        message.success(resp.msg);
        getFilmClassTree();
        return true;
      }

      message.error(resp.msg);
      return false;
    } finally {
      setResetting(false);
    }
  };

  const handleResetConfirm = async () => {
    const success = await resetFilmClass();
    if (success) {
      setResetConfirmOpen(false);
    }
  };

  const saveTree = async () => {
    setSaving(true);
    try {
      const resp = await ApiPost("/manage/film/class/tree/save", {
        children: classTree,
      });
      if (resp.code === 0) {
        message.success(resp.msg);
        getFilmClassTree();
      } else {
        message.error(resp.msg);
      }
    } finally {
      setSaving(false);
    }
  };

  const handleRootDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);
    setActiveSortableId(null);
    setOverId(null);
    if (!over || active.id === over.id) {
      return;
    }

    const rootIds = classTree.map((item) => `root-${item.id}`);
    const oldIndex = rootIds.indexOf(String(active.id));
    const newIndex = rootIds.indexOf(String(over.id));
    if (oldIndex === -1 || newIndex === -1) {
      return;
    }

    setClassTree((prev) => normalizeTree(arrayMove(prev, oldIndex, newIndex)));
  };

  const handleChildDragEnd = (rootId: number, event: DragEndEvent) => {
    const { active, over } = event;
    setActiveId(null);
    setActiveSortableId(null);
    setOverId(null);
    if (!over || active.id === over.id) {
      return;
    }

    setClassTree((prev) => {
      const next = cloneTree(prev);
      const root = next.find((item) => item.id === rootId);
      if (!root || !root.children) {
        return prev;
      }

      const childIds = root.children.map((item) => `child-${item.id}`);
      const oldIndex = childIds.indexOf(String(active.id));
      const newIndex = childIds.indexOf(String(over.id));
      if (oldIndex === -1 || newIndex === -1) {
        return prev;
      }

      root.children = arrayMove(root.children, oldIndex, newIndex);
      return normalizeTree(next);
    });
  };

  const toggleRootCollapse = (rootId: number) => {
    setCollapsedRootIds((prev) =>
      prev.includes(rootId)
        ? prev.filter((id) => id !== rootId)
        : [...prev, rootId],
    );
  };

  const handleDragStart = (event: DragStartEvent) => {
    const id = String(event.active.id);
    setActiveSortableId(id);
    setOverId(id);
    const nodeId = getSortableNodeId(id);
    if (
      nodeId !== null &&
      (id.startsWith("root-") || id.startsWith("child-"))
    ) {
      setActiveId(nodeId);
    }
  };

  const handleDragOver = (event: DragOverEvent) => {
    if (!activeSortableId) {
      setOverId(null);
      return;
    }

    const nextOverId = event.over ? String(event.over.id) : null;
    if (!nextOverId) {
      setOverId(null);
      return;
    }

    if (activeSortableId.startsWith("root-")) {
      setOverId(nextOverId.startsWith("root-") ? nextOverId : null);
      return;
    }

    if (activeSortableId.startsWith("child-")) {
      const activeChildId = getSortableNodeId(activeSortableId);
      const overChildId = nextOverId.startsWith("child-")
        ? getSortableNodeId(nextOverId)
        : null;
      if (activeChildId === null || overChildId === null) {
        setOverId(activeSortableId);
        return;
      }

      const activeRootId = childParentMap[activeChildId];
      const overRootId = childParentMap[overChildId];
      setOverId(
        activeRootId && activeRootId === overRootId
          ? nextOverId
          : activeSortableId,
      );
      return;
    }

    setOverId(nextOverId);
  };

  const handleDragCancel = () => {
    setActiveId(null);
    setActiveSortableId(null);
    setOverId(null);
  };

  return (
    <ManagePageShell
      eyebrow="内容管理"
      title="分类管理"
      description="维护当前主站业务分类树，支持展示控制、拖拽排序和重置为原始分类。"
      actions={
        <div className={styles.heroActions}>
          <Button
            icon={<ReloadOutlined />}
            onClick={getFilmClassTree}
            loading={loading}
          >
            刷新分类
          </Button>
          <Button
            icon={<ReloadOutlined />}
            onClick={() => setResetConfirmOpen(true)}
          >
            重置分类
          </Button>
          <Button
            type="primary"
            icon={<SaveOutlined />}
            loading={saving}
            disabled={!hasPendingChanges}
            onClick={saveTree}
          >
            保存排序
          </Button>
        </div>
      }
      extra={
        <div className={styles.statsGrid}>
          <Card size="small" className={styles.statCard}>
            <span className={styles.statLabel}>分类总数</span>
            <strong className={styles.statValue}>{stats.total}</strong>
          </Card>
          <Card size="small" className={styles.statCard}>
            <span className={styles.statLabel}>一级分类</span>
            <strong className={styles.statValue}>{stats.roots}</strong>
          </Card>
          <Card size="small" className={styles.statCard}>
            <span className={styles.statLabel}>二级分类</span>
            <strong className={styles.statValue}>{stats.children}</strong>
          </Card>
          <Card size="small" className={styles.statCard}>
            <span className={styles.statLabel}>隐藏分类</span>
            <strong className={styles.statValue}>{stats.hidden}</strong>
          </Card>
        </div>
      }
      panelClassName={styles.treePanel}
      panelless
    >
      <Modal
        open={resetConfirmOpen}
        title="确认重置分类？"
        okText="确认重置"
        cancelText="取消"
        confirmLoading={resetting}
        okButtonProps={{ danger: true }}
        cancelButtonProps={{ disabled: resetting }}
        closable={!resetting}
        maskClosable={!resetting}
        keyboard={!resetting}
        onOk={handleResetConfirm}
        onCancel={() => {
          if (!resetting) {
            setResetConfirmOpen(false);
          }
        }}
      >
        <p>
          该操作会清空当前分类的业务名称、显示状态、排序等设置，并重新获取主站原始分类。
        </p>
      </Modal>

      {classTree.length === 0 ? (
        <Empty description="暂无分类数据" />
      ) : (
        <DndContext
          sensors={sensors}
          collisionDetection={closestCenter}
          onDragStart={handleDragStart}
          onDragOver={handleDragOver}
          onDragEnd={(event) => {
            if (String(event.active.id).startsWith("root-")) {
              handleRootDragEnd(event);
            }
          }}
          onDragCancel={handleDragCancel}
        >
          <SortableContext
            items={classTree.map((item) => `root-${item.id}`)}
            strategy={verticalListSortingStrategy}
          >
            <div className={styles.listWrap}>
              {classTree.map((root) => (
                <RootGroup
                  key={root.id}
                  root={root}
                  activeId={activeId}
                  activeSortableId={activeSortableId}
                  collapsed={collapsedRootIds.includes(root.id)}
                  sensors={sensors}
                  onDragStart={handleDragStart}
                  onDragOver={handleDragOver}
                  onChildDragEnd={handleChildDragEnd}
                  onDragCancel={handleDragCancel}
                  onToggleCollapse={toggleRootCollapse}
                  onToggle={(item, checked) =>
                    changeClassState(item.id, checked)
                  }
                  onEdit={(item) => openEditDialog(item.id)}
                  onDelete={(item) => delClass(item.id)}
                  onMeasure={handleRootMeasure}
                  activePreview={activePreview}
                />
              ))}
            </div>
          </SortableContext>
          <DragOverlay>
            {activePreview?.entity.type === "root" ? (
              <DragPreview
                entity={activePreview.entity}
                width={activePreview.width}
              />
            ) : null}
          </DragOverlay>
        </DndContext>
      )}
      <Modal
        title="更新分类信息"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => editForm.submit()}
        width={480}
      >
        <Form form={editForm} layout="vertical" onFinish={onEditFinish}>
          <Form.Item
            label="分类名称"
            name="name"
            rules={[{ required: true, message: "请输入分类名称" }]}
          >
            <Input placeholder="分类名称,用于首页导航展示" />
          </Form.Item>

          <Form.Item label="分类层级">
            <Tag color={editingItem?.pid === 0 ? "processing" : "default"}>
              {editingItem?.pid === 0 ? "一级分类" : "二级分类"}
            </Tag>
          </Form.Item>

          <Form.Item label="是否展示" name="show" valuePropName="checked">
            <Switch checkedChildren="展示" unCheckedChildren="隐藏" />
          </Form.Item>

          {editingItem?.children && editingItem.children.length > 0 && (
            <Form.Item label="当前子分类">
              <Space wrap>
                {editingItem.children.map((item) => (
                  <Tag key={item.id}>{item.name}</Tag>
                ))}
              </Space>
            </Form.Item>
          )}
        </Form>
      </Modal>
    </ManagePageShell>
  );
}
