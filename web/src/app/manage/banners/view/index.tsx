"use client";

import React, { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { Button, Empty, Space, Spin, Tag } from "antd";
import { DesktopOutlined, FireOutlined, PictureOutlined, PlusOutlined } from "@ant-design/icons";
import { useRouter, useSearchParams } from "next/navigation";

import ManagePageHeader from "@/app/manage/components/page-header";

import { useAppMessage } from "@/lib/useAppMessage";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import {
  ContentSectionCard,
  ContentSourceSelect,
} from "@/app/manage/components/content-shell";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import BannerDrawer from "./banner-drawer";
import BannerQuickDrawer from "./banner-quick-drawer";
import BannerQuickModal from "./banner-quick-modal";
import BannerGrid from "./banner-grid";
import RecommendManager from "./recommend-manager";
import { QuickConfigMemo, QuickStrategy } from "./banner-quick-config";
import { useBannerWorkspace } from "./use-banner-workspace";
import { BannerRecord, EditorMode } from "./types";
import styles from "./index.module.less";
import tabStyles from "./home-tabs.module.less";

function BannersTabView() {
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
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", flexWrap: "wrap", gap: 12, marginBottom: 16 }}>
        <ContentSourceSelect
          sources={sources}
          sourceId={sourceId}
          primaryId={primaryId}
          disabled={loading || orderSaving}
          onChange={handleSourceChange}
        />
      </div>
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
              onClick={() => setQuickModalOpen(true)}
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
    </div>
  );
}

type HomeManageTab = "banners" | "recommends";

const HOME_MANAGE_TABS: { key: HomeManageTab; label: string; icon: React.ReactNode }[] = [
  { key: "banners", label: "首页轮播", icon: <PictureOutlined /> },
  { key: "recommends", label: "首页推荐", icon: <FireOutlined /> },
];

function HomeManageBody() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const currentTab: HomeManageTab =
    searchParams.get("tab") === "recommends" ? "recommends" : "banners";

  const replaceTabQuery = useCallback(
    (tab: HomeManageTab) => {
      const params = new URLSearchParams(searchParams.toString());
      params.set("tab", tab);
      const qs = params.toString();
      router.replace(qs ? `/manage/banners?${qs}` : "/manage/banners");
    },
    [router, searchParams],
  );

  return (
    <div className={tabStyles.page}>
      <ManagePageHeader
        className={tabStyles.pageHeader}
        title="首页管理"
        description="统一管理首页轮播与首页推荐板块内容。"
      />

      <div className={tabStyles.tabBar} role="tablist" aria-label="首页管理分类">
        {HOME_MANAGE_TABS.map((tab) => {
          const active = currentTab === tab.key;
          return (
            <button
              key={tab.key}
              type="button"
              role="tab"
              aria-selected={active}
              className={`${tabStyles.tabItem} ${active ? tabStyles.tabItemActive : ""}`}
              onClick={() => replaceTabQuery(tab.key)}
            >
              <span className={tabStyles.tabIcon}>{tab.icon}</span>
              <span>{tab.label}</span>
            </button>
          );
        })}
      </div>

      <div className={tabStyles.tabContent} role="tabpanel">
        {currentTab === "recommends" ? (
          <RecommendManager />
        ) : (
          <div className={tabStyles.tabPaneScrollable}>
            <BannersTabView />
          </div>
        )}
      </div>
    </div>
  );
}

export default function BannersPageView() {
  return (
    <Suspense fallback={null}>
      <HomeManageBody />
    </Suspense>
  );
}
