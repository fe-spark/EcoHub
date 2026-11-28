"use client";

import { Button } from "antd";
import { ApartmentOutlined, ReloadOutlined } from "@ant-design/icons";
import {
  ContentNonPrimaryAlert,
  ContentPageShell,
  ContentSectionCard,
  ContentSourceSelect,
} from "@/app/manage/components/content-shell";
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

  const selectSourceWithGuard = (nextSourceId: string) => {
    if (!nextSourceId || nextSourceId === treeState.sourceId) return;
    runUnlessDirty(
      () => void treeState.selectSource(nextSourceId),
      "切换站点？",
      "切换",
    );
  };

  return (
    <ContentPageShell
      title="分类管理"
      description="调整分类的排序，以及是否在前台显示。"
      headerActions={
        <ContentSourceSelect
          sources={treeState.sources}
          sourceId={treeState.sourceId}
          primaryId={treeState.primaryId}
          disabled={treeState.loadingTree || treeState.savingTree}
          onChange={selectSourceWithGuard}
        />
      }
      dirty={canWrite && treeState.hasPendingChanges}
    >
      <ContentNonPrimaryAlert
        sources={treeState.sources}
        sourceId={treeState.sourceId}
        primaryId={treeState.primaryId}
        primaryName={treeState.primaryName}
        scopeLabel="分类排序与显隐"
        disabled={treeState.loadingTree || treeState.savingTree}
        onSwitchToPrimary={selectSourceWithGuard}
      />

      <ContentSectionCard
        icon={<ApartmentOutlined />}
        title="分类结构"
        count={treeState.stats.total}
        extra={
          <div className={styles.stats}>
            <span>
              <span className={styles.statValue}>{treeState.stats.roots}</span> 主类
            </span>
            <span className={styles.statSep} />
            <span>
              <span className={styles.statValue}>{treeState.stats.children}</span> 子类
            </span>
            {treeState.stats.hidden > 0 ? (
              <>
                <span className={styles.statSep} />
                <span className={styles.statWarn}>隐藏 {treeState.stats.hidden}</span>
              </>
            ) : null}
          </div>
        }
        actions={
          <Button
            icon={<ReloadOutlined />}
            onClick={() =>
              runUnlessDirty(
                () => void treeState.fetchFilmClassTree(true),
                "刷新分类？",
                "刷新",
              )
            }
            loading={treeState.loadingTree}
            disabled={treeState.savingTree}
          >
            刷新分类
          </Button>
        }
      >
        <CategoryTreeCard
          classTree={treeState.classTree}
          expandedKeys={treeState.expandedKeys}
          loadingTree={treeState.loadingTree}
          savingTree={treeState.savingTree}
          onExpand={(keys) => treeState.setExpandedKeys(keys)}
          onMove={treeState.moveClassWithinSameParent}
          onShowChange={(id, show) => void treeState.updateClassVisibility(id, show)}
        />
      </ContentSectionCard>

      <UnsavedChangesBar
        visible={canWrite && treeState.hasPendingChanges}
        saving={treeState.savingTree}
        onDiscard={treeState.discardTree}
        onSave={() => void treeState.saveTree()}
      />
    </ContentPageShell>
  );
}
