"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { Button, Space, Spin, Empty, Tooltip } from "antd";
import { DesktopOutlined, SettingOutlined } from "@ant-design/icons";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import BannerDrawer from "./banner-drawer";
import BannerQuickDrawer from "./banner-quick-drawer";
import BannerQuickModal from "./banner-quick-modal";
import BannerConfigDrawer from "./banner-config-drawer";
import BannerGrid from "./banner-grid";
import { QuickStrategy } from "./banner-quick-config";
import { useBannerDeck } from "./use-banner-deck";
import { BannerRecord, EditorMode } from "./types";
import styles from "./index.module.less";

export default function BannersPageView() {
  const tmdbEnabled = useTmdbEnabled();
  const {
    banners,
    bannersRef,
    orderDirty,
    setBanners,
    replaceList,
    absorbServerList,
    discardOrder,
  } = useBannerDeck();
  const [loading, setLoading] = useState(false);
  const [orderSaving, setOrderSaving] = useState(false);
  const [quickModalOpen, setQuickModalOpen] = useState(false);
  const [configDrawerOpen, setConfigDrawerOpen] = useState(false);
  const [quickDrawerOpen, setQuickDrawerOpen] = useState(false);
  const [quickDeck, setQuickDeck] = useState<BannerRecord[]>([]);
  const [quickStrategy, setQuickStrategy] = useState<QuickStrategy>("hot");

  // 手动添加/编辑弹窗状态
  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);

  const { message } = useAppMessage();
  const messageRef = useRef(message);
  messageRef.current = message;
  const { canWrite } = useManagePermission();

  const fetchBanners = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/banner/list");
      if (resp.code === 0) {
        absorbServerList((resp.data || []) as BannerRecord[]);
      } else {
        messageRef.current.error(resp.msg || "获取轮播列表失败");
      }
    } catch {
      messageRef.current.error("网络异常，获取轮播列表失败");
    } finally {
      setLoading(false);
    }
  }, [absorbServerList]);

  useEffect(() => {
    void fetchBanners();
  }, [fetchBanners]);

  // 拖拽只改本地顺序，保存走底部条
  const handleSortList = useCallback(
    (nextList: BannerRecord[]) => {
      setBanners(nextList);
    },
    [setBanners],
  );

  const handleMoveOrder = useCallback(
    (index: number, direction: "up" | "down") => {
      const current = bannersRef.current;
      const targetIndex = direction === "up" ? index - 1 : index + 1;
      if (targetIndex < 0 || targetIndex >= current.length) return;
      const updated = [...current];
      const temp = updated[index];
      updated[index] = updated[targetIndex];
      updated[targetIndex] = temp;
      setBanners(updated);
    },
    [bannersRef, setBanners],
  );

  const handleSaveOrder = useCallback(async () => {
    if (!canWrite) return;
    const draft = bannersRef.current;
    setOrderSaving(true);
    try {
      const resp = await ApiPost("/manage/banner/deck", draft);
      if (resp.code === 0) {
        const next = Array.isArray(resp.data) ? (resp.data as BannerRecord[]) : draft;
        replaceList(next);
        message.success(resp.msg || "排片顺序已保存");
        return;
      }
      message.error(resp.msg || "排序保存失败");
    } catch {
      message.error("网络异常，排序保存失败");
    } finally {
      setOrderSaving(false);
    }
  }, [bannersRef, canWrite, message, replaceList]);

  // 删除某一项轮播
  const handleDelete = useCallback(
    async (id: string) => {
      try {
        const resp = await ApiPost("/manage/banner/delete", { ids: [String(id)] });
        if (resp.code === 0) {
          message.success("删除成功");
          void fetchBanners();
        } else {
          message.error(resp.msg || "删除失败");
        }
      } catch {
        message.error("网络异常，删除失败");
      }
    },
    [message, fetchBanners],
  );

  const openCreateEditor = () => {
    setCurrentRow(null);
    setEditorMode("create");
    setEditorVisible(true);
  };

  const openEditEditor = useCallback((record: BannerRecord) => {
    setCurrentRow(record);
    setEditorMode("edit");
    setEditorVisible(true);
  }, []);

  const openQuickModal = () => setQuickModalOpen(true);

  return (
    <div className={`${styles.pageStack} ${orderDirty ? styles.pageStackDirty : ""}`}>
      <ManagePageHeader title="首页轮播" />

      {/* 顶部操作工具栏：配置使用齿轮图标按钮，独立放置；其他操作保持文字按钮 */}
      <div className={styles.toolbarCard}>
        <div className={styles.toolbar}>
          {/* 左侧：标题与统计徽标 */}
          <div className={styles.toolbarLeft}>
            <DesktopOutlined style={{ color: "var(--ant-color-primary)", fontSize: 16 }} />
            <span className={styles.toolbarTitle}>首页轮播</span>
            <span className={styles.countBadge}>{banners.length}</span>
          </div>

          {/* 右侧：只放配置 + 一键排片 */}
          <Space size={8} wrap className={styles.toolbarActions}>
            <Tooltip title="排片配置">
              <Button
                size="middle"
                icon={<SettingOutlined />}
                disabled={!canWrite}
                onClick={() => setConfigDrawerOpen(true)}
              />
            </Tooltip>

            <Button
              type="primary"
              size="middle"
              disabled={!canWrite}
              onClick={openQuickModal}
            >
              一键排片
            </Button>
          </Space>
        </div>
      </div>

      {/* 轮播卡片列表展示 */}
      {loading && banners.length === 0 ? (
        <div className={styles.emptyCard}>
          <Spin description="加载中..." />
        </div>
      ) : banners.length === 0 ? (
        <div className={styles.emptyCard}>
          <Empty
            description="暂无排片内容"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          >
            <Button
              type="primary"
              disabled={!canWrite}
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

      {/* 纯净一键排片进度执行器 */}
      <BannerQuickModal
        open={quickModalOpen}
        onClose={() => setQuickModalOpen(false)}
        onOpenConfig={() => setConfigDrawerOpen(true)}
        onGenerated={(deck, strategy) => {
          setQuickDeck(deck);
          setQuickStrategy(strategy);
          setQuickModalOpen(false);
          setQuickDrawerOpen(true);
        }}
      />

      {/* 独立的排片配置抽屉 */}
      <BannerConfigDrawer
        open={configDrawerOpen}
        onClose={() => setConfigDrawerOpen(false)}
      />

      {/* 排片结果抽屉 */}
      <BannerQuickDrawer
        open={quickDrawerOpen}
        deck={quickDeck}
        strategy={quickStrategy}
        tmdbEnabled={tmdbEnabled}
        onClose={() => {
          setQuickDrawerOpen(false);
          setQuickDeck([]);
        }}
        onRedo={() => {
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickModalOpen(true);
        }}
        onChange={setQuickDeck}
        onSuccess={(next) => {
          replaceList(next);
          setQuickDrawerOpen(false);
          void fetchBanners();
        }}
      />

      {/* 手动添加/编辑轮播抽屉 */}
      <BannerDrawer
        open={editorVisible}
        mode={editorMode}
        currentRow={currentRow}
        tmdbEnabled={tmdbEnabled}
        onClose={() => setEditorVisible(false)}
        onSuccess={fetchBanners}
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
