import React, { createContext, useCallback, useContext, useMemo, useRef } from "react";
import { Empty, Space, Switch, Table, Tag, Tooltip, Typography } from "antd";
import type { TableProps } from "antd";
import type { ColumnsType } from "antd/es/table";
import {
  FolderOpenOutlined,
  HolderOutlined,
  QuestionCircleOutlined,
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
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./index.module.less";

const RowDragHandleContext = createContext<React.HTMLAttributes<HTMLElement>>({});

interface CategoryTreeCardProps {
  classTree: FilmClassNode[];
  expandedKeys: React.Key[];
  loadingTree: boolean;
  savingTree: boolean;
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

  const handleProps = useMemo(
    () => ({ ...attributes, ...listeners }),
    [attributes, listeners],
  );

  return (
    <RowDragHandleContext.Provider value={handleProps}>
      <tr
        {...props}
        ref={setNodeRef}
        style={style}
        className={[props.className, isDragging ? styles.draggingRow : ""].filter(Boolean).join(" ")}
      />
    </RowDragHandleContext.Provider>
  );
}

function DragHandleButton({ canDrag }: { canDrag: boolean }) {
  const handleProps = useContext(RowDragHandleContext);
  return (
    <span
      {...(canDrag ? handleProps : {})}
      style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", padding: "4px 6px" }}
    >
      <HolderOutlined
        className={styles.dragHandle}
        title={canDrag ? "按住拖拽排序" : "不可拖拽"}
        style={canDrag ? undefined : { opacity: 0.35, cursor: "not-allowed" }}
      />
    </span>
  );
}

export default function CategoryTreeCard(props: CategoryTreeCardProps) {
  const {
    classTree,
    expandedKeys,
    loadingTree,
    savingTree,
    onExpand,
    onMove,
    onShowChange,
  } = props;
  const { canWrite } = useManagePermission();
  const treePanelRef = useRef<HTMLDivElement>(null);
  const tableBodyRectRef = useRef<DOMRect | null>(null);
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 4 } }));
  const sortableItems = useMemo(() => flattenVisibleNodes(classTree, expandedKeys).map(String), [classTree, expandedKeys]);
  const rootShowById = useMemo(() => {
    const out: Record<number, boolean> = {};
    for (const node of classTree) {
      out[node.id] = node.show;
    }
    return out;
  }, [classTree]);

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
    if (!canWrite || savingTree) {
      return;
    }
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
      render: () => <DragHandleButton canDrag={canWrite && !savingTree} />,
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
      title: (
        <Space size={4}>
          <span>影片数</span>
          <Tooltip title="主站统计全量分类影片，一级含子类汇总；非主站仅统计该站独立入库影片">
            <QuestionCircleOutlined />
          </Tooltip>
        </Space>
      ),
      dataIndex: "filmCount",
      width: 105,
      align: "center",
      render: (filmCount?: number) => {
        const count = filmCount ?? 0;
        return (
          <Typography.Text strong={count > 0} type={count > 0 ? undefined : "secondary"}>
            {count}
          </Typography.Text>
        );
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
          disabled={!canWrite || savingTree || (record.pid > 0 && rootShowById[record.pid] === false)}
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
            rowKey="id"
            columns={columns}
            dataSource={classTree}
            components={tableComponents}
            loading={loadingTree}
            pagination={false}
            size="middle"
            scroll={{ x: "max-content" }}
            locale={{ emptyText: <Empty description="该采集站还没有分类" /> }}
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
