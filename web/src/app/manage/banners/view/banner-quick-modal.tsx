"use client";

import React, { useEffect, useMemo, useRef, useState } from "react";
import { Button, Modal, Progress, Space, Tag } from "antd";
import { CompassOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerRecord } from "./types";
import { QuickStrategy } from "./banner-quick-config";
import {
  BannerQuickRecipe,
  positiveQuotas,
} from "./banner-quick-recipe";
import styles from "./banner-quick.module.less";

interface BannerQuickModalProps {
  open: boolean;
  onClose: () => void;
  onOpenConfig?: () => void;
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
  stage: "filtering" | "picking" | "done";
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
  onClose,
  onOpenConfig,
  onGenerated,
}: BannerQuickModalProps) {
  const [submitting, setSubmitting] = useState(false);
  const [progress, setProgress] = useState<QuickProgressState>(initialProgress);
  const abortRef = useRef<AbortController | null>(null);
  const cancelledRef = useRef(false);
  const { message } = useAppMessage();
  const messageRef = useRef(message);
  useEffect(() => {
    messageRef.current = message;
  }, [message]);

  const stopGenerate = () => {
    cancelledRef.current = true;
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setSubmitting(false);
    setProgress(initialProgress);
    onClose();
  };

  useEffect(() => {
    if (!open) {
      cancelledRef.current = true;
      if (abortRef.current) {
        abortRef.current.abort();
        abortRef.current = null;
      }
      setSubmitting(false);
      setProgress(initialProgress);
      return;
    }

    let isCancelled = false;
    cancelledRef.current = false;
    const controller = new AbortController();
    abortRef.current = controller;
    setSubmitting(true);
    setProgress({
      current: 0,
      total: 0,
      message: "正在连接...",
      stage: "filtering",
      pickedList: [],
    });

    // 读取配置并立即开始排片
    Promise.all([ApiGet("/categories"), ApiGet("/manage/banner/quick-config")])
      .then(async ([navResp, cfgResp]) => {
        if (isCancelled || cancelledRef.current) return;
        const nav =
          navResp.code === 0 && Array.isArray(navResp.data)
            ? navResp.data
                .map((item: { id?: number; name?: string }) => ({
                  id: Number(item.id),
                  name: String(item.name || ""),
                }))
                .filter((item: { id: number }) => item.id > 0)
            : [];
        const recipe = cfgResp.code === 0 ? (cfgResp.data as BannerQuickRecipe) : null;
        const strategy: QuickStrategy = recipe?.strategy || "hot";

        // 提取有效配额，若无配置则默认每类 1 部
        let quotas = recipe?.quotas
          ? recipe.quotas.filter((q) => q.count > 0)
          : [];
        if (quotas.length === 0 && nav.length > 0) {
          quotas = nav.map((c) => ({ categoryId: c.id, count: 1 }));
        }

        if (quotas.length === 0) {
          messageRef.current.warning("尚未配置排片分类数量");
          stopGenerate();
          onOpenConfig?.();
          return;
        }

        let handedOff = false;
        try {
          await fetchEventStream("/api/manage/banner/quick-generate", {
            method: "POST",
            body: {
              strategy,
              quotas,
            },
            signal: controller.signal,
            onMessage: (evt: any) => {
              if (cancelledRef.current || isCancelled) return;
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
                  if (
                    evt.name &&
                    !nextList.some((item) => Number(item.mid) === Number(evt.mid))
                  ) {
                    nextList.push({
                      mid: Number(evt.mid),
                      name: evt.name,
                      category: evt.category,
                    });
                  }
                  return {
                    current: typeof evt.current === "number" ? evt.current : prev.current,
                    total: typeof evt.total === "number" ? evt.total : prev.total,
                    stage: (evt.stage as any) || "picking",
                    message: evt.message || prev.message,
                    pickedList: nextList,
                  };
                });
                return;
              }

              if (evt.event === "done") {
                const deck = Array.isArray(evt.banners)
                  ? (evt.banners as BannerRecord[])
                  : [];
                handedOff = true;
                abortRef.current = null;
                setSubmitting(false);
                setProgress(initialProgress);
                messageRef.current.success("排片完成");
                onGenerated(deck, strategy);
                return;
              }

              if (evt.event === "error") {
                messageRef.current.error(evt.error || "排片失败");
                setSubmitting(false);
                setProgress(initialProgress);
                onClose();
              }
            },
          });
        } catch (err: any) {
          if (handedOff) return;
          if (err?.name !== "AbortError") {
            messageRef.current.error(err?.message || "排片失败");
          }
          setSubmitting(false);
          setProgress(initialProgress);
          onClose();
        }
      })
      .catch(() => {
        if (!isCancelled && !cancelledRef.current) {
          messageRef.current.error("读取排片配置失败");
          stopGenerate();
        }
      });

    return () => {
      isCancelled = true;
      if (abortRef.current === controller) {
        controller.abort();
        abortRef.current = null;
      }
    };
  }, [open]);

  const isFiltering = progress.stage === "filtering";
  const stageTitle = isFiltering ? "正在检索片库分类" : "正在装配排片卡组";

  const percent = useMemo(() => {
    if (progress.total <= 0) return 0;
    const ratio = Math.min(1, Math.max(0, progress.current / progress.total));
    if (isFiltering) {
      return Math.min(50, Math.round(ratio * 50));
    }
    return Math.min(100, Math.round(50 + ratio * 50));
  }, [progress.current, progress.total, isFiltering]);

  return (
    <Modal
      open={open}
      title={
        <Space size={8}>
          <CompassOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>{stageTitle}</span>
        </Space>
      }
      width={640}
      centered
      mask={{ closable: false }}
      closable={false}
      destroyOnHidden
      footer={
        <Button onClick={stopGenerate} disabled={!submitting}>
          中止
        </Button>
      }
    >
      <div className={styles.progressPanel}>
        <div className={styles.progressHead}>
          <span className={styles.progressTitle}>{stageTitle}</span>
          <span className={styles.progressCount}>
            {progress.current} / {progress.total}
          </span>
        </div>
        <Progress percent={percent} status="active" />
        <div className={styles.progressMessage}>
          {progress.message || "正在处理..."}
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
    </Modal>
  );
}
