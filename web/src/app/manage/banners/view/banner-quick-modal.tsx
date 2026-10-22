"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button, Flex, Modal, Progress, Space, Spin, Tag } from "antd";
import { CompassOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerRecord } from "./types";
import BannerQuickConfig, { CategoryQuotaItem, QuickStrategy } from "./banner-quick-config";
import {
  BannerQuickRecipe,
  mergeBannerQuickCategories,
  normalizeQuickStrategy,
  positiveQuotas,
  quickRecipeSnapshot,
} from "./banner-quick-recipe";
import styles from "./banner-quick.module.less";

interface BannerQuickModalProps {
  open: boolean;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onGenerated: (deck: BannerRecord[], strategy: QuickStrategy) => void;
}

interface PickedFilm {
  mid: number;
  name: string;
  category?: string;
}

interface QuickProgressState {
  current: number;
  total: number;
  message: string;
  stage: "filtering" | "picking" | "scraping" | "done";
  pickedList: PickedFilm[];
}

const initialProgress: QuickProgressState = {
  current: 0,
  total: 0,
  message: "",
  stage: "picking",
  pickedList: [],
};

export default function BannerQuickModal({
  open,
  tmdbEnabled = false,
  onClose,
  onGenerated,
}: BannerQuickModalProps) {
  const [stage, setStage] = useState<"config" | "progress">("config");
  const [strategy, setStrategy] = useState<QuickStrategy>("hot");
  const [autoScrape, setAutoScrape] = useState(false);
  const [categories, setCategories] = useState<CategoryQuotaItem[]>([]);
  const [configLoading, setConfigLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [savedSnapshot, setSavedSnapshot] = useState("");
  const [progress, setProgress] = useState<QuickProgressState>(initialProgress);
  const abortRef = useRef<AbortController | null>(null);
  const cancelledRef = useRef(false);
  const { message } = useAppMessage();
  const messageRef = useRef(message);
  messageRef.current = message;

  const totalPlannedCount = useMemo(
    () => categories.reduce((sum, item) => sum + (item.count || 0), 0),
    [categories],
  );
  const isZero = totalPlannedCount <= 0;
  const currentSnapshot = quickRecipeSnapshot(strategy, Boolean(tmdbEnabled && autoScrape), categories);
  const configDirty = !configLoading && savedSnapshot !== "" && currentSnapshot !== savedSnapshot;

  const applyRecipe = useCallback((nav: { id: number; name: string }[], recipe: BannerQuickRecipe | null) => {
    const saved = Boolean(recipe?.saved);
    const nextStrategy = saved ? normalizeQuickStrategy(recipe?.strategy) : "hot";
    const nextScrape = saved ? Boolean(recipe?.autoScrape) : Boolean(tmdbEnabled);
    const nextCategories = mergeBannerQuickCategories(nav, recipe);
    setStrategy(nextStrategy);
    setAutoScrape(nextScrape);
    setCategories(nextCategories);
    setSavedSnapshot(quickRecipeSnapshot(nextStrategy, Boolean(tmdbEnabled && nextScrape), nextCategories));
  }, [tmdbEnabled]);

  useEffect(() => {
    if (open) return;
    cancelledRef.current = true;
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setSubmitting(false);
    setSaving(false);
    setStage("config");
    setProgress(initialProgress);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setConfigLoading(true);
    Promise.all([ApiGet("/categories"), ApiGet("/manage/banner/quick-config")])
      .then(([navResp, cfgResp]) => {
        if (cancelled) return;
        const nav = navResp.code === 0 && Array.isArray(navResp.data)
          ? navResp.data.map((item: { id?: number; name?: string }) => ({
            id: Number(item.id),
            name: String(item.name || ""),
          })).filter((item: { id: number }) => item.id > 0)
          : [];
        const recipe = cfgResp.code === 0 ? (cfgResp.data as BannerQuickRecipe) : null;
        if (cfgResp.code !== 0) messageRef.current.error(cfgResp.msg || "配置读取失败");
        applyRecipe(nav, recipe);
      })
      .catch(() => {
        if (!cancelled) {
          messageRef.current.error("配置读取失败");
          applyRecipe([], null);
        }
      })
      .finally(() => {
        if (!cancelled) setConfigLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open, applyRecipe]);

  const stopGenerate = useCallback((backToConfig: boolean) => {
    cancelledRef.current = true;
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setSubmitting(false);
    setProgress(initialProgress);
    if (backToConfig) setStage("config");
  }, []);

  const handleClose = () => {
    stopGenerate(true);
    onClose();
  };

  const handleSave = async () => {
    setSaving(true);
    try {
      const resp = await ApiPost("/manage/banner/quick-config", {
        strategy,
        autoScrape: Boolean(tmdbEnabled && autoScrape),
        quotas: positiveQuotas(categories),
      });
      if (resp.code !== 0) {
        message.error(resp.msg || "保存失败");
        return;
      }
      setSavedSnapshot(quickRecipeSnapshot(strategy, Boolean(tmdbEnabled && autoScrape), categories));
      message.success("配置已保存");
    } catch {
      message.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  const handleStart = async () => {
    if (isZero) {
      message.warning("请配置排片数量");
      return;
    }
    const quotas = categories
      .filter((item) => (item.count || 0) > 0)
      .map((item) => ({ categoryId: item.id, count: item.count }));

    setSubmitting(true);
    setStage("progress");
    setProgress({
      current: 0,
      total: totalPlannedCount,
      message: "连接中...",
      stage: "filtering",
      pickedList: [],
    });

    cancelledRef.current = false;
    const controller = new AbortController();
    abortRef.current = controller;
    let handedOff = false;

    try {
      await fetchEventStream("/api/manage/banner/quick-generate", {
        method: "POST",
        body: {
          strategy,
          quotas,
          autoScrape: tmdbEnabled && autoScrape,
        },
        signal: controller.signal,
        onMessage: (evt: any) => {
          if (cancelledRef.current) return;
          if (!evt || typeof evt !== "object") return;
          if (evt.event === "start") {
            setProgress((prev) => ({
              ...prev,
              total: evt.total ?? prev.total,
              stage: "filtering",
              message: evt.message || "检索中...",
            }));
            return;
          }
          if (evt.event === "progress") {
            setProgress((prev) => {
              const nextList = [...prev.pickedList];
              if (evt.name && !nextList.some((item) => Number(item.mid) === Number(evt.mid))) {
                nextList.push({
                  mid: Number(evt.mid),
                  name: evt.name,
                  category: evt.category,
                });
              }
              const isScraping = evt.stage === "scraping";
              return {
                current: typeof evt.current === "number" ? evt.current : prev.current,
                total: typeof evt.total === "number" ? evt.total : prev.total,
                stage: (evt.stage as any) || (isScraping ? "scraping" : "picking"),
                message: evt.message || prev.message,
                pickedList: nextList,
              };
            });
            return;
          }
          if (evt.event === "done") {
            const deck = Array.isArray(evt.banners) ? (evt.banners as BannerRecord[]) : [];
            handedOff = true;
            abortRef.current = null;
            setSubmitting(false);
            setProgress(initialProgress);
            setStage("config");
            message.success("排片完成");
            onGenerated(deck, strategy);
            return;
          }
          if (evt.event === "error") {
            message.error(evt.error || "排片失败");
            setSubmitting(false);
            setStage("config");
            setProgress(initialProgress);
          }
        },
      });
    } catch (err: any) {
      if (handedOff) return;
      if (err?.name === "AbortError") {
        message.info("已取消");
      } else {
        message.error(err?.message || "排片失败");
      }
      setSubmitting(false);
      setStage("config");
      setProgress(initialProgress);
    } finally {
      if (abortRef.current === controller) abortRef.current = null;
    }
  };

  const isScrapingStage = progress.stage === "scraping";
  const percent = useMemo(() => {
    if (progress.total <= 0) return 0;
    const ratio = Math.min(1, Math.max(0, progress.current / progress.total));
    if (autoScrape && tmdbEnabled) {
      if (isScrapingStage) {
        return Math.min(100, Math.round(40 + ratio * 60));
      }
      return Math.round(ratio * 40);
    }
    return Math.min(100, Math.round(ratio * 100));
  }, [progress.current, progress.total, isScrapingStage, autoScrape, tmdbEnabled]);

  return (
    <Modal
      open={open}
      title={
        <Space size={8}>
          <CompassOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>{stage === "progress" ? (isScrapingStage ? "TMDB 剧照刮削" : "正在排片") : "一键排片"}</span>
        </Space>
      }
      width={760}
      centered
      mask={{ closable: !submitting }}
      closable={!submitting}
      onCancel={handleClose}
      destroyOnHidden
      footer={
        stage === "progress" ? (
          <Button onClick={() => stopGenerate(true)}>中止</Button>
        ) : (
          <Flex justify="flex-end" align="center" gap={8}>
            {configDirty && <span className={styles.quotaHint}>配置未保存</span>}
            <Button onClick={handleClose}>取消</Button>
            <Button
              disabled={configLoading || saving || submitting}
              loading={saving}
              onClick={() => void handleSave()}
            >
              保存配置
            </Button>
            <Button
              type="primary"
              disabled={isZero || configLoading || saving || submitting}
              onClick={() => void handleStart()}
            >
              开始排片
            </Button>
          </Flex>
        )
      }
    >
      {stage === "config" ? (
        <Spin spinning={configLoading}>
          <BannerQuickConfig
            strategy={strategy}
            onStrategyChange={setStrategy}
            tmdbEnabled={tmdbEnabled}
            autoScrape={autoScrape}
            onAutoScrapeChange={setAutoScrape}
            categories={categories}
            onCategoriesChange={setCategories}
            disabled={configLoading || saving || submitting}
          />
        </Spin>
      ) : (
        <div className={styles.progressPanel}>
          <div className={styles.progressHead}>
            <span className={styles.progressTitle}>
              {isScrapingStage ? "TMDB 智能刮削" : "正在从片库排片"}
            </span>
            <span className={styles.progressCount}>
              {progress.current} / {progress.total}
            </span>
          </div>
          <Progress percent={percent} status="active" />
          <div className={styles.progressMessage}>
            {progress.message || "检索中..."}
          </div>
          {progress.pickedList.length > 0 && (
            <div className={styles.progressTags}>
              {progress.pickedList.map((item, idx) => (
                <Tag
                  key={`${item.mid}-${idx}`}
                  style={{
                    display: "inline-flex",
                    alignItems: "center",
                    gap: 4,
                  }}
                >
                  <span className={styles.progressTagName}>{item.name}</span>
                  {item.category && (
                    <span className={styles.progressTagCategory}>
                      ({item.category})
                    </span>
                  )}
                </Tag>
              ))}
            </div>
          )}
        </div>
      )}
    </Modal>
  );
}
