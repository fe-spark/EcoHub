"use client";

import React, { Suspense, useCallback, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button, Empty, Space, Spin, Tag } from "antd";
import {
  DesktopOutlined,
  FireOutlined,
  PictureOutlined,
  PlusOutlined,
} from "@ant-design/icons";

import { useAppMessage } from "@/lib/useAppMessage";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import ManagePageHeader from "@/app/manage/components/page-header";
import {
  ContentNonPrimaryAlert,
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
import { useHomeSource } from "./use-home-source";
import { BannerRecord, BannerSourceOption, EditorMode } from "./types";
import styles from "./index.module.less";
import tabStyles from "./home-tabs.module.less";

interface BannersTabViewProps {
  targetSourceId: string;
  primaryId: string;
  primaryLoading: boolean;
  onSourcesSync: (
    sources: BannerSourceOption[] | undefined,
    resolvedSourceId: string,
    primarySourceId?: string,
  ) => void;
  onDirtyChange: (dirty: boolean) => void;
  onBusyChange: (busy: boolean) => void;
  resetSignal: number;
}

function BannersTabView({
  targetSourceId,
  primaryId,
  primaryLoading,
  onSourcesSync,
  onDirtyChange,
  onBusyChange,
  resetSignal,
}: BannersTabViewProps) {
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

  useEffect(() => {
    if (resetSignal > 0) {
      resetModals();
    }
  }, [resetModals, resetSignal]);

  const {
    sourceId,
    banners,
    orderDirty,
    loading,
    orderSaving,
    canWrite,
    refreshAfterWrite,
    handleSortList,
    handleMoveOrder,
    handleSaveOrder,
    handleDelete,
    discardOrder,
    applyPublishedDeck,
  } = useBannerWorkspace({
    targetSourceId,
    primaryId,
    primaryLoading,
    onSourcesSync,
    hasPendingModal: editorVisible || quickDrawerOpen || quickModalOpen,
    onResetModals: resetModals,
  });

  useEffect(() => {
    onDirtyChange(orderDirty);
  }, [onDirtyChange, orderDirty]);

  useEffect(() => {
    onBusyChange(loading || orderSaving);
  }, [loading, onBusyChange, orderSaving]);

  useEffect(() => {
    if (!orderDirty) return;
    const handleBeforeUnload = (e: BeforeUnloadEvent) => {
      e.preventDefault();
      e.returnValue = "";
    };
    window.addEventListener("beforeunload", handleBeforeUnload);
    return () => window.removeEventListener("beforeunload", handleBeforeUnload);
  }, [orderDirty]);

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
  const { message, modal } = useAppMessage();
  const currentTab: HomeManageTab =
    searchParams.get("tab") === "recommends" ? "recommends" : "banners";

  const [bannerDirty, setBannerDirty] = useState(false);
  const [bannerBusy, setBannerBusy] = useState(false);
  const [recommendDirty, setRecommendDirty] = useState(false);
  const [recommendBusy, setRecommendBusy] = useState(false);
  const [resetSignal, setResetSignal] = useState(0);

  const activeDirty = currentTab === "recommends" ? recommendDirty : bannerDirty;
  const activeBusy = currentTab === "recommends" ? recommendBusy : bannerBusy;

  const {
    sources,
    sourceId,
    primaryId,
    primaryName,
    primaryLoading,
    syncSources,
    handleSourceChange,
  } = useHomeSource({
    activeDirty,
    activeBusy,
    onPrimaryFollow: (hadPending) => {
      setResetSignal((n) => n + 1);
      if (hadPending) {
        message.warning("主站已变更，未保存的调整已重置");
      }
    },
  });

  const replaceTabQuery = useCallback(
    (tab: HomeManageTab) => {
      if (tab === currentTab) return;
      const doSwitch = () => {
        setBannerDirty(false);
        setRecommendDirty(false);
        const params = new URLSearchParams(searchParams.toString());
        params.set("tab", tab);
        const qs = params.toString();
        router.replace(qs ? `/manage/banners?${qs}` : "/manage/banners");
      };
      if (!activeDirty) {
        doSwitch();
        return;
      }
      modal.confirm({
        title: "切换标签页？",
        content: "当前调整还没保存，切换后会丢掉这次修改。",
        okText: "切换",
        cancelText: "留下",
        onOk: doSwitch,
      });
    },
    [activeDirty, currentTab, modal, router, searchParams],
  );

  return (
    <div className={tabStyles.page}>
      <ManagePageHeader
        className={tabStyles.pageHeader}
        title="首页管理"
        description="统一管理首页轮播与首页推荐板块内容。"
        actions={
          <ContentSourceSelect
            sources={sources}
            sourceId={sourceId}
            primaryId={primaryId}
            disabled={activeBusy}
            onChange={handleSourceChange}
          />
        }
      />

      <ContentNonPrimaryAlert
        sources={sources}
        sourceId={sourceId}
        primaryId={primaryId}
        primaryName={primaryName}
        scopeLabel="首页轮播与推荐"
        disabled={activeBusy}
        onSwitchToPrimary={handleSourceChange}
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
          <RecommendManager
            sourceId={sourceId}
            primaryLoading={primaryLoading}
            onSourcesSync={syncSources}
            onDirtyChange={setRecommendDirty}
            onBusyChange={setRecommendBusy}
          />
        ) : (
          <div className={tabStyles.tabPaneScrollable}>
            <BannersTabView
              targetSourceId={sourceId}
              primaryId={primaryId}
              primaryLoading={primaryLoading}
              onSourcesSync={syncSources}
              onDirtyChange={setBannerDirty}
              onBusyChange={setBannerBusy}
              resetSignal={resetSignal}
            />
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
