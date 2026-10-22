"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { Button, Space, Spin, Empty } from "antd";
import {
  PlusOutlined,
  DesktopOutlined,
  CompassOutlined,
} from "@ant-design/icons";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import BannerDrawer from "./banner-drawer";
import BannerQuickDrawer from "./banner-quick-drawer";
import BannerQuickModal from "./banner-quick-modal";
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

      {/* 顶部操作工具栏卡片 */}
      <div className={styles.toolbarCard}>
        <div className={styles.toolbar}>
          <div className={styles.toolbarLeft}>
            <Space size={10} align="center" wrap>
              <DesktopOutlined style={{ color: "var(--ant-color-primary)", fontSize: 16 }} />
              <span className={styles.toolbarTitle}>首页轮播卡组</span>
              <span className={styles.countBadge}>{banners.length}</span>
            </Space>
          </div>

          <Space size={8} wrap className={styles.toolbarActions}>
            <Button
              type="primary"
              size="middle"
              icon={<CompassOutlined />}
              disabled={!canWrite}
              onClick={openQuickModal}
              style={{ background: "#0958d9", borderColor: "#0958d9" }}
            >
              一键排片
            </Button>

            <Button
              type="primary"
              size="middle"
              icon={<PlusOutlined />}
              onClick={openCreateEditor}
              disabled={!canWrite}
            >
              添加影片
            </Button>
          </Space>
        </div>
      </div>

      {/* 轮播卡片列表展示 */}
      {loading && banners.length === 0 ? (
        <div className={styles.emptyCard}>
          <Spin description="加载轮播数据中..." />
        </div>
      ) : banners.length === 0 ? (
        <div className={styles.emptyCard}>
          <Empty
            description="当前暂无首页排片影片，支持一键排片或手动添加"
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          >
            <Space size={12}>
              <Button
                type="primary"
                icon={<CompassOutlined />}
                disabled={!canWrite}
                onClick={openQuickModal}
              >
                一键排片
              </Button>
              <Button
                icon={<PlusOutlined />}
                disabled={!canWrite}
                onClick={openCreateEditor}
              >
                添加影片
              </Button>
            </Space>
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

      <BannerQuickModal
        open={quickModalOpen}
        tmdbEnabled={tmdbEnabled}
        onClose={() => setQuickModalOpen(false)}
        onGenerated={(deck, strategy) => {
          setQuickDeck(deck);
          setQuickStrategy(strategy);
          setQuickModalOpen(false);
          setQuickDrawerOpen(true);
        }}
      />

      {/* 排片结果抽屉：顺序在抽屉内直接调整，确认后才替换线上 */}
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

      {/* 手动添加/编辑轮播弹窗（在详情中完整展示横屏大图与封面图） */}
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
