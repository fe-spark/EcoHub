"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { Button, Space, Spin, Empty, Select } from "antd";
import { DesktopOutlined } from "@ant-design/icons";

import { API_CONFLICT, ApiDelete, ApiGet, ApiPut } from "@/lib/client-api";
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
import { BannerListView, BannerRecord, EditorMode } from "./types";
import styles from "./index.module.less";

type SiteOption = {
  id: string;
  name: string;
  state: boolean;
};

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
  const [quickRevision, setQuickRevision] = useState("");
  // 轮播按站点分开存；为空时服务端取首选站。
  const [sourceId, setSourceId] = useState("");
  const [sites, setSites] = useState<SiteOption[]>([]);
  const revisionRef = useRef("");
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
      const resp = await ApiGet<BannerListView>("/manage/banners", {
        sourceId: sourceId || undefined,
      });
      if (resp.code === 0 && resp.data) {
        revisionRef.current = resp.data.revision;
        absorbServerList(resp.data.items || []);
        if (!sourceId && resp.data.sourceId) setSourceId(resp.data.sourceId);
      } else {
        messageRef.current.error(resp.msg || "获取轮播列表失败");
      }
    } catch {
      messageRef.current.error("网络异常，获取轮播列表失败");
    } finally {
      setLoading(false);
    }
  }, [absorbServerList, sourceId]);

  useEffect(() => {
    void fetchBanners();
  }, [fetchBanners]);

  useEffect(() => {
    ApiGet("/manage/collect/list")
      .then((resp) => {
        if (resp.code === 0 && Array.isArray(resp.data)) {
          setSites(resp.data as SiteOption[]);
        }
      })
      .catch(() => {});
  }, []);

  const handleSiteChange = (next: string) => {
    if (next === sourceId) return;
    // 换站直接丢弃未保存的顺序。
    replaceList([]);
    revisionRef.current = "";
    setSourceId(next);
  };

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
        sourceId,
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
      message.error(resp.msg || "排序保存失败");
    } catch {
      message.error("网络异常，排序保存失败");
    } finally {
      setOrderSaving(false);
    }
  }, [bannersRef, canWrite, message, replaceList, sourceId, fetchBanners]);

  // 删除某一项轮播
  const handleDelete = useCallback(
    async (id: string) => {
      try {
        const resp = await ApiDelete(`/manage/banners/${encodeURIComponent(String(id))}`);
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

      {/* 顶部操作工具栏 */}
      <div className={styles.toolbarCard}>
        <div className={styles.toolbar}>
          {/* 左侧：标题与统计徽标 */}
          <div className={styles.toolbarLeft}>
            <DesktopOutlined style={{ color: "var(--ant-color-primary)", fontSize: 16 }} />
            <span className={styles.toolbarTitle}>首页轮播</span>
            <span className={styles.countBadge}>{banners.length}</span>
          </div>

          {/* 右侧：采集源与一键排片 */}
          <Space size={8} wrap className={styles.toolbarActions}>
            <div className={styles.sourceField}>
              <span className={styles.sourceLabel}>采集源</span>
              <Select
                placeholder="选择采集源"
                className={styles.sourceSelect}
                value={sourceId || undefined}
                disabled={orderSaving}
                onChange={handleSiteChange}
                options={sites.map((site) => ({
                  label: site.state === false ? `${site.name} (已停用)` : site.name,
                  value: site.id,
                }))}
                showSearch
                optionFilterProp="label"
                popupMatchSelectWidth={false}
              />
            </div>
            <Button
              type="primary"
              size="middle"
              disabled={!canWrite || !sourceId}
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

      {/* 一键排片：每次先确认配置，再执行 */}
      <BannerQuickModal
        open={quickModalOpen}
        sourceId={sourceId}
        onClose={() => setQuickModalOpen(false)}
        onGenerated={(result, strategy) => {
          setQuickDeck(result.items);
          setQuickRevision(result.revision);
          setQuickStrategy(strategy);
          setQuickModalOpen(false);
          setQuickDrawerOpen(true);
        }}
      />

      {/* 排片结果抽屉 */}
      <BannerQuickDrawer
        open={quickDrawerOpen}
        sourceId={sourceId}
        revision={quickRevision}
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
        onSuccess={(view) => {
          revisionRef.current = view.revision;
          replaceList(view.items || []);
          setQuickDrawerOpen(false);
          setQuickDeck([]);
        }}
      />

      {/* 手动添加/编辑轮播抽屉 */}
      <BannerDrawer
        open={editorVisible}
        mode={editorMode}
        sourceId={sourceId}
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
