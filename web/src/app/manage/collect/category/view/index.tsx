"use client";

import { useEffect } from "react";
import { Modal } from "antd";
import ManagePageHeader from "@/app/manage/components/page-header";
import CategoryTreeCard from "./category-tree-card";
import { useCategoryTreeState } from "./use-category-tree-state";
import styles from "./index.module.less";

export default function CategoryWorkspacePageView() {
  const treeState = useCategoryTreeState();
  const { fetchFilmClassTree } = treeState;

  useEffect(() => {
    void fetchFilmClassTree();
  }, [fetchFilmClassTree]);

  const handleSourceChange = (value: string) => {
    if (treeState.hasPendingChanges) {
      Modal.confirm({
        title: "切换采集源？",
        content: "当前排序还没保存，切换后会丢掉这次调整。",
        okText: "切换",
        cancelText: "留下",
        onOk: () => treeState.fetchFilmClassTree(value),
      });
      return;
    }
    void treeState.fetchFilmClassTree(value);
  };

  return (
    <div className={styles.pageBody}>
      <ManagePageHeader
        className={styles.pageHeader}
        title="分类管理"
        description="按采集源查看该站分类，调整排序和显示。分类不能删除，只能隐藏或显示。"
      />

      <CategoryTreeCard
        classTree={treeState.classTree}
        expandedKeys={treeState.expandedKeys}
        loadingTree={treeState.loadingTree}
        savingTree={treeState.savingTree}
        updatingShowIds={treeState.updatingShowIds}
        hasPendingChanges={treeState.hasPendingChanges}
        sources={treeState.sources}
        sourceId={treeState.sourceId}
        stats={treeState.stats}
        onSourceChange={handleSourceChange}
        onRefresh={() => void treeState.fetchFilmClassTree(treeState.sourceId)}
        onSave={() => void treeState.saveTree()}
        onExpand={(keys) => treeState.setExpandedKeys(keys)}
        onMove={treeState.moveClassWithinSameParent}
        onShowChange={(id, show) => void treeState.updateClassVisibility(id, show)}
      />
    </div>
  );
}
