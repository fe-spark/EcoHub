"use client";

import { useEffect } from "react";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { useManagePermission } from "@/lib/manage-permission";
import { useAppMessage } from "@/lib/useAppMessage";
import CategoryTreeCard from "./category-tree-card";
import { useCategoryTreeState } from "./use-category-tree-state";
import styles from "./index.module.less";

export default function CategoryWorkspacePageView() {
  const treeState = useCategoryTreeState();
  const { canWrite } = useManagePermission();
  const { modal } = useAppMessage();
  const { fetchFilmClassTree } = treeState;

  useEffect(() => {
    void fetchFilmClassTree();
  }, [fetchFilmClassTree]);

  const runUnlessDirty = (action: () => void, title: string, okText: string) => {
    if (treeState.savingTree) {
      return;
    }
    if (!treeState.hasPendingChanges) {
      action();
      return;
    }
    modal.confirm({
      title,
      content: "当前调整还没保存，继续后会丢掉这次调整。",
      okText,
      cancelText: "留下",
      onOk: action,
    });
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
        sources={treeState.sources}
        sourceId={treeState.sourceId}
        stats={treeState.stats}
        onSourceChange={(value) =>
          runUnlessDirty(() => void treeState.fetchFilmClassTree(value), "切换采集源？", "切换")
        }
        onRefresh={() =>
          runUnlessDirty(
            () => void treeState.fetchFilmClassTree(treeState.sourceId),
            "刷新分类？",
            "刷新",
          )
        }
        onExpand={(keys) => treeState.setExpandedKeys(keys)}
        onMove={treeState.moveClassWithinSameParent}
        onShowChange={(id, show) => void treeState.updateClassVisibility(id, show)}
      />
      <UnsavedChangesBar
        visible={canWrite && treeState.hasPendingChanges}
        saving={treeState.savingTree}
        onDiscard={treeState.discardTree}
        onSave={() => void treeState.saveTree()}
      />
    </div>
  );
}
