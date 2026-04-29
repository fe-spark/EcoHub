"use client";

import { useEffect, useState } from "react";
import { Card, Descriptions, Modal, Tag } from "antd";
import ManagePageHeader from "@/app/manage/components/page-header";
import CategoryTreeCard from "./category-tree-card";
import { useCategoryTreeState } from "./use-category-tree-state";
import styles from "./index.module.less";

export default function CategoryWorkspacePageView() {
  const [resetConfirmOpen, setResetConfirmOpen] = useState(false);
  const treeState = useCategoryTreeState();
  const { fetchFilmClassTree } = treeState;
  const hiddenStatus = treeState.stats.hidden === 0 ? "正常" : `待检查 ${treeState.stats.hidden}`;

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
      <ManagePageHeader title="分类管理" description="维护当前主站分类框架。分类规则已拆分到独立页面。" />

      <Card size="small">
        <Descriptions size="small" column={{ xs: 1, md: 2, xl: 4 }}>
          <Descriptions.Item label="分类节点">{treeState.stats.total}</Descriptions.Item>
          <Descriptions.Item label="一级 / 二级">{treeState.stats.roots} / {treeState.stats.children}</Descriptions.Item>
          <Descriptions.Item label="隐藏分类">
            <Tag color={treeState.stats.hidden === 0 ? "success" : "warning"}>{hiddenStatus}</Tag>
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
          hasPendingChanges={treeState.hasPendingChanges}
          onRefresh={() => void treeState.fetchFilmClassTree()}
          onReset={() => setResetConfirmOpen(true)}
          onSave={() => void treeState.saveTree()}
          onExpand={(keys) => treeState.setExpandedKeys(keys)}
          onMove={treeState.moveClassWithinSameParent}
          onDelete={treeState.queueDeleteClass}
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
        该操作会清空当前分类框架，并重新获取主站原始分类；新分类与规则只会影响下一轮主站采集。
      </Modal>
    </div>
  );
}
