"use client";

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
        description="调整分类的排序，以及是否在前台显示。"
      />

      <CategoryTreeCard
        sources={treeState.sources}
        sourceId={treeState.sourceId}
        primaryId={treeState.primaryId}
        classTree={treeState.classTree}
        expandedKeys={treeState.expandedKeys}
        loadingTree={treeState.loadingTree}
        savingTree={treeState.savingTree}
        stats={treeState.stats}
        onSourceChange={(nextSourceId) => {
          if (nextSourceId === treeState.sourceId) return;
          runUnlessDirty(
            () => void treeState.selectSource(nextSourceId),
            "切换采集源？",
            "切换",
          );
        }}
        onRefresh={() =>
          runUnlessDirty(
            () => void treeState.fetchFilmClassTree(true),
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
