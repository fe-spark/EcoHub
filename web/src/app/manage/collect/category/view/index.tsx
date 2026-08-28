"use client";

import { useEffect, useState } from "react";
import { Card, Descriptions, Modal, Tag, Typography } from "antd";
import ManagePageHeader from "@/app/manage/components/page-header";
import CategoryTreeCard from "./category-tree-card";
import { useCategoryTreeState } from "./use-category-tree-state";
import styles from "./index.module.less";

export default function CategoryWorkspacePageView() {
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false);
  const treeState = useCategoryTreeState();
  const { fetchFilmClassTree } = treeState;

  useEffect(() => {
    void fetchFilmClassTree();
  }, [fetchFilmClassTree]);

  const handleResetConfirm = async () => {
    const resetDone = await treeState.resetTree();
    if (resetDone) {
      setResetConfirmOpen(false);
    }
  };

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader title="分类管理" description="维护当前主采集站分类框架、排序与显示状态；分类不允许删除，只能隐藏或显示。" />

      <Card className={styles.panelCard} styles={{ body: { padding: "14px 16px" } }}>
        <Descriptions size="small" column={{ xs: 1, sm: 3, md: 3 }}>
          <Descriptions.Item label="分类节点总数">
            <Typography.Text strong>{treeState.stats.total}</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="主类 / 子类结构">
            <Typography.Text strong>{treeState.stats.roots}</Typography.Text> 主类 / <Typography.Text type="secondary">{treeState.stats.children} 子类</Typography.Text>
          </Descriptions.Item>
          <Descriptions.Item label="隐藏分类状态">
            <Tag color={treeState.stats.hidden === 0 ? "success" : "warning"} variant="filled">
              {treeState.stats.hidden === 0 ? "全部显示 (正常)" : `已隐藏 ${treeState.stats.hidden} 个`}
            </Tag>
          </Descriptions.Item>
        </Descriptions>
      </Card>

      <div className={styles.workspace}>
        <CategoryTreeCard
          classTree={treeState.classTree}
          expandedKeys={treeState.expandedKeys}
          loadingTree={treeState.loadingTree}
          savingTree={treeState.savingTree}
          resettingTree={treeState.resettingTree}
          updatingShowIds={treeState.updatingShowIds}
          hasPendingChanges={treeState.hasPendingChanges}
          onRefresh={() => void treeState.fetchFilmClassTree()}
          onReset={() => setResetConfirmOpen(true)}
          onSave={() => void treeState.saveTree()}
          onExpand={(keys) => treeState.setExpandedKeys(keys)}
          onMove={treeState.moveClassWithinSameParent}
          onShowChange={(id, show) => void treeState.updateClassVisibility(id, show)}
        />
      </div>

      <Modal
        title="确认重置分类？"
        open={resetConfirmOpen}
        width={560}
        okText="确认重置"
        cancelText="取消"
        confirmLoading={treeState.resettingTree}
        onOk={() => void handleResetConfirm()}
        onCancel={() => setResetConfirmOpen(false)}
      >
        该操作会清空当前分类框架，并重新获取主采集站原始分类；分类规则会重新生成展示分类与来源映射，不会重写历史影片。
      </Modal>
    </div>
  );
}
