"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button, Drawer, Space, Spin, Tag } from "antd";
import { SettingOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import BannerQuickConfig, { CategoryQuotaItem, QuickStrategy } from "./banner-quick-config";
import {
  BannerQuickRecipe,
  mergeBannerQuickCategories,
  normalizeQuickStrategy,
  positiveQuotas,
  quickRecipeSnapshot,
} from "./banner-quick-recipe";
import styles from "./banner-quick.module.less";

interface BannerConfigDrawerProps {
  open: boolean;
  onClose: () => void;
  onSaved?: () => void;
}

export default function BannerConfigDrawer({
  open,
  onClose,
  onSaved,
}: BannerConfigDrawerProps) {
  const [strategy, setStrategy] = useState<QuickStrategy>("hot");
  const [categories, setCategories] = useState<CategoryQuotaItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [savedSnapshot, setSavedSnapshot] = useState("");
  const { message } = useAppMessage();
  const messageRef = useRef(message);
  messageRef.current = message;

  const currentSnapshot = quickRecipeSnapshot(strategy, categories);
  const configDirty = !loading && savedSnapshot !== "" && currentSnapshot !== savedSnapshot;

  const applyRecipe = useCallback(
    (nav: { id: number; name: string }[], recipe: BannerQuickRecipe | null) => {
      const saved = Boolean(recipe?.saved);
      const nextStrategy = saved ? normalizeQuickStrategy(recipe?.strategy) : "hot";
      const nextCategories = mergeBannerQuickCategories(nav, recipe);
      setStrategy(nextStrategy);
      setCategories(nextCategories);
      setSavedSnapshot(quickRecipeSnapshot(nextStrategy, nextCategories));
    },
    [],
  );

  const fetchConfig = useCallback(() => {
    let cancelled = false;
    setLoading(true);
    Promise.all([ApiGet("/categories"), ApiGet("/manage/banner/quick-config")])
      .then(([navResp, cfgResp]) => {
        if (cancelled) return;
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
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [applyRecipe]);

  useEffect(() => {
    if (open) {
      fetchConfig();
    }
  }, [open, fetchConfig]);

  const handleSave = async () => {
    setSaving(true);
    try {
      const resp = await ApiPost("/manage/banner/quick-config", {
        strategy,
        quotas: positiveQuotas(categories),
      });
      if (resp.code !== 0) {
        message.error(resp.msg || "保存失败");
        return;
      }
      setSavedSnapshot(quickRecipeSnapshot(strategy, categories));
      message.success("排片配置已保存");
      onSaved?.();
      onClose();
    } catch {
      message.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={560}
      destroyOnClose
      title={
        <Space size={10} align="center">
          <SettingOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
          <span style={{ fontSize: 16, fontWeight: 600 }}>排片配置</span>
          {configDirty && (
            <Tag color="warning" style={{ borderRadius: 4, margin: 0 }}>
              未保存
            </Tag>
          )}
        </Space>
      }
      extra={
        <Space size={8}>
          <Button onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button
            type="primary"
            loading={saving}
            disabled={loading || saving}
            onClick={() => void handleSave()}
          >
            保存配置
          </Button>
        </Space>
      }
    >
      <Spin spinning={loading}>
        <div style={{ paddingBottom: 24 }}>
          <BannerQuickConfig
            strategy={strategy}
            onStrategyChange={setStrategy}
            categories={categories}
            onCategoriesChange={setCategories}
            disabled={loading || saving}
          />
        </div>
      </Spin>
    </Drawer>
  );
}
