"use client";

import React, { useState, useEffect, useCallback, useMemo, useRef } from "react";
import {
  Card,
  Table,
  Button,
  Tag,
  Space,
  Tooltip,
  Popconfirm,
  Image as AntImage,
  Typography,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  PlusOutlined,
  ThunderboltOutlined,
  DesktopOutlined,
  PictureOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  StarFilled,
  CompassOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ManagePageHeader from "@/app/manage/components/page-header";
import BannerModal from "./banner-modal";
import BannerQuickModal from "./banner-quick-modal";
import { BannerRecord, EditorMode, MAX_BANNER_COUNT } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;

export default function BannersPageView() {
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

  // 一键批量刮削当前排片列表中缺失横图的影片
  const handleBatchScrape = useCallback(async () => {
    setBatchScraping(true);
    try {
      const resp = await ApiPost("/manage/banner/batch-scrape", {});
      if (resp.code === 0) {
        message.success(resp.msg || "一键批量刮削完成");
        if (resp.data?.banners) {
          setBanners(resp.data.banners as BannerRecord[]);
        } else {
          await fetchBanners();
        }
      } else {
        message.error(resp.msg || "批量刮削失败");
      }
    } catch (err: any) {
      message.error(err?.message || "网络异常，批量刮削失败");
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

  const columns = useMemo<ColumnsType<BannerRecord>>(() => {
    return [
      {
        title: "位次与排序",
        key: "order",
        width: 100,
        fixed: "left",
        align: "center",
        render: (_, __, index) => {
          const isFirst = index === 0;
          const isLast = index === banners.length - 1;
          return (
            <Space size={4} align="center">
              <Tag color="purple" style={{ borderRadius: 4, fontWeight: 700, margin: 0 }}>
                #{String(index + 1).padStart(2, "0")}
              </Tag>
              <Space.Compact size="small" direction="vertical">
                <Button
                  size="small"
                  type="text"
                  icon={<ArrowUpOutlined style={{ fontSize: 10 }} />}
                  disabled={!canWrite || isFirst}
                  onClick={() => handleMoveOrder(index, "up")}
                  style={{ height: 18, padding: 0 }}
                />
                <Button
                  size="small"
                  type="text"
                  icon={<ArrowDownOutlined style={{ fontSize: 10 }} />}
                  disabled={!canWrite || isLast}
                  onClick={() => handleMoveOrder(index, "down")}
                  style={{ height: 18, padding: 0 }}
                />
              </Space.Compact>
            </Space>
          );
        },
      },
      {
        title: "影片与竖屏海报",
        key: "filmInfo",
        width: 280,
        render: (_, record) => {
          const posterUrl = record.picture || record.poster || record.pictureSlide || FALLBACK_IMG;
          return (
            <div className={styles.filmCell}>
              <div className={styles.posterThumbWrapper}>
                <AntImage
                  src={posterUrl}
                  width={52}
                  height={72}
                  className={styles.posterThumb}
                  fallback={FALLBACK_IMG}
                  preview={{ mask: "预览海报" }}
                  style={{ objectFit: "cover", width: 52, height: 72, display: "block" }}
                />
              </div>

              <div className={styles.filmInfo}>
                <Text className={styles.filmName} ellipsis={{ tooltip: record.name }}>
                  {record.name}
                </Text>
                <div className={styles.filmMeta}>
                  {record.cName && (
                    <Tag color="orange" style={{ borderRadius: 4, margin: 0 }}>
                      {record.cName}
                    </Tag>
                  )}
                  {record.year ? (
                    <Tag color="blue" style={{ borderRadius: 4, margin: 0 }}>
                      {record.year}
                    </Tag>
                  ) : null}
                  <span className={styles.filmId}>MID: {record.mid || record.id}</span>
                </div>
              </div>
            </div>
          );
        },
      },
      {
        title: "横屏背景大图 (Backdrop)",
        key: "pictureSlide",
        width: 160,
        align: "center",
        render: (_, record) => {
          const hasRealSlide = Boolean(
            record.pictureSlide &&
              record.pictureSlide.trim() !== "" &&
              record.pictureSlide !== record.picture &&
              record.pictureSlide !== record.poster,
          );

          if (hasRealSlide) {
            return (
              <div className={styles.slideCell}>
                <div className={styles.slideThumbWrapper}>
                  <AntImage
                    src={record.pictureSlide}
                    width={96}
                    height={54}
                    className={styles.slideThumb}
                    fallback={FALLBACK_IMG}
                    preview={{ mask: "预览横图" }}
                    style={{ objectFit: "cover", width: 96, height: 54, display: "block" }}
                  />
                </div>
              </div>
            );
          }

          return (
            <div className={styles.slideCell}>
              <Tooltip title="缺少横屏大图，点击可进入编辑弹窗刮削补齐">
                <div
                  className={styles.emptySlideBox}
                  onClick={() => openEditEditor(record)}
                  style={{ cursor: "pointer" }}
                >
                  <PictureOutlined style={{ fontSize: 14 }} />
                  <span>暂无横图</span>
                </div>
              </Tooltip>
            </div>
          );
        },
      },
      {
        title: "评分与简介",
        key: "meta",
        width: 220,
        render: (_, record) => (
          <Space direction="vertical" size={2} style={{ width: "100%" }}>
            <Space size={6} align="center">
              {record.score && record.score > 0 ? (
                <Tag color="gold" icon={<StarFilled />} style={{ margin: 0, borderRadius: 4 }}>
                  {record.score.toFixed(1)} 分
                </Tag>
              ) : (
                <Tag style={{ margin: 0, borderRadius: 4 }}>暂无评分</Tag>
              )}
              {record.remark && (
                <Text type="secondary" style={{ fontSize: 12 }}>
                  {record.remark}
                </Text>
              )}
            </Space>
            {record.blurb ? (
              <Text
                type="secondary"
                ellipsis={{ tooltip: record.blurb }}
                style={{ fontSize: 12, display: "block" }}
              >
                {record.blurb}
              </Text>
            ) : null}
          </Space>
        ),
      },
      {
        title: "操作",
        key: "action",
        align: "center",
        fixed: "right",
        width: 110,
        render: (_, record) => (
          <Space size={6}>
            <Tooltip title="编辑轮播（内含 TMDB 刮削与图片配置）">
              <Button
                size="small"
                icon={<EditOutlined />}
                disabled={!canWrite}
                onClick={() => openEditEditor(record)}
              />
            </Tooltip>

            <Popconfirm
              title="确定从轮播中移除此片？"
              onConfirm={() => handleDelete(record.id)}
              okText="确定"
              cancelText="取消"
              okButtonProps={{ danger: true }}
            >
              <Tooltip title="从轮播中移除">
                <Button
                  size="small"
                  danger
                  icon={<DeleteOutlined />}
                  disabled={!canWrite}
                />
              </Tooltip>
            </Popconfirm>
          </Space>
        ),
      },
    ];
  }, [banners.length, canWrite, handleMoveOrder, openEditEditor, handleDelete]);

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="首页轮播"
        description="纯手动排片模式。支持自由添加、排序、编辑影片，支持单片精准 TMDB 刮削与一键批量刮削补图。"
      />

      <Card
        className={styles.tableCard}
        title={
          <Space size={10} align="center">
            <DesktopOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>手动排片卡组</span>
            <span className={styles.countBadge}>
              {banners.length} / {MAX_BANNER_COUNT} 部影片
            </span>
            <Tag color={slideReadyCount === banners.length ? "success" : "warning"} style={{ borderRadius: 4 }}>
              横屏大图就绪: {slideReadyCount}/{banners.length}
            </Tag>
          </Space>
        }
        extra={
          <Space size={10}>
            <Tooltip title="按所选分类与排序策略（最新/最热/评分）一键从片库生成排片">
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
            </Tooltip>

            <Tooltip title="自动检测当前排片中缺少横屏大图的影片，一键并发从 TMDB 检索补齐横屏背景图">
              <Button
                type="default"
                size="middle"
                icon={<ThunderboltOutlined />}
                loading={batchScraping}
                disabled={!canWrite || banners.length === 0}
                onClick={handleBatchScrape}
              >
                一键批量刮削
              </Button>
            </Tooltip>

            <Tooltip title={isFull ? `已达上限（${MAX_BANNER_COUNT}部）` : undefined}>
              <span>
                <Button
                  type="primary"
                  size="middle"
                  icon={<PlusOutlined />}
                  onClick={openCreateEditor}
                  disabled={!canWrite || isFull}
                >
                  添加轮播影片
                </Button>
              </span>
            </Tooltip>
          </Space>
        }
      >
        <Table
          bordered
          dataSource={banners}
          columns={columns}
          rowKey="id"
          loading={loading}
          size="middle"
          pagination={false}
          scroll={{ x: 950 }}
        />
      </Card>

      {/* 一键快速排片弹窗 */}
      <BannerQuickModal
        open={quickModalOpen}
        onClose={() => setQuickModalOpen(false)}
        onSuccess={(newBanners) => {
          setBanners(newBanners);
          void fetchBanners();
        }}
      />

      {/* 手动添加/编辑轮播弹窗（内含单片 TMDB 精准刮削） */}
      <BannerModal
        open={editorVisible}
        mode={editorMode}
        currentRow={currentRow}
        onClose={() => setEditorVisible(false)}
        onSuccess={fetchBanners}
      />
    </div>
  );
}
