"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button, Empty, Space, Spin, Tag } from "antd";
import { DesktopOutlined, PlusOutlined } from "@ant-design/icons";

import { useAppMessage } from "@/lib/useAppMessage";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import {
  ContentPageShell,
  ContentSectionCard,
  ContentSourceSelect,
} from "@/app/manage/components/content-shell";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import BannerDrawer from "./banner-drawer";
import BannerQuickDrawer from "./banner-quick-drawer";
import BannerQuickModal from "./banner-quick-modal";
import BannerGrid from "./banner-grid";
import { QuickConfigMemo, QuickStrategy } from "./banner-quick-config";
import { useBannerWorkspace } from "./use-banner-workspace";
import { BannerRecord, EditorMode } from "./types";
import styles from "./index.module.less";

export default function BannersPageView() {
  const tmdbEnabled = useTmdbEnabled();
  const { message, modal } = useAppMessage();
  const messageRef = useRef(message);
  useEffect(() => {
    messageRef.current = message;
  }, [message]);

  const [quickModalOpen, setQuickModalOpen] = useState(false);
  const [quickDrawerOpen, setQuickDrawerOpen] = useState(false);
  const [quickConfigMap, setQuickConfigMap] = useState<Record<string, QuickConfigMemo>>({});
  const [quickDeck, setQuickDeck] = useState<BannerRecord[]>([]);
  const [quickRevision, setQuickRevision] = useState("");
  const [quickSourceId, setQuickSourceId] = useState("");
  const [quickStrategy, setQuickStrategy] = useState<QuickStrategy>("hot");

  // 手动添加/编辑弹窗状态
  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);
  const [editorSourceId, setEditorSourceId] = useState("");

  const resetModals = useCallback((warnMessage?: string) => {
    if (warnMessage) {
      messageRef.current.warning(warnMessage);
    }
    setQuickModalOpen(false);
    setQuickDrawerOpen(false);
    setQuickDeck([]);
    setQuickSourceId("");
    setEditorVisible(false);
    setCurrentRow(null);
  }, []);

  const {
    sources,
    sourceId,
    primaryId,
    banners,
    orderDirty,
    loading,
    orderSaving,
    canWrite,
    refreshAfterWrite,
    selectSource,
    handleSortList,
    handleMoveOrder,
    handleSaveOrder,
    handleDelete,
    discardOrder,
    applyPublishedDeck,
  } = useBannerWorkspace({
    hasPendingModal: editorVisible || quickDrawerOpen || quickModalOpen,
    onResetModals: resetModals,
  });

  const handleSourceChange = (nextSourceId: string) => {
    if (!nextSourceId || nextSourceId === sourceId || orderSaving) return;
    if (!orderDirty) {
      void selectSource(nextSourceId);
      return;
    }
    modal.confirm({
      title: "切换采集源？",
      content: "当前排片顺序还没保存，继续后会丢掉这次调整。",
      okText: "切换",
      cancelText: "留下",
      onOk: () => selectSource(nextSourceId),
    });
  };

  const openCreateEditor = () => {
    setCurrentRow(null);
    setEditorMode("create");
    setEditorSourceId(sourceId);
    setEditorVisible(true);
  };

  const openEditEditor = useCallback(
    (record: BannerRecord) => {
      setCurrentRow(record);
      setEditorMode("edit");
      setEditorSourceId(sourceId);
      setEditorVisible(true);
    },
    [sourceId],
  );

  const isCurrentPrimary = Boolean(sourceId && primaryId && sourceId === primaryId);

  return (
    <ContentPageShell
      title="首页轮播"
      description="按采集站安排首页轮播展示的影片。"
      headerActions={
        <ContentSourceSelect
          sources={sources}
          sourceId={sourceId}
          primaryId={primaryId}
          disabled={loading || orderSaving}
          onChange={handleSourceChange}
        />
      }
      dirty={orderDirty}
    >
      <ContentSectionCard
        icon={<DesktopOutlined />}
        title="已排轮播"
        count={banners.length}
        extra={
          isCurrentPrimary ? (
            <Tag color="gold" style={{ margin: 0 }}>
              当前前台展示
            </Tag>
          ) : null
        }
        actions={
          <Space size={8} wrap>
            <Button
              icon={<PlusOutlined />}
              disabled={!canWrite || !sourceId}
              onClick={openCreateEditor}
            >
              添加影片
            </Button>
            <Button
              type="primary"
              disabled={!canWrite || !sourceId}
              onClick={() => {
                if (!orderDirty) {
                  setQuickModalOpen(true);
                  return;
                }
                modal.confirm({
                  title: "放弃未保存的排序？",
                  content: "当前排片顺序尚未保存，发布一键排片将覆盖它。",
                  okText: "继续",
                  cancelText: "返回",
                  onOk: () => setQuickModalOpen(true),
                });
              }}
            >
              一键排片
            </Button>
          </Space>
        }
        bodyPadding
      >
        {loading && banners.length === 0 ? (
          <div className={styles.emptyCard}>
            <Spin description="加载中..." />
          </div>
        ) : banners.length === 0 ? (
          <div className={styles.emptyCard}>
            <Empty
              description={sourceId ? "当前采集站暂无轮播" : "暂无可用采集站"}
              image={Empty.PRESENTED_IMAGE_SIMPLE}
            >
              <Button
                type="primary"
                disabled={!canWrite || !sourceId}
                onClick={openCreateEditor}
              >
                添加影片
              </Button>
            </Empty>
          </div>
        ) : (
          <BannerGrid
            banners={banners}
            canWrite={canWrite && !orderSaving}
            onSortList={handleSortList}
            onEdit={openEditEditor}
            onDelete={handleDelete}
            onMoveOrder={handleMoveOrder}
            onOpenCreate={openCreateEditor}
          />
        )}
      </ContentSectionCard>

      {/* 一键排片：每次先确认配置，再执行 */}
      <BannerQuickModal
        open={quickModalOpen}
        sourceId={sourceId}
        lastConfig={sourceId ? quickConfigMap[sourceId] : undefined}
        onRemember={(config) => {
          if (!sourceId) return;
          setQuickConfigMap((prev) => ({ ...prev, [sourceId]: config }));
        }}
        onClose={() => setQuickModalOpen(false)}
        onGenerated={(result, strategy) => {
          const targetSource = result.sourceId || sourceId;
          if (targetSource !== sourceId) return;
          setQuickDeck(result.items);
          setQuickRevision(result.revision);
          setQuickSourceId(targetSource);
          setQuickStrategy(strategy);
          setQuickModalOpen(false);
          setQuickDrawerOpen(true);
        }}
      />

      {/* 排片结果抽屉 */}
      <BannerQuickDrawer
        open={quickDrawerOpen}
        sourceId={quickSourceId}
        revision={quickRevision}
        deck={quickDeck}
        strategy={quickStrategy}
        tmdbEnabled={tmdbEnabled}
        onClose={() => {
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickSourceId("");
        }}
        onRedo={() => {
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickSourceId("");
          setQuickModalOpen(true);
        }}
        onChange={setQuickDeck}
        onSuccess={(view) => {
          if (view.sourceId && view.sourceId !== sourceId) return;
          applyPublishedDeck(view);
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickSourceId("");
        }}
      />

      {/* 手动添加/编辑轮播抽屉 */}
      <BannerDrawer
        open={editorVisible}
        mode={editorMode}
        sourceId={editorSourceId}
        isPrimary={Boolean(editorSourceId && primaryId && editorSourceId === primaryId)}
        currentRow={currentRow}
        tmdbEnabled={tmdbEnabled}
        onClose={() => setEditorVisible(false)}
        onSuccess={() => refreshAfterWrite(editorSourceId || sourceId, primaryId)}
      />

      <UnsavedChangesBar
        visible={orderDirty && canWrite}
        saving={orderSaving}
        message="排片顺序未保存"
        onDiscard={discardOrder}
        onSave={() => void handleSaveOrder()}
      />
    </ContentPageShell>
  );
}
