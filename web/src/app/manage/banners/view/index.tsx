"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import { Table, Button, Tag, Space, Tooltip } from "antd";
import {
  PlusOutlined,
  ThunderboltOutlined,
  DesktopOutlined,
  CompassOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import ManagePageHeader from "@/app/manage/components/page-header";
import BannerModal from "./banner-modal";
import BannerQuickModal from "./banner-quick-modal";
import { getBannerColumns } from "./banner-columns";
import { BannerRecord, EditorMode, MAX_BANNER_COUNT } from "./types";
import styles from "./index.module.less";

export default function BannersPageView() {
  const tmdbEnabled = useTmdbEnabled();
  const [banners, setBanners] = useState<BannerRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const [batchScraping, setBatchScraping] = useState(false);
  const [quickModalOpen, setQuickModalOpen] = useState(false);

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

  // 一键批量刮削当前排片列表中缺失横图的影片（SSE 实时事件流）
  const handleBatchScrape = useCallback(async () => {
    setBatchScraping(true);
    let successCount = 0;
    try {
      await fetchEventStream("/api/manage/banner/batch-scrape", {
        method: "POST",
        onMessage: (evt: any) => {
          if (!evt || typeof evt !== "object") return;
          if (evt.event === "progress") {
            if (evt.success && evt.slide && evt.mid) {
              setBanners((prev) =>
                prev.map((item) =>
                  String(item.id) === String(evt.mid)
                    ? { ...item, pictureSlide: evt.slide, isScraped: true }
                    : item,
                ),
              );
            }
          } else if (evt.event === "done") {
            successCount = evt.successCount ?? 0;
            if (evt.banners && Array.isArray(evt.banners)) {
              setBanners(evt.banners as BannerRecord[]);
            } else {
              void fetchBanners();
            }
            message.success(`批量刮削完成（成功 ${successCount} 部）`);
          } else if (evt.event === "error") {
            message.error(evt.error || "批量刮削失败");
          }
        },
      });
    } catch (err: any) {
      message.error(err?.message || "批量刮削失败");
    } finally {
      setBatchScraping(false);
    }
  }, [message, fetchBanners]);

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

  const columns = useMemo<ColumnsType<BannerRecord>>(() => {
    return getBannerColumns({
      banners,
      canWrite,
      handleMoveOrder,
      openEditEditor,
      handleDelete,
    });
  }, [banners, canWrite, handleMoveOrder, openEditEditor, handleDelete]);

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader title="首页轮播" />

      <Table
        bordered
        dataSource={banners}
        columns={columns}
        rowKey="id"
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: 950 }}
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>
              <Space size={10} align="center">
                <DesktopOutlined style={{ color: "var(--ant-color-primary)" }} />
                <span>轮播列表</span>
                <span className={styles.countBadge}>
                  {banners.length} / {MAX_BANNER_COUNT}
                </span>
                <Tag
                  color={slideReadyCount === banners.length ? "success" : "warning"}
                  style={{ borderRadius: 4 }}
                >
                  横图: {slideReadyCount}/{banners.length}
                </Tag>
                {customCoverCount > 0 && (
                  <Tag color="cyan" style={{ borderRadius: 4 }}>
                    自定义: {customCoverCount}/{banners.length}
                  </Tag>
                )}
              </Space>
            </div>

            <Space size={8} wrap className={styles.tableActions}>
              <Button
                type="primary"
                size="middle"
                icon={<CompassOutlined />}
                disabled={!canWrite}
                onClick={() => setQuickModalOpen(true)}
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
                  批量刮削
                </Button>
              )}

              <Tooltip title={isFull ? "已达上限" : undefined}>
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
        )}
      />

      {/* 一键快速排片弹窗 */}
      <BannerQuickModal
        open={quickModalOpen}
        tmdbEnabled={tmdbEnabled}
        onClose={() => setQuickModalOpen(false)}
        onSuccess={(newBanners) => {
          setBanners(newBanners);
          void fetchBanners();
        }}
      />

      {/* 手动添加/编辑轮播弹窗 */}
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
