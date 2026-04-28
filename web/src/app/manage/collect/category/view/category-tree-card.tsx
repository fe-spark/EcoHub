import React from "react";
import { Button, Card, Empty, Flex, Popconfirm, Space, Tag, Tree, Typography } from "antd";
import type { TreeProps } from "antd/es/tree";
import { DeleteOutlined, ReloadOutlined, SaveOutlined } from "@ant-design/icons";
import type { ClassTreeDataNode, FilmClassNode } from "./types";
import styles from "./index.module.less";

interface CategoryTreeCardProps {
  classTree: FilmClassNode[];
  treeData: ClassTreeDataNode[];
  expandedKeys: React.Key[];
  loadingTree: boolean;
  savingTree: boolean;
  resettingTree: boolean;
  hasPendingChanges: boolean;
  onRefresh: () => void;
  onReset: () => void;
  onSave: () => void;
  onExpand: (keys: React.Key[]) => void;
  onDrop: TreeProps<ClassTreeDataNode>["onDrop"];
  allowDrop: TreeProps<ClassTreeDataNode>["allowDrop"];
  onDelete: (id: number) => void;
}

export default function CategoryTreeCard(props: CategoryTreeCardProps) {
  const {
    classTree,
    treeData,
    expandedKeys,
    loadingTree,
    savingTree,
    resettingTree,
    hasPendingChanges,
    onRefresh,
    onReset,
    onSave,
    onExpand,
    onDrop,
    allowDrop,
    onDelete,
  } = props;

  const renderTreeNode = (treeNode: ClassTreeDataNode) => {
    const node = treeNode.rawNode;
    const isRoot = node.pid === 0;
    const childCount = node.children?.length || 0;
    const expanded = expandedKeys.includes(treeNode.key);

    return (
      <Flex className={styles.treeNode} gap={12} justify="space-between" align="center">
        <Flex vertical gap={4} className={styles.treeNodeMain}>
          <Space size={[8, 4]} wrap>
            <Typography.Text strong>{node.name}</Typography.Text>
            <Tag color={isRoot ? "gold" : "blue"}>{isRoot ? "一级分类" : "二级分类"}</Tag>
            {isRoot && childCount > 0 ? <Tag color="processing">{expanded ? `已展开 ${childCount}` : `已折叠 ${childCount}`}</Tag> : null}
          </Space>
          <Space size={[12, 4]} wrap>
            <Typography.Text type="secondary">ID {node.id}</Typography.Text>
            <Typography.Text type="secondary">排序 {node.sort || 0}</Typography.Text>
            <Typography.Text type="secondary">{isRoot ? `子分类 ${childCount}` : `父级 ${node.pid}`}</Typography.Text>
          </Space>
        </Flex>
        <Space size={8} wrap onClick={(event) => event.stopPropagation()}>
          <Popconfirm title="确认删除该分类？" okText="删除" cancelText="取消" onConfirm={() => void onDelete(node.id)}>
            <Button size="small" type="text" danger icon={<DeleteOutlined />} />
          </Popconfirm>
        </Space>
      </Flex>
    );
  };

  return (
    <Card
      extra={
        <Space wrap>
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
      }
    >
      <Flex vertical gap={16}>
        <Typography.Text type="secondary">这里只负责分类层级排序和删除草稿。拖拽只允许同层级重排，不支持跨父级搬移；所有修改点击保存后才会统一提交。</Typography.Text>
        {classTree.length === 0 ? (
          <Empty description="暂无分类数据" />
        ) : (
          <div className={styles.treePanel}>
            <Tree<ClassTreeDataNode>
              blockNode
              draggable
              showLine={{ showLeafIcon: false }}
              selectable={false}
              expandedKeys={expandedKeys}
              treeData={treeData}
              allowDrop={allowDrop}
              onExpand={onExpand}
              onDrop={onDrop}
              titleRender={renderTreeNode}
            />
          </div>
        )}
      </Flex>
    </Card>
  );
}
