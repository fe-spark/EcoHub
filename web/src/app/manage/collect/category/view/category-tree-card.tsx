import React, { useCallback, useMemo, useRef } from "react";
import { Button, Empty, Space, Switch, Table, Tag, Typography, Card } from "antd";
import type { TableProps } from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  FolderOpenOutlined,
  HolderOutlined,
  ReloadOutlined,
  SaveOutlined,
  SubnodeOutlined,
} from "@ant-design/icons";
import {
  DndContext,
  PointerSensor,
  closestCenter,
  useSensor,
  useSensors,
  type CollisionDetection,
  type DragEndEvent,
  type Modifier,
} from "@dnd-kit/core";
import { SortableContext, useSortable, verticalListSortingStrategy } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import type { FilmClassNode } from "./types";
import styles from "./index.module.less";

interface CategoryTreeCardProps {
  classTree: FilmClassNode[];
  expandedKeys: React.Key[];
  loadingTree: boolean;
  savingTree: boolean;
  resettingTree: boolean;
  updatingShowIds: number[];
  hasPendingChanges: boolean;
  onRefresh: () => void;
  onReset: () => void;
  onSave: () => void;
  onExpand: (keys: React.Key[]) => void;
  onMove: (dragId: number, dropId: number) => void;
  onShowChange: (id: number, show: boolean) => void;
}

function flattenVisibleNodes(nodes: FilmClassNode[], expandedKeys: React.Key[]) {
  return nodes.flatMap((node) => {
    if (!expandedKeys.includes(node.id)) {
      return [node.id];
    }
    return [node.id, ...flattenVisibleNodes(node.children || [], expandedKeys)];
  });
}

function SortableTableRow(props: React.HTMLAttributes<HTMLTableRowElement> & { "data-row-key"?: React.Key }) {
  const rowKey = props["data-row-key"];
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: String(rowKey) });

  const style: React.CSSProperties = {
    ...props.style,
    transform: CSS.Translate.toString(transform),
    transition,
    ...(isDragging ? { position: "relative", zIndex: 1 } : {}),
  };

  return (
    <tr
      {...props}
      ref={setNodeRef}
      style={style}
      className={[props.className, isDragging ? styles.draggingRow : ""].filter(Boolean).join(" ")}
      {...attributes}
      {...listeners}
    />
  );
}

export default function CategoryTreeCard(props: CategoryTreeCardProps) {
  const {
    classTree,
    expandedKeys,
    loadingTree,
    savingTree,
    resettingTree,
    updatingShowIds,
    hasPendingChanges,
    onRefresh,
    onReset,
    onSave,
    onExpand,
    onMove,
    onShowChange,
  } = props;
  const treePanelRef = useRef<HTMLDivElement>(null);
  const tableBodyRectRef = useRef<DOMRect | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const sortableItems = useMemo(() => flattenVisibleNodes(classTree, expandedKeys).map(String), [classTree, expandedKeys]);

  const collisionDetection = useCallback<CollisionDetection>((args) => {
    const pointer = args.pointerCoordinates;
    const bodyRect = tableBodyRectRef.current;
    if (pointer && bodyRect) {
      const isInsideBody =
        pointer.x >= bodyRect.left &&
        pointer.x <= bodyRect.right &&
        pointer.y >= bodyRect.top &&
        pointer.y <= bodyRect.bottom;
      if (!isInsideBody) {
        return [];
      }
    }
    return closestCenter(args);
  }, []);

  const restrictDragToTableBody = useCallback<Modifier>(({ activeNodeRect, transform }) => {
    const bodyRect = tableBodyRectRef.current;
    if (!bodyRect || !activeNodeRect) {
      return transform;
    }

    const minY = bodyRect.top - activeNodeRect.top;
    const maxY = bodyRect.bottom - activeNodeRect.bottom;
    return {
      ...transform,
      y: Math.min(Math.max(transform.y, minY), maxY),
    };
  }, []);

  const handleDragStart = () => {
    tableBodyRectRef.current = treePanelRef.current?.querySelector(".ant-table-tbody")?.getBoundingClientRect() ?? null;
  };

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event;
    if (!over || active.id === over.id) {
      return;
    }
    onMove(Number(active.id), Number(over.id));
  };

  const columns: ColumnsType<FilmClassNode> = [
    {
      title: "排序",
      key: "drag",
      width: 60,
      align: "center",
      render: () => <HolderOutlined className={styles.dragHandle} title="按住拖拽排序" />,
    },
    {
      title: "ID",
      dataIndex: "id",
      width: 70,
      align: "center",
      render: (value: number) => (
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          #{value}
        </Typography.Text>
      ),
    },
    {
      title: "分类名称",
      dataIndex: "name",
      render: (value: string, record) => {
        const isRoot = record.pid === 0;
        const subCount = record.children?.length || 0;
        return (
          <Space size={8} align="center">
            {isRoot ? (
              <FolderOpenOutlined style={{ color: "var(--ant-color-primary)", fontSize: 15 }} />
            ) : (
              <SubnodeOutlined style={{ color: "var(--ant-color-text-tertiary)", fontSize: 13 }} />
            )}
            <Typography.Text strong={isRoot} style={{ fontSize: isRoot ? 14 : 13 }}>
              {value}
            </Typography.Text>
            {isRoot && subCount > 0 && (
              <Tag color="default" style={{ marginInlineStart: 4, borderRadius: 4, fontSize: 12 }}>
                {subCount} 个子类
              </Tag>
            )}
          </Space>
        );
      },
    },
    {
      title: "层级关系",
      dataIndex: "pid",
      width: 110,
      align: "center",
      render: (value: number) =>
        value === 0 ? (
          <Tag color="orange" variant="filled">
            一级主类
          </Tag>
        ) : (
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            二级 (PID: {value})
          </Typography.Text>
        ),
    },
    {
      title: "排序序号",
      dataIndex: "sort",
      width: 90,
      align: "center",
      render: (value?: number) => (
        <Typography.Text type="secondary">{value || 0}</Typography.Text>
      ),
    },
    {
      title: "子项数",
      dataIndex: "children",
      width: 90,
      align: "center",
      render: (children?: FilmClassNode[], record?: FilmClassNode) => {
        if (record?.pid === 0) {
          const count = children?.length || 0;
          return <Typography.Text strong={count > 0}>{count}</Typography.Text>;
        }
        return <Typography.Text type="secondary">-</Typography.Text>;
      },
    },
    {
      title: "显示",
      dataIndex: "show",
      width: 90,
      fixed: "right",
      align: "center",
      render: (value: boolean, record) => (
        <Switch
          size="small"
          checked={value}
          loading={updatingShowIds.includes(record.id)}
          onChange={(checked) => onShowChange(record.id, checked)}
        />
      ),
    },
  ];
  const tableComponents: TableProps<FilmClassNode>["components"] = {
    body: {
      row: SortableTableRow,
    },
  };

  return (
    <div className={styles.treePanel} ref={treePanelRef}>
      <DndContext
        sensors={sensors}
        modifiers={[restrictDragToTableBody]}
        collisionDetection={collisionDetection}
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <SortableContext items={sortableItems} strategy={verticalListSortingStrategy}>
          <Table<FilmClassNode>
            bordered
            rowKey="id"
            columns={columns}
            dataSource={classTree}
            components={tableComponents}
            loading={loadingTree}
            pagination={false}
            size="middle"
            scroll={{ x: "max-content" }}
            locale={{ emptyText: <Empty description="暂无分类数据" /> }}
            rowClassName={(record) => {
              const isExpandable = (record.children?.length || 0) > 0;
              const levelClass = record.pid === 0 ? styles.level1Row : styles.level2Row;
              return `${levelClass} ${isExpandable ? styles.expandableRow : ""}`;
            }}
            onRow={(record) => ({
              onClick: (event: React.MouseEvent) => {
                const target = event.target as HTMLElement;
                if (
                  target.closest(".ant-switch") ||
                  target.closest(".ant-btn") ||
                  target.closest("button") ||
                  target.closest("input") ||
                  target.closest(`.${styles.dragHandle}`) ||
                  target.closest(".ant-table-row-expand-icon")
                ) {
                  return;
                }
                if ((record.children?.length || 0) > 0) {
                  const isExpanded = expandedKeys.includes(record.id);
                  const nextKeys = isExpanded
                    ? expandedKeys.filter((k) => k !== record.id)
                    : [...expandedKeys, record.id];
                  onExpand(nextKeys);
                }
              },
            })}
            title={() => (
              <div className={styles.tableHeader}>
                <div className={styles.tableTitle}>分类管理</div>
                <Space wrap className={styles.tableActions}>
                  <Button icon={<ReloadOutlined />} onClick={onRefresh} loading={loadingTree}>
                    刷新分类
                  </Button>
                  <Button onClick={onReset} loading={resettingTree}>
                    重置分类
                  </Button>
                  <Button type="primary" icon={<SaveOutlined />} onClick={onSave} loading={savingTree} disabled={!hasPendingChanges}>
                    保存变更
                  </Button>
                </Space>
              </div>
            )}
            expandable={{
              expandedRowKeys: expandedKeys,
              rowExpandable: (record) => (record.children?.length || 0) > 0,
              onExpandedRowsChange: onExpand,
              expandIconColumnIndex: 2,
            }}
          />
        </SortableContext>
      </DndContext>
    </div>
  );
}
