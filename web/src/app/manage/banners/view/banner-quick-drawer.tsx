"use client";

import React, { useState, useEffect, useRef, useCallback, useMemo } from "react";
import {
  Drawer,
  InputNumber,
  Radio,
  Space,
  Progress,
  Tag,
  Button,
  Flex,
  Tooltip,
  Switch,
  Alert,
  Popconfirm,
  Image,
} from "antd";
import {
  CompassOutlined,
  FireOutlined,
  StarOutlined,
  StarFilled,
  ClockCircleOutlined,
  LoadingOutlined,
  RedoOutlined,
  AppstoreOutlined,
  ThunderboltOutlined,
  CheckCircleOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  DeleteOutlined,
  ExclamationCircleOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { BannerRecord, MAX_BANNER_COUNT } from "./types";
import styles from "./index.module.less";

interface BannerQuickDrawerProps {
  open: boolean;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onSuccess: (banners: BannerRecord[]) => void;
}

interface CategoryQuotaItem {
  id: number;
  name: string;
  count: number;
}

interface PickedFilm {
  mid: number;
  name: string;
  category?: string;
}

interface QuickProgressState {
  stage: "idle" | "filtering" | "picking" | "scraping" | "done";
  current: number;
  total: number;
  message: string;
  pickedList: PickedFilm[];
}

const initialProgress: QuickProgressState = {
  stage: "idle",
  current: 0,
  total: 0,
  message: "",
  pickedList: [],
};

export default function BannerQuickDrawer({
  open,
  tmdbEnabled = false,
  onClose,
  onSuccess,
}: BannerQuickDrawerProps) {
  const [drawerStage, setDrawerStage] = useState<"config" | "generating" | "preview">("config");
  const [strategy, setStrategy] = useState<"hot" | "score" | "latest">("hot");
  const [autoScrape, setAutoScrape] = useState(false);
  const [categories, setCategories] = useState<CategoryQuotaItem[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [publishing, setPublishing] = useState(false);
  const [progress, setProgress] = useState<QuickProgressState>(initialProgress);
  const [previewDeck, setPreviewDeck] = useState<BannerRecord[]>([]);

  const abortControllerRef = useRef<AbortController | null>(null);
  const { message } = useAppMessage();

  // 计算当前配置的总排片数
  const totalPlannedCount = useMemo(() => {
    return categories.reduce((sum, item) => sum + (item.count || 0), 0);
  }, [categories]);

  const isOverLimit = totalPlannedCount > MAX_BANNER_COUNT;
  const isZero = totalPlannedCount <= 0;

  // 均分排片数量至各分类
  const handleDistribute = useCallback(
    (targetTotal: number) => {
      if (categories.length === 0) return;
      const numCats = categories.length;
      const perCat = Math.floor(targetTotal / numCats);
      const rem = targetTotal % numCats;

      setCategories((prev) =>
        prev.map((cat, idx) => ({
          ...cat,
          count: perCat + (idx < rem ? 1 : 0),
        })),
      );
    },
    [categories.length],
  );

  // 清空所有分类排片数量
  const handleClearAll = useCallback(() => {
    setCategories((prev) => prev.map((cat) => ({ ...cat, count: 0 })));
  }, []);

  useEffect(() => {
    if (!open) {
      if (abortControllerRef.current) {
        abortControllerRef.current.abort();
        abortControllerRef.current = null;
      }
      setSubmitting(false);
      setPublishing(false);
      setDrawerStage("config");
      setProgress(initialProgress);
      setPreviewDeck([]);
      return;
    }

    setDrawerStage("config");
    setStrategy("hot");
    setAutoScrape(Boolean(tmdbEnabled));
    setProgress(initialProgress);
    setPreviewDeck([]);

    // 拉取所有可用一级分类并生成初始配额
    ApiGet("/categories")
      .then((resp) => {
        if (resp.code === 0 && Array.isArray(resp.data)) {
          const list: CategoryQuotaItem[] = resp.data.map((c: any) => ({
            id: Number(c.id),
            name: String(c.name || ""),
            count: 0,
          }));

          // 默认分配策略：总计 6 部
          let remaining = 6;
          for (let i = 0; i < list.length && remaining > 0; i++) {
            const allocate = i < 2 ? Math.min(2, remaining) : 1;
            list[i].count = allocate;
            remaining -= allocate;
          }
          setCategories(list);
        }
      })
      .catch(() => {});
  }, [open, tmdbEnabled]);

  const handleCancel = useCallback(() => {
    if (abortControllerRef.current) {
      abortControllerRef.current.abort();
      abortControllerRef.current = null;
    }
    setSubmitting(false);
    setPublishing(false);
    setProgress(initialProgress);
    onClose();
  }, [onClose]);

  // 开始流式排片
  const handleStartGenerate = async () => {
    if (isZero) {
      message.warning("请至少为一个分类指定排片数量");
      return;
    }
    if (isOverLimit) {
      message.error(`排片总数不能超过上限（${MAX_BANNER_COUNT}部），当前已设定 ${totalPlannedCount} 部`);
      return;
    }

    const activeQuotas = categories
      .filter((c) => (c.count || 0) > 0)
      .map((c) => ({
        categoryId: c.id,
        count: c.count,
      }));

    try {
      setSubmitting(true);
      setDrawerStage("generating");
      setProgress({
        stage: "filtering",
        current: 0,
        total: totalPlannedCount,
        message: "正在连接排片流式服务...",
        pickedList: [],
      });

      const controller = new AbortController();
      abortControllerRef.current = controller;

      let finalBanners: BannerRecord[] | null = null;

      await fetchEventStream("/api/manage/banner/quick-generate", {
        method: "POST",
        body: {
          strategy,
          quotas: activeQuotas,
          autoScrape: tmdbEnabled && autoScrape,
        },
        signal: controller.signal,
        onMessage: (evt: any) => {
          if (!evt || typeof evt !== "object") return;

          if (evt.event === "start") {
            setProgress((prev) => ({
              ...prev,
              stage: "filtering",
              total: evt.total ?? totalPlannedCount,
              message: evt.message || "正在检索片库候选影片...",
            }));
          } else if (evt.event === "progress") {
            setProgress((prev) => {
              const nextList = [...prev.pickedList];
              if (evt.name && !nextList.some((item) => Number(item.mid) === Number(evt.mid))) {
                nextList.push({
                  mid: Number(evt.mid),
                  name: evt.name,
                  category: evt.category,
                });
              }
              return {
                ...prev,
                stage: evt.stage || "picking",
                current: typeof evt.current === "number" ? evt.current : prev.current,
                total: typeof evt.total === "number" ? evt.total : prev.total,
                message: evt.message || prev.message,
                pickedList: nextList,
              };
            });
          } else if (evt.event === "done") {
            finalBanners = Array.isArray(evt.banners) ? (evt.banners as BannerRecord[]) : [];
            setProgress((prev) => ({
              ...prev,
              stage: "done",
              current: prev.total,
              message: evt.message || "一键排片方案生成完毕",
            }));
            setPreviewDeck(finalBanners);
            setDrawerStage("preview");
            setSubmitting(false);
            message.success("排片方案已成功生成，请在抽屉内预览确认！");
          } else if (evt.event === "error") {
            message.error(evt.error || "一键排片失败");
            setSubmitting(false);
            setDrawerStage("config");
          }
        },
      });
    } catch (err: any) {
      if (err?.name === "AbortError") {
        message.info("已取消一键排片");
      } else {
        message.error(err?.message || "网络异常，一键排片失败");
      }
      setSubmitting(false);
      setDrawerStage("config");
    } finally {
      abortControllerRef.current = null;
    }
  };

  // 在抽屉内直接替换发布到线上
  const handlePublishToLive = async () => {
    if (previewDeck.length === 0) {
      message.warning("排片方案为空，无法发布");
      return;
    }
    setPublishing(true);
    try {
      const resp = await ApiPost("/manage/banner/save-deck", previewDeck);
      if (resp.code === 0) {
        message.success("整套轮播已成功替换并实时发布上线！");
        onSuccess(previewDeck);
        onClose();
      } else {
        message.error(resp.msg || "发布失败");
      }
    } catch {
      message.error("网络异常，发布失败");
    } finally {
      setPublishing(false);
    }
  };

  // 预览卡片微调：上移/下移
  const handleMovePreviewOrder = (index: number, direction: "up" | "down") => {
    const targetIndex = direction === "up" ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= previewDeck.length) return;
    const updated = [...previewDeck];
    const temp = updated[index];
    updated[index] = updated[targetIndex];
    updated[targetIndex] = temp;
    // 重新校准 sort
    updated.forEach((item, idx) => {
      item.sort = idx + 1;
    });
    setPreviewDeck(updated);
  };

  // 预览卡片微调：移除单项
  const handleRemovePreviewItem = (index: number) => {
    const updated = previewDeck.filter((_, idx) => idx !== index);
    updated.forEach((item, idx) => {
      item.sort = idx + 1;
    });
    setPreviewDeck(updated);
    message.info("已从本次排片方案中移除该影片");
  };

  const percent =
    progress.total > 0
      ? Math.min(100, Math.round((progress.current / progress.total) * 100))
      : 0;

  // 统计就绪情况
  const slideReadyCount = useMemo(() => {
    return previewDeck.filter(
      (b) =>
        b.pictureSlide &&
        b.pictureSlide.trim() !== "" &&
        b.pictureSlide !== b.picture &&
        b.pictureSlide !== b.poster,
    ).length;
  }, [previewDeck]);

  return (
    <Drawer
      open={open}
      onClose={handleCancel}
      width={840}
      destroyOnClose
      maskClosable={!submitting && !publishing}
      title={
        <Flex align="center" justify="space-between" style={{ width: "100%", paddingRight: 8 }}>
          <Space size={10} align="center">
            <CompassOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
            <span style={{ fontSize: 16, fontWeight: 600 }}>一键排片工作台</span>
          </Space>
          <Space size={8}>
            {drawerStage === "config" && (
              <Tag color="default" style={{ borderRadius: 4, margin: 0 }}>
                步骤 1: 排片参数配置
              </Tag>
            )}
            {drawerStage === "generating" && (
              <Tag color="processing" icon={<LoadingOutlined />} style={{ borderRadius: 4, margin: 0 }}>
                步骤 2: 流式排片与刮削中
              </Tag>
            )}
            {drawerStage === "preview" && (
              <Tag color="success" icon={<CheckCircleOutlined />} style={{ borderRadius: 4, margin: 0 }}>
                步骤 3: 方案预览与确认
              </Tag>
            )}
          </Space>
        </Flex>
      }
      footer={
        <Flex justify="space-between" align="center">
          <div>
            {drawerStage === "preview" && (
              <Button onClick={() => setDrawerStage("config")} disabled={publishing}>
                调整参数重新排片
              </Button>
            )}
          </div>
          <Space size={8}>
            <Button onClick={handleCancel} disabled={publishing}>
              {drawerStage === "preview" ? "放弃本次排片" : submitting ? "中止排片" : "取消"}
            </Button>

            {drawerStage !== "preview" ? (
              <Button
                type="primary"
                onClick={handleStartGenerate}
                loading={submitting}
                disabled={submitting || isZero || isOverLimit}
              >
                {submitting ? "排片处理中..." : "开始排片"}
              </Button>
            ) : (
              <Popconfirm
                title="确认替换线上轮播？"
                description={`将以当前排好的 ${previewDeck.length} 部影片整体替换发布到线上首页，前台将实时生效。`}
                onConfirm={handlePublishToLive}
                okText="确认发布"
                cancelText="取消"
                okButtonProps={{ loading: publishing }}
              >
                <Button type="primary" loading={publishing} style={{ background: "#52c41a", borderColor: "#52c41a" }}>
                  确认替换线上轮播
                </Button>
              </Popconfirm>
            )}
          </Space>
        </Flex>
      }
    >
      <div className={styles.drawerStack}>
        {/* 阶段 1 & 2：配置面板与排片进度 */}
        {drawerStage !== "preview" && (
          <>
            {/* 排序策略 */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <span>1. 排序策略</span>
              </div>
              <Radio.Group
                value={strategy}
                onChange={(e) => setStrategy(e.target.value)}
                buttonStyle="solid"
                style={{ width: "100%" }}
                disabled={submitting}
              >
                <Radio.Button value="hot" style={{ width: "33.33%", textAlign: "center" }}>
                  <FireOutlined /> 最热优先
                </Radio.Button>
                <Radio.Button value="score" style={{ width: "33.33%", textAlign: "center" }}>
                  <StarOutlined /> 评分优先
                </Radio.Button>
                <Radio.Button value="latest" style={{ width: "33.33%", textAlign: "center" }}>
                  <ClockCircleOutlined /> 最新更新
                </Radio.Button>
              </Radio.Group>
            </div>

            {/* 同步 TMDB 自动刮削 */}
            <div
              style={{
                display: "flex",
                alignItems: "center",
                justifyContent: "space-between",
                padding: "12px 16px",
                borderRadius: 10,
                background: "var(--ant-color-fill-quaternary, rgba(255, 255, 255, 0.02))",
                border: "1px solid var(--ant-color-border-secondary)",
              }}
            >
              <Space size={10} align="center">
                <ThunderboltOutlined
                  style={{
                    color: tmdbEnabled ? "#722ed1" : "var(--ant-color-text-quaternary)",
                    fontSize: 20,
                  }}
                />
                <div>
                  <div style={{ fontSize: 13, fontWeight: 600, color: "var(--ant-color-text)" }}>
                    同步 TMDB 自动刮削
                  </div>
                  <div style={{ fontSize: 12, color: "var(--ant-color-text-secondary)" }}>
                    {tmdbEnabled
                      ? "排片时自动对影片执行 TMDB 全量元数据刮削（海报、横图、评分、演职员、简介等全字段补齐）"
                      : "未配置或未启用 TMDB 刮削（请先前往「系统设置 → 刮削配置」完成配置）"}
                  </div>
                </div>
              </Space>

              <Tooltip title={!tmdbEnabled ? "未配置或未启用 TMDB 刮削功能，开关已禁用" : undefined}>
                <Switch
                  checked={tmdbEnabled && autoScrape}
                  onChange={(checked) => setAutoScrape(checked)}
                  disabled={!tmdbEnabled || submitting}
                />
              </Tooltip>
            </div>

            {/* 各分类排片数量配额配置 */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <Space size={6} align="center">
                  <AppstoreOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>2. 各分类排片数量配置</span>
                </Space>

                <Space size={8} align="center">
                  <Tag
                    color={isOverLimit ? "error" : isZero ? "warning" : "processing"}
                    style={{ margin: 0, borderRadius: 4 }}
                  >
                    已计划总数: {totalPlannedCount} / {MAX_BANNER_COUNT} 部
                  </Tag>
                  {!submitting && (
                    <Space size={4}>
                      <Button size="small" onClick={() => handleDistribute(6)}>
                        均分 6 部
                      </Button>
                      <Button size="small" onClick={() => handleDistribute(MAX_BANNER_COUNT)}>
                        均分 12 部
                      </Button>
                      <Tooltip title="将全部类别的排片数量重置为 0">
                        <Button size="small" icon={<RedoOutlined />} onClick={handleClearAll} />
                      </Tooltip>
                    </Space>
                  )}
                </Space>
              </div>

              <div
                style={{
                  display: "grid",
                  gridTemplateColumns: "repeat(auto-fill, minmax(170px, 1fr))",
                  gap: 8,
                  maxHeight: 260,
                  overflowY: "auto",
                  padding: "4px 2px",
                }}
              >
                {categories.map((cat) => {
                  const isChecked = (cat.count || 0) > 0;
                  return (
                    <div
                      key={cat.id}
                      style={{
                        display: "flex",
                        alignItems: "center",
                        justifyContent: "space-between",
                        padding: "8px 10px",
                        borderRadius: 8,
                        background: isChecked
                          ? "var(--ant-color-primary-bg, rgba(22, 119, 255, 0.06))"
                          : "var(--ant-color-fill-quaternary, rgba(255, 255, 255, 0.02))",
                        border: `1px solid ${
                          isChecked
                            ? "var(--ant-color-primary-border, #91caff)"
                            : "var(--ant-color-border-secondary)"
                        }`,
                        transition: "all 0.2s ease",
                      }}
                    >
                      <span
                        style={{
                          fontSize: 13,
                          fontWeight: isChecked ? 600 : 400,
                          color: isChecked ? "var(--ant-color-text)" : "var(--ant-color-text-secondary)",
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                          whiteSpace: "nowrap",
                        }}
                      >
                        {cat.name}
                      </span>
                      <InputNumber
                        min={0}
                        max={MAX_BANNER_COUNT}
                        size="small"
                        value={cat.count}
                        onChange={(val) => {
                          const nextCount = Math.max(0, Number(val) || 0);
                          setCategories((prev) =>
                            prev.map((c) => (c.id === cat.id ? { ...c, count: nextCount } : c)),
                          );
                        }}
                        style={{ width: 62 }}
                        disabled={submitting}
                      />
                    </div>
                  );
                })}
              </div>

              {isOverLimit && (
                <div style={{ fontSize: 12, color: "var(--ant-color-error)" }}>
                  * 排片总数已超出上限（最多 {MAX_BANNER_COUNT} 部），请减少部分分类的数量
                </div>
              )}
              {isZero && !isOverLimit && (
                <div style={{ fontSize: 12, color: "var(--ant-color-warning)" }}>
                  * 请至少为一个分类配置排片数量（大于 0）
                </div>
              )}
            </div>

            {/* 流式排片进度指示器 */}
            {submitting && (
              <div
                style={{
                  padding: 14,
                  borderRadius: 10,
                  background: "var(--ant-color-bg-layout)",
                  border: "1px solid var(--ant-color-border-secondary)",
                }}
              >
                <div
                  style={{
                    display: "flex",
                    justifyContent: "space-between",
                    alignItems: "center",
                    marginBottom: 6,
                  }}
                >
                  <span style={{ fontSize: 13, fontWeight: 600, color: "var(--ant-color-text)" }}>
                    {progress.stage === "done"
                      ? "排片方案已生成"
                      : "正在抽选并处理影片..."}
                  </span>
                  <span style={{ fontSize: 12, color: "var(--ant-color-text-secondary)" }}>
                    {progress.current} / {progress.total}
                  </span>
                </div>

                <Progress
                  percent={percent}
                  status={progress.stage === "done" ? "success" : "active"}
                  strokeColor={{ "0%": "#1677ff", "100%": "#52c41a" }}
                  size="small"
                />

                <div
                  style={{
                    marginTop: 6,
                    fontSize: 12,
                    color: "var(--ant-color-text-description)",
                    whiteSpace: "nowrap",
                    overflow: "hidden",
                    textOverflow: "ellipsis",
                  }}
                >
                  {progress.message || "正在检索片库候选影片..."}
                </div>

                {progress.pickedList.length > 0 && (
                  <div
                    style={{
                      marginTop: 8,
                      paddingTop: 8,
                      borderTop: "1px dashed var(--ant-color-border-secondary)",
                      display: "flex",
                      flexWrap: "wrap",
                      gap: 6,
                      maxHeight: 110,
                      overflowY: "auto",
                    }}
                  >
                    {progress.pickedList.map((p, idx) => (
                      <Tag
                        key={p.mid || idx}
                        color="blue"
                        style={{
                          margin: 0,
                          fontSize: 11,
                          borderRadius: 4,
                          display: "inline-flex",
                          alignItems: "center",
                          gap: 4,
                        }}
                      >
                        <span style={{ maxWidth: 140, overflow: "hidden", textOverflow: "ellipsis" }}>
                          {p.name}
                        </span>
                        {p.category && (
                          <span style={{ opacity: 0.75, fontSize: 10 }}>({p.category})</span>
                        )}
                      </Tag>
                    ))}
                  </div>
                )}
              </div>
            )}
          </>
        )}

        {/* 阶段 3：排片结果预览与核对（抽屉内完整展示） */}
        {drawerStage === "preview" && (
          <>
            <Alert
              type="info"
              showIcon
              icon={<ExclamationCircleOutlined />}
              message="排片方案已成功生成（草稿预览状态）"
              description="线上首页轮播尚未发生任何变动。请在下方核对影片阵容、海报封面与横图就绪情况。核对满意后，点击底部「确认替换线上轮播」即可全量生效。"
              style={{ borderRadius: 8 }}
            />

            {/* 方案概要指标栏 */}
            <div className={styles.drawerSummaryBar}>
              <Space size={14} wrap align="center">
                <span>
                  排片总数: <strong>{previewDeck.length}</strong> 部
                </span>
                <span>
                  横图就绪:{" "}
                  <strong style={{ color: slideReadyCount === previewDeck.length ? "#52c41a" : "#fa8c16" }}>
                    {slideReadyCount} / {previewDeck.length}
                  </strong>
                </span>
                <span>
                  排序策略:{" "}
                  <strong>
                    {strategy === "hot" ? "最热优先" : strategy === "score" ? "评分优先" : "最新更新"}
                  </strong>
                </span>
              </Space>
              <span style={{ fontSize: 12, color: "var(--ant-color-text-secondary)" }}>
                （支持在卡片右上方微调顺位或移除单片）
              </span>
            </div>

            {/* 排片方案卡片网格列表 */}
            <div className={styles.previewDeckGrid}>
              {previewDeck.map((item, idx) => {
                const posterUrl = item.picture || item.poster || item.customPicture || FALLBACK_IMG;
                const hasRealSlide = Boolean(
                  item.pictureSlide &&
                    item.pictureSlide.trim() !== "" &&
                    item.pictureSlide !== item.picture &&
                    item.pictureSlide !== item.poster,
                );

                return (
                  <div key={item.id || item.mid || idx} className={styles.previewDeckCard}>
                    <div className={styles.previewDeckCardIndex}>#{String(idx + 1).padStart(2, "0")}</div>

                    {/* 竖版封面 */}
                    <div className={styles.previewDeckCardPoster}>
                      <Image
                        src={posterUrl}
                        alt={item.name}
                        fallback={FALLBACK_IMG}
                        preview={{ mask: "预览" }}
                        wrapperStyle={{ width: "100%", height: "100%" }}
                        style={{ width: "100%", height: "100%", objectFit: "cover" }}
                      />
                    </div>

                    {/* 卡片详情 */}
                    <div className={styles.previewDeckCardInfo}>
                      <div>
                        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 6 }}>
                          <span className={styles.previewDeckCardTitle} title={item.name}>
                            {item.name}
                          </span>

                          {/* 顺位微调与移除 */}
                          <Space size={2} align="center">
                            <Button
                              type="text"
                              size="small"
                              icon={<ArrowUpOutlined style={{ fontSize: 10 }} />}
                              disabled={idx === 0}
                              onClick={() => handleMovePreviewOrder(idx, "up")}
                              title="上移"
                              style={{ width: 22, height: 22, padding: 0 }}
                            />
                            <Button
                              type="text"
                              size="small"
                              icon={<ArrowDownOutlined style={{ fontSize: 10 }} />}
                              disabled={idx === previewDeck.length - 1}
                              onClick={() => handleMovePreviewOrder(idx, "down")}
                              title="下移"
                              style={{ width: 22, height: 22, padding: 0 }}
                            />
                            <Popconfirm
                              title="确认从本次排片中移除该影片？"
                              onConfirm={() => handleRemovePreviewItem(idx)}
                              okText="确定"
                              cancelText="取消"
                            >
                              <Button
                                type="text"
                                danger
                                size="small"
                                icon={<DeleteOutlined style={{ fontSize: 11 }} />}
                                title="移除"
                                style={{ width: 22, height: 22, padding: 0 }}
                              />
                            </Popconfirm>
                          </Space>
                        </div>

                        {/* 标签栏 */}
                        <div className={styles.previewDeckCardTags}>
                          {item.cName && (
                            <Tag color="orange" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                              {item.cName}
                            </Tag>
                          )}
                          {item.year ? (
                            <Tag color="blue" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                              {item.year}
                            </Tag>
                          ) : null}
                          {item.score && item.score > 0 ? (
                            <Tag color="gold" icon={<StarFilled />} style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                              {item.score.toFixed(1)} 分
                            </Tag>
                          ) : null}
                          {hasRealSlide ? (
                            <Tag color="success" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                              横图就绪
                            </Tag>
                          ) : (
                            <Tag color="warning" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                              无横图
                            </Tag>
                          )}
                        </div>
                      </div>

                      {/* 剧情简介或横图缩略 */}
                      <div style={{ display: "flex", gap: 8, alignItems: "flex-end", marginTop: 4 }}>
                        <div className={styles.previewDeckCardBlurb} title={item.blurb}>
                          {item.blurb || "暂无简介，点击封面可预览"}
                        </div>

                        {hasRealSlide && item.pictureSlide && (
                          <div className={styles.previewDeckCardSlideThumb} title="横屏大图">
                            <Image
                              src={item.pictureSlide}
                              alt="横图"
                              fallback={FALLBACK_IMG}
                              preview={{ mask: "横图" }}
                              wrapperStyle={{ width: "100%", height: "100%" }}
                              style={{ width: "100%", height: "100%", objectFit: "cover" }}
                            />
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          </>
        )}
      </div>
    </Drawer>
  );
}
