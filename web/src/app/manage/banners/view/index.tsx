"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Button, Tag, Space, Tooltip, Spin, Empty, Progress } from "antd";
import {
  PlusOutlined,
  ThunderboltOutlined,
  DesktopOutlined,
  CompassOutlined,
  LoadingOutlined,
} from "@ant-design/icons";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import ManagePageHeader from "@/app/manage/components/page-header";
import BannerModal from "./banner-modal";
import BannerQuickDrawer from "./banner-quick-drawer";
import BannerGrid from "./banner-grid";
import { BannerRecord, EditorMode, MAX_BANNER_COUNT } from "./types";
import styles from "./index.module.less";

interface ScrapeProgressInfo {
  current: number;
  total: number;
  currentName?: string;
  successCount: number;
}

export default function BannersPageView() {
  const tmdbEnabled = useTmdbEnabled();
  const [banners, setBanners] = useState<BannerRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [batchScraping, setBatchScraping] = useState(false);
  const [scrapeProgress, setScrapeProgress] = useState<ScrapeProgressInfo | null>(null);
  const batchScrapeAbortRef = useRef<AbortController | null>(null);
  const [quickDrawerOpen, setQuickDrawerOpen] = useState(false);

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
        setBanners((resp.data || []) as BannerRecord[]);
      } else {
        messageRef.current.error(resp.msg || "获取轮播列表失败");
      }
    } catch {
      messageRef.current.error("网络异常，获取轮播列表失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchBanners();
  }, [fetchBanners]);

  // 取消批量刮削
  const handleCancelBatchScrape = useCallback(() => {
    if (batchScrapeAbortRef.current) {
      batchScrapeAbortRef.current.abort();
      batchScrapeAbortRef.current = null;
    }
    setBatchScraping(false);
    setScrapeProgress(null);
    message.info("已取消批量刮削");
  }, [message]);

  // 一键批量刮削当前排片列表中缺失横图的影片（SSE 实时事件流）
  const handleBatchScrape = useCallback(async () => {
    setBatchScraping(true);
    setScrapeProgress({ current: 0, total: 0, successCount: 0 });
    const controller = new AbortController();
    batchScrapeAbortRef.current = controller;
    let accumulatedSuccess = 0;

    try {
      await fetchEventStream("/api/manage/banner/batch-scrape", {
        method: "POST",
        signal: controller.signal,
        onMessage: (evt: any) => {
          if (!evt || typeof evt !== "object") return;
          if (evt.event === "start") {
            setScrapeProgress({
              current: 0,
              total: evt.missingCount ?? 0,
              successCount: 0,
            });
          } else if (evt.event === "progress") {
            if (evt.success) {
              accumulatedSuccess++;
            }
            setScrapeProgress((prev) => ({
              current: typeof evt.current === "number" ? evt.current : (prev?.current ?? 0),
              total: typeof evt.total === "number" ? evt.total : (prev?.total ?? 0),
              currentName: evt.name || prev?.currentName,
              successCount: accumulatedSuccess,
            }));
            if (evt.success && evt.slide && evt.mid) {
              setBanners((prev) =>
                prev.map((item) =>
                  Number(item.mid) === Number(evt.mid)
                    ? { ...item, pictureSlide: evt.slide, isScraped: true }
                    : item,
                ),
              );
            }
          } else if (evt.event === "done") {
            setScrapeProgress(null);
            const totalSuccess = evt.successCount ?? accumulatedSuccess;
            if (evt.banners && Array.isArray(evt.banners)) {
              setBanners(evt.banners as BannerRecord[]);
            } else {
              void fetchBanners();
            }
            message.success(`批量刮削完成（成功更新 ${totalSuccess} 部影片全量数据）`);
          } else if (evt.event === "error") {
            setScrapeProgress(null);
            message.error(evt.error || "批量刮削失败");
          }
        },
      });
    } catch (err: any) {
      if (err?.name === "AbortError") {
        message.info("已取消批量刮削");
      } else {
        message.error(err?.message || "批量刮削失败");
      }
      setScrapeProgress(null);
    } finally {
      setBatchScraping(false);
      batchScrapeAbortRef.current = null;
    }
  }, [message, fetchBanners]);

  // 拖拽完成保存排序
  const handleSortList = useCallback(
    async (nextList: BannerRecord[]) => {
      setBanners(nextList);
      try {
        const resp = await ApiPost("/manage/banner/save-deck", nextList);
        if (resp.code === 0) {
          message.success("排片位次已更新并生效");
        } else {
          message.error(resp.msg || "排序保存失败");
          void fetchBanners();
        }
      } catch {
        message.error("网络异常，排序保存失败");
        void fetchBanners();
      }
    },
    [message, fetchBanners],
  );

  // 上移/下移位次并保存
  const handleMoveOrder = useCallback(
    async (index: number, direction: "up" | "down") => {
      const targetIndex = direction === "up" ? index - 1 : index + 1;
      if (targetIndex < 0 || targetIndex >= banners.length) return;
      const updated = [...banners];
      const temp = updated[index];
      updated[index] = updated[targetIndex];
      updated[targetIndex] = temp;

      setBanners(updated);
      try {
        const resp = await ApiPost("/manage/banner/save-deck", updated);
        if (resp.code !== 0) {
          message.error(resp.msg || "排序保存失败");
          void fetchBanners();
        }
      } catch {
        message.error("网络异常，排序保存失败");
        void fetchBanners();
      }
    },
    [banners, message, fetchBanners],
  );

  // 删除某一项轮播
  const handleDelete = useCallback(
    async (id: string) => {
      try {
        const resp = await ApiPost("/manage/banner/del", { id: String(id) });
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

  const isFull = banners.length >= MAX_BANNER_COUNT;

  const openCreateEditor = () => {
    if (isFull) {
      message.warning(`已达轮播上限（${MAX_BANNER_COUNT}部）`);
      return;
    }
    setCurrentRow(null);
    setEditorMode("create");
    setEditorVisible(true);
  };

  const openEditEditor = useCallback((record: BannerRecord) => {
    setCurrentRow(record);
    setEditorMode("edit");
    setEditorVisible(true);
  }, []);

  // 统计横屏大图就绪情况
  const slideReadyCount = useMemo(() => {
    return banners.filter(
      (b) =>
        b.pictureSlide &&
        b.pictureSlide.trim() !== "" &&
        b.pictureSlide !== b.picture &&
        b.pictureSlide !== b.poster,
    ).length;
  }, [banners]);

  // 统计自定义封面情况
  const customCoverCount = useMemo(() => {
    return banners.filter((b) => b.isCustomPic).length;
  }, [banners]);

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader title="首页轮播" />

      {/* 顶部操作工具栏卡片 */}
      <div className={styles.toolbarCard}>
        <div className={styles.toolbar}>
          <div className={styles.toolbarLeft}>
            <Space size={10} align="center" wrap>
              <DesktopOutlined style={{ color: "var(--ant-color-primary)", fontSize: 16 }} />
              <span className={styles.toolbarTitle}>首页轮播卡组</span>
              <span className={styles.countBadge}>
                {banners.length} / {MAX_BANNER_COUNT}
              </span>
              <Tag
                color={slideReadyCount === banners.length && banners.length > 0 ? "success" : "warning"}
                style={{ borderRadius: 4, margin: 0 }}
              >
                横图就绪: {slideReadyCount}/{banners.length}
              </Tag>
              {customCoverCount > 0 && (
                <Tag color="cyan" style={{ borderRadius: 4, margin: 0 }}>
                  自定义封面: {customCoverCount}/{banners.length}
                </Tag>
              )}
              <span className={styles.toolbarHint}>
                （支持按住卡片头部拖拽排序）
              </span>
            </Space>
          </div>

          <Space size={8} wrap className={styles.toolbarActions}>
            <Button
              type="primary"
              size="middle"
              icon={<CompassOutlined />}
              disabled={!canWrite}
              onClick={() => setQuickDrawerOpen(true)}
              style={{ background: "#0958d9", borderColor: "#0958d9" }}
            >
              一键排片
            </Button>

            {tmdbEnabled && (
              <Button
                type="default"
                size="middle"
                icon={<ThunderboltOutlined />}
                loading={batchScraping}
                disabled={!canWrite || banners.length === 0}
                onClick={handleBatchScrape}
              >
                {batchScraping && scrapeProgress && scrapeProgress.total > 0
                  ? `刮削中 (${scrapeProgress.current}/${scrapeProgress.total})`
                  : batchScraping
                  ? "刮削中..."
                  : "批量刮削"}
              </Button>
            )}

            <Tooltip title={isFull ? `已达上限（${MAX_BANNER_COUNT}部）` : undefined}>
              <span>
                <Button
                  type="primary"
                  size="middle"
                  icon={<PlusOutlined />}
                  onClick={openCreateEditor}
                  disabled={!canWrite || isFull}
                >
                  添加影片
                </Button>
              </span>
            </Tooltip>
          </Space>
        </div>
      </div>

      {/* 批量刮削 SSE 实时进度展示面板 */}
      {batchScraping && scrapeProgress && (
        <div className={styles.scrapeProgressCard}>
          <div className={styles.scrapeProgressTop}>
            <Space size={8} wrap>
              <Spin indicator={<LoadingOutlined style={{ fontSize: 16 }} spin />} />
              <span className={styles.scrapeProgressTitle}>
                正在批量刮削影片全量元数据...
              </span>
              <Tag color="blue" style={{ borderRadius: 4 }}>
                进度：{scrapeProgress.current} / {scrapeProgress.total}
              </Tag>
              {scrapeProgress.successCount > 0 && (
                <Tag color="success" style={{ borderRadius: 4 }}>
                  已完成刮削：{scrapeProgress.successCount} 部
                </Tag>
              )}
            </Space>
            <Button size="small" danger onClick={handleCancelBatchScrape}>
              取消刮削
            </Button>
          </div>
          <Progress
            percent={
              scrapeProgress.total > 0
                ? Math.min(100, Math.round((scrapeProgress.current / scrapeProgress.total) * 100))
                : 0
            }
            status="active"
            strokeColor={{ "0%": "#1677ff", "100%": "#52c41a" }}
            style={{ margin: "8px 0 4px" }}
          />
          <div className={styles.scrapeProgressDetail}>
            {scrapeProgress.currentName ? (
              <span>
                当前正在检索 TMDB: <strong>{scrapeProgress.currentName}</strong>
              </span>
            ) : (
              <span>正在排队请求 TMDB 剧照...</span>
            )}
          </div>
        </div>
      )}

      {/* 轮播卡片列表展示 */}
      {loading && banners.length === 0 ? (
        <div className={styles.emptyCard}>
          <Spin tip="加载轮播数据中..." />
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
                onClick={() => setQuickDrawerOpen(true)}
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
          canWrite={canWrite}
          onSortList={handleSortList}
          onEdit={openEditEditor}
          onDelete={handleDelete}
          onMoveOrder={handleMoveOrder}
          onOpenCreate={openCreateEditor}
        />
      )}

      {/* 一键排片工作台抽屉（在抽屉内完成排片与卡组核对后直接替换发布线上） */}
      <BannerQuickDrawer
        open={quickDrawerOpen}
        tmdbEnabled={tmdbEnabled}
        onClose={() => setQuickDrawerOpen(false)}
        onSuccess={(newBanners) => {
          setBanners(newBanners);
          void fetchBanners();
        }}
      />

      {/* 手动添加/编辑轮播弹窗（在详情中完整展示横屏大图与封面图） */}
      <BannerModal
        open={editorVisible}
        mode={editorMode}
        currentRow={currentRow}
        tmdbEnabled={tmdbEnabled}
        onClose={() => setEditorVisible(false)}
        onSuccess={fetchBanners}
      />
    </div>
  );
}
