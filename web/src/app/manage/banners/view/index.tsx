"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import axios from "axios";
import { Button, Space, Spin, Empty, Tag } from "antd";
import { DesktopOutlined } from "@ant-design/icons";

import { API_CONFLICT, API_PRIMARY_MISMATCH, ApiDelete, ApiGet, ApiPut } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import BannerDrawer from "./banner-drawer";
import BannerQuickDrawer from "./banner-quick-drawer";
import BannerQuickModal from "./banner-quick-modal";
import BannerGrid from "./banner-grid";
import { QuickConfigMemo, QuickStrategy } from "./banner-quick-config";
import { useBannerDeck } from "./use-banner-deck";
import { BannerListView, BannerRecord, EditorMode } from "./types";
import styles from "./index.module.less";

export default function BannersPageView() {
  const { primary, refresh: refreshPrimary } = usePrimarySource();
  const primaryId = primary?.id || "";
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
  const [quickConfig, setQuickConfig] = useState<QuickConfigMemo>();
  const [quickDeck, setQuickDeck] = useState<BannerRecord[]>([]);
  const [quickRevision, setQuickRevision] = useState("");
  const [quickPrimaryId, setQuickPrimaryId] = useState("");
  const revisionRef = useRef("");
  const [quickStrategy, setQuickStrategy] = useState<QuickStrategy>("hot");

  // 手动添加/编辑弹窗状态
  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);
  const [editorPrimaryId, setEditorPrimaryId] = useState("");

  const { message } = useAppMessage();
  const messageRef = useRef(message);
  messageRef.current = message;
  const { canWrite } = useManagePermission();
  const previousPrimaryIdRef = useRef(primaryId);
  const pendingEditRef = useRef(false);
  pendingEditRef.current = orderDirty || editorVisible || quickDrawerOpen || quickModalOpen;

  // 主站变化或再次刷新时中止上一个列表请求。
  const listAbortRef = useRef<AbortController | null>(null);
  useEffect(() => {
    setQuickConfig(undefined);
  }, [primaryId]);

  const fetchBanners = useCallback(async () => {
    if (!primaryId) {
      replaceList([]);
      revisionRef.current = "";
      setLoading(false);
      return;
    }
    listAbortRef.current?.abort();
    const controller = new AbortController();
    listAbortRef.current = controller;
    setLoading(true);
    try {
      const resp = await ApiGet<BannerListView>(
        "/manage/banners",
        undefined,
        undefined,
        { signal: controller.signal },
      );
      if (controller.signal.aborted) return;
      if (resp.code === 0 && resp.data) {
        if (resp.data.primarySourceId && resp.data.primarySourceId !== primaryId) {
          replaceList([]);
          revisionRef.current = "";
          void refreshPrimary();
          return;
        }
        revisionRef.current = resp.data.revision;
        absorbServerList(resp.data.items || []);
      } else {
        messageRef.current.error(resp.msg || "获取轮播列表失败");
      }
    } catch (err) {
      if (controller.signal.aborted || axios.isCancel(err)) return;
      messageRef.current.error("网络异常，获取轮播列表失败");
    } finally {
      if (listAbortRef.current === controller) {
        listAbortRef.current = null;
        setLoading(false);
      }
    }
  }, [absorbServerList, primaryId, refreshPrimary, replaceList]);

  useEffect(() => {
    if (previousPrimaryIdRef.current !== primaryId) {
      if (previousPrimaryIdRef.current && pendingEditRef.current) {
        messageRef.current.warning("主站已变更，未保存的轮播编辑已关闭");
      }
      listAbortRef.current?.abort();
      replaceList([]);
      revisionRef.current = "";
      setQuickModalOpen(false);
      setQuickDrawerOpen(false);
      setQuickDeck([]);
      setQuickPrimaryId("");
      setEditorVisible(false);
      setCurrentRow(null);
      previousPrimaryIdRef.current = primaryId;
    }
    void fetchBanners();
  }, [fetchBanners, primaryId, replaceList]);

  useEffect(() => () => listAbortRef.current?.abort(), []);

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
      const resp = await ApiPut<BannerListView>("/manage/banners/order", {
        sourceId: primaryId,
        revision: revisionRef.current,
        ids: draft.map((item) => item.id),
      });
      if (resp.code === 0 && resp.data) {
        revisionRef.current = resp.data.revision;
        replaceList(resp.data.items || []);
        message.success(resp.msg || "排片顺序已保存");
        return;
      }
      if (resp.code === API_CONFLICT) {
        // 列表已被别处改过：拉最新数据，本地顺序尽量保留，再确认一次。
        message.warning("轮播已被修改，已刷新为最新数据，请确认顺序后再保存");
        void fetchBanners();
        return;
      }
      if (resp.code === API_PRIMARY_MISMATCH) {
        message.warning(resp.msg || "主站已变更，请刷新后重试");
        void refreshPrimary();
        return;
      }
      message.error(resp.msg || "排序保存失败");
    } catch {
      message.error("网络异常，排序保存失败");
    } finally {
      setOrderSaving(false);
    }
  }, [bannersRef, canWrite, message, primaryId, replaceList, refreshPrimary, fetchBanners]);

  // 删除某一项轮播
  const handleDelete = useCallback(
    async (id: string) => {
      try {
        const resp = await ApiDelete(`/manage/banners/${encodeURIComponent(String(id))}`);
        if (resp.code === 0) {
          message.success("删除成功");
          void fetchBanners();
        } else if (resp.code === API_PRIMARY_MISMATCH) {
          message.warning(resp.msg || "主站已变更，请刷新后重试");
          void refreshPrimary();
        } else {
          message.error(resp.msg || "删除失败");
        }
      } catch {
        message.error("网络异常，删除失败");
      }
    },
    [message, fetchBanners, refreshPrimary],
  );

  const openCreateEditor = () => {
    setCurrentRow(null);
    setEditorMode("create");
    setEditorPrimaryId(primaryId);
    setEditorVisible(true);
  };

  const openEditEditor = useCallback((record: BannerRecord) => {
    setCurrentRow(record);
    setEditorMode("edit");
    setEditorPrimaryId(primaryId);
    setEditorVisible(true);
  }, [primaryId]);

  const openQuickModal = () => setQuickModalOpen(true);

  return (
    <div className={`${styles.pageStack} ${orderDirty ? styles.pageStackDirty : ""}`}>
      <ManagePageHeader title="首页轮播" description="安排首页轮播展示的影片。" />

      {/* 顶部操作工具栏 */}
      <div className={styles.toolbarCard}>
        <div className={styles.toolbar}>
          {/* 左侧：标题与统计徽标 */}
          <div className={styles.toolbarLeft}>
            <DesktopOutlined style={{ color: "var(--ant-color-primary)", fontSize: 16 }} />
            <span className={styles.toolbarTitle}>首页轮播</span>
            <span className={styles.countBadge}>{banners.length}</span>
          </div>

          {/* 右侧：当前主站与一键排片 */}
          <Space size={8} wrap className={styles.toolbarActions}>
            <Tag color={primary ? "gold" : "warning"}>
              {primary ? `主站：${primary.name}` : "未配置主站"}
            </Tag>
            <Button
              type="primary"
              size="middle"
              disabled={!canWrite || !primaryId}
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
            description={primary ? "当前主站暂无轮播" : "未配置主站"}
            image={Empty.PRESENTED_IMAGE_SIMPLE}
          >
            <Button
              type="primary"
              disabled={!canWrite || !primaryId}
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

      {/* 一键排片：每次先确认配置，再执行 */}
      <BannerQuickModal
        open={quickModalOpen}
        lastConfig={quickConfig}
        onRemember={setQuickConfig}
        onClose={() => setQuickModalOpen(false)}
        onGenerated={(result, strategy) => {
          if (result.primarySourceId && result.primarySourceId !== primaryId) {
            void refreshPrimary();
            return;
          }
          setQuickDeck(result.items);
          setQuickRevision(result.revision);
          setQuickPrimaryId(result.primarySourceId || primaryId);
          setQuickStrategy(strategy);
          setQuickModalOpen(false);
          setQuickDrawerOpen(true);
        }}
      />

      {/* 排片结果抽屉 */}
      <BannerQuickDrawer
        open={quickDrawerOpen}
        primarySourceId={quickPrimaryId}
        revision={quickRevision}
        deck={quickDeck}
        strategy={quickStrategy}
        tmdbEnabled={tmdbEnabled}
        onClose={() => {
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickPrimaryId("");
        }}
        onRedo={() => {
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickPrimaryId("");
          setQuickModalOpen(true);
        }}
        onChange={setQuickDeck}
        onSuccess={(view) => {
          if (view.primarySourceId && view.primarySourceId !== primaryId) {
            void refreshPrimary();
            return;
          }
          revisionRef.current = view.revision;
          replaceList(view.items || []);
          setQuickDrawerOpen(false);
          setQuickDeck([]);
          setQuickPrimaryId("");
        }}
      />

      {/* 手动添加/编辑轮播抽屉 */}
      <BannerDrawer
        open={editorVisible}
        mode={editorMode}
        primarySourceId={editorPrimaryId}
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
