"use client";

import React, { useEffect, useMemo, useRef, useState } from "react";
import { Button, Modal, Space, Spin } from "antd";
import { CompassOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerGenerateResult } from "./types";
import BannerQuickConfig, {
  CategoryQuotaItem,
  QUOTA_MAX,
  QuickConfigMemo,
  QuickStrategy,
} from "./banner-quick-config";
import { positiveQuotas } from "./banner-quick-recipe";

interface BannerQuickModalProps {
  open: boolean;
  sourceId: string;
  // 该站上次排片用的配置；没有时用默认值。
  lastConfig?: QuickConfigMemo;
  onRemember: (config: QuickConfigMemo) => void;
  onClose: () => void;
  onGenerated: (result: BannerGenerateResult, strategy: QuickStrategy) => void;
}

// 该站没有上次配置时的默认值：最热，每类 1 部。
const DEFAULT_STRATEGY: QuickStrategy = "hot";
const DEFAULT_COUNT = 1;

export default function BannerQuickModal({
  open,
  sourceId,
  lastConfig,
  onRemember,
  onClose,
  onGenerated,
}: BannerQuickModalProps) {
  const [loadingCategories, setLoadingCategories] = useState(false);
  const [running, setRunning] = useState(false);
  const [strategy, setStrategy] = useState<QuickStrategy>(DEFAULT_STRATEGY);
  const [categories, setCategories] = useState<CategoryQuotaItem[]>([]);
  const abortRef = useRef<AbortController | null>(null);
  const { message } = useAppMessage();
  const messageRef = useRef(message);
  useEffect(() => {
    messageRef.current = message;
  }, [message]);
  const lastConfigRef = useRef(lastConfig);
  useEffect(() => {
    lastConfigRef.current = lastConfig;
  }, [lastConfig]);

  const abortRunning = () => {
    abortRef.current?.abort();
    abortRef.current = null;
    setRunning(false);
  };

  // 每次打开都重新读该站分类。已有分类沿用上次的数量，新分类用默认值，已不存在的分类丢掉。
  useEffect(() => {
    if (!open) {
      abortRunning();
      return;
    }

    let cancelled = false;
    const memo = lastConfigRef.current;
    setStrategy(memo?.strategy ?? DEFAULT_STRATEGY);
    setCategories([]);
    setLoadingCategories(true);
    ApiGet("/categories", { sourceId: sourceId || undefined })
      .then((resp) => {
        if (cancelled) return;
        const nav: CategoryQuotaItem[] =
          resp.code === 0 && Array.isArray(resp.data)
            ? resp.data
                .map((item: { id?: number; name?: string }) => ({
                  id: Number(item.id),
                  name: String(item.name || ""),
                  count: Math.min(memo?.counts[Number(item.id)] ?? DEFAULT_COUNT, QUOTA_MAX),
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
  }, [open, sourceId]);

  // 组件卸载时中止仍在进行的排片。
  useEffect(() => () => abortRef.current?.abort(), []);

  const quotas = useMemo(() => positiveQuotas(categories), [categories]);

  const startGenerate = async () => {
    if (quotas.length === 0) {
      messageRef.current.warning("请至少为一个分类设置排片数量");
      return;
    }
    const runStrategy = strategy;
    onRemember({
      strategy: runStrategy,
      counts: Object.fromEntries(categories.map((item) => [item.id, item.count || 0])),
    });
    const controller = new AbortController();
    abortRef.current = controller;
    setRunning(true);
    try {
      const resp = await ApiPost<BannerGenerateResult>(
        "/manage/banners/generate",
        { sourceId, strategy: runStrategy, quotas },
        { signal: controller.signal },
      );
      if (abortRef.current !== controller) return;
      abortRef.current = null;
      setRunning(false);
      if (resp.code !== 0 || !resp.data) {
        messageRef.current.error(resp.msg || "排片失败");
        return;
      }
      const result = resp.data;
      if (result.skipped?.length) {
        const nameOf = new Map(categories.map((item) => [item.id, item.name]));
        const text = result.skipped
          .map((item) =>
            item.reason === "not_site_category"
              ? `${nameOf.get(item.categoryId) || item.categoryId}（不是该站分类）`
              : `${nameOf.get(item.categoryId) || item.categoryId}（要 ${item.requested} 部，只有 ${item.got} 部）`,
          )
          .join("、");
        messageRef.current.warning(`部分分类没有排满：${text}`);
      }
      onGenerated({ ...result, items: result.items || [], skipped: result.skipped || [] }, runStrategy);
    } catch {
      // 中止不提示；网络错误已由请求层提示。
      if (abortRef.current === controller) {
        abortRef.current = null;
        setRunning(false);
      }
    }
  };

  return (
    <Modal
      open={open}
      title={
        <Space size={8}>
          <CompassOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>一键排片</span>
        </Space>
      }
      width={640}
      centered
      mask={{ closable: false }}
      closable={!running}
      onCancel={onClose}
      destroyOnHidden
      footer={
        <Space size={8}>
          {running ? (
            <Button onClick={abortRunning}>中止</Button>
          ) : (
            <Button onClick={onClose}>取消</Button>
          )}
          <Button
            type="primary"
            loading={running}
            disabled={loadingCategories || quotas.length === 0}
            onClick={() => void startGenerate()}
          >
            {running ? "排片中" : "开始排片"}
          </Button>
        </Space>
      }
    >
      {loadingCategories ? (
        <div style={{ padding: "32px 0", textAlign: "center" }}>
          <Spin description="读取分类..." />
        </div>
      ) : (
        <BannerQuickConfig
          strategy={strategy}
          onStrategyChange={setStrategy}
          categories={categories}
          onCategoriesChange={setCategories}
          disabled={running}
        />
      )}
    </Modal>
  );
}
