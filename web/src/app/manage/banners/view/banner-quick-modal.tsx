"use client";

import React, { useEffect, useMemo, useRef, useState } from "react";
import { Button, Modal, Progress, Space, Spin, Tag } from "antd";
import { CompassOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerRecord } from "./types";
import BannerQuickConfig, { CategoryQuotaItem, QuickStrategy } from "./banner-quick-config";
import { positiveQuotas } from "./banner-quick-recipe";
import styles from "./banner-quick.module.less";

interface BannerQuickModalProps {
  open: boolean;
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
  stage: "filtering" | "picking" | "done";
  pickedList: PickedFilm[];
}

// 排片配置不保存。每次打开都用默认值：最热，每类 1 部。
const DEFAULT_STRATEGY: QuickStrategy = "hot";
const DEFAULT_COUNT = 1;

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
  onGenerated,
}: BannerQuickModalProps) {
  const [phase, setPhase] = useState<"config" | "running">("config");
  const [loadingCategories, setLoadingCategories] = useState(false);
  const [strategy, setStrategy] = useState<QuickStrategy>(DEFAULT_STRATEGY);
  const [categories, setCategories] = useState<CategoryQuotaItem[]>([]);
  const [progress, setProgress] = useState<QuickProgressState>(initialProgress);
  const abortRef = useRef<AbortController | null>(null);
  const { message } = useAppMessage();
  const messageRef = useRef(message);
  useEffect(() => {
    messageRef.current = message;
  }, [message]);

  const abortRunning = () => {
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
  };

  // 每次打开都重新读分类，并回到默认配置。
  useEffect(() => {
    if (!open) {
      abortRunning();
      setPhase("config");
      setProgress(initialProgress);
      return;
    }

    let cancelled = false;
    setPhase("config");
    setStrategy(DEFAULT_STRATEGY);
    setCategories([]);
    setLoadingCategories(true);
    ApiGet("/categories")
      .then((resp) => {
        if (cancelled) return;
        const nav: CategoryQuotaItem[] =
          resp.code === 0 && Array.isArray(resp.data)
            ? resp.data
                .map((item: { id?: number; name?: string }) => ({
                  id: Number(item.id),
                  name: String(item.name || ""),
                  count: DEFAULT_COUNT,
                }))
                .filter((item: CategoryQuotaItem) => item.id > 0)
            : [];
        setCategories(nav);
        if (resp.code !== 0) {
          messageRef.current.error(resp.msg || "读取分类失败");
        }
      })
      .catch(() => {
        if (!cancelled) messageRef.current.error("读取分类失败");
      })
      .finally(() => {
        if (!cancelled) setLoadingCategories(false);
      });

    return () => {
      cancelled = true;
    };
  }, [open]);

  // 组件卸载时中止仍在进行的排片。
  useEffect(() => abortRunning, []);

  const quotas = useMemo(() => positiveQuotas(categories), [categories]);

  const startGenerate = async () => {
    if (quotas.length === 0) {
      messageRef.current.warning("请至少为一个分类设置排片数量");
      return;
    }
    const runStrategy = strategy;
    const controller = new AbortController();
    abortRef.current = controller;
    setPhase("running");
    setProgress({
      current: 0,
      total: 0,
      message: "正在连接...",
      stage: "filtering",
      pickedList: [],
    });

    const isStale = () => abortRef.current !== controller;
    let handedOff = false;
    try {
      await fetchEventStream("/api/manage/banner/quick-generate", {
        method: "POST",
        body: { strategy: runStrategy, quotas },
        signal: controller.signal,
        onMessage: (evt: any) => {
          if (isStale()) return;
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
            setProgress(initialProgress);
            messageRef.current.success("排片完成");
            onGenerated(deck, runStrategy);
            return;
          }

          if (evt.event === "error") {
            abortRef.current = null;
            messageRef.current.error(evt.error || "排片失败");
            setProgress(initialProgress);
            setPhase("config");
          }
        },
      });
    } catch (err: any) {
      if (handedOff || isStale()) return;
      abortRef.current = null;
      if (err?.name !== "AbortError") {
        messageRef.current.error(err?.message || "排片失败");
      }
      setProgress(initialProgress);
      setPhase("config");
    }
  };

  // 中止后回到配置，可以改了再排。
  const stopGenerate = () => {
    abortRunning();
    setProgress(initialProgress);
    setPhase("config");
  };

  const running = phase === "running";
  const isFiltering = progress.stage === "filtering";
  const stageTitle = !running
    ? "一键排片"
    : isFiltering
      ? "正在检索片库分类"
      : "正在装配排片卡组";

  const percent = (() => {
    if (progress.total <= 0) return 0;
    const ratio = Math.min(1, Math.max(0, progress.current / progress.total));
    if (isFiltering) {
      return Math.min(50, Math.round(ratio * 50));
    }
    return Math.min(100, Math.round(50 + ratio * 50));
  })();

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
      closable={!running}
      onCancel={onClose}
      destroyOnHidden
      footer={
        running ? (
          <Button onClick={stopGenerate}>中止</Button>
        ) : (
          <Space size={8}>
            <Button onClick={onClose}>取消</Button>
            <Button
              type="primary"
              disabled={loadingCategories || quotas.length === 0}
              onClick={() => void startGenerate()}
            >
              开始排片
            </Button>
          </Space>
        )
      }
    >
      {!running ? (
        loadingCategories ? (
          <div style={{ padding: "32px 0", textAlign: "center" }}>
            <Spin description="读取分类..." />
          </div>
        ) : (
          <BannerQuickConfig
            strategy={strategy}
            onStrategyChange={setStrategy}
            categories={categories}
            onCategoriesChange={setCategories}
          />
        )
      ) : (
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
      )}
    </Modal>
  );
}
