"use client";

import React, { useEffect, useMemo, useState } from "react";
import { Button, Modal, Space } from "antd";
import { CompassOutlined } from "@ant-design/icons";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import BannerQuickConfig, {
  CategoryQuotaItem,
  QuickStrategy,
} from "./banner-quick-config";

export interface RecommendQuickGroupItem {
  id: string;
  title: string;
  syncLimit: number;
}

export interface RecommendPreviewGroup {
  groupId: string;
  title: string;
  items: {
    groupId: string;
    sourceId: string;
    mid: number;
    sort: number;
    doubanScore?: string;
    name: string;
    picture: string;
    year: string;
    cName: string;
    area: string;
    remarks: string;
    score?: number;
  }[];
}

interface RecommendQuickModalProps {
  open: boolean;
  sourceId: string;
  groups: RecommendQuickGroupItem[];
  onClose: () => void;
  onGenerated: (previewGroups: RecommendPreviewGroup[], strategy: QuickStrategy) => void;
}

const RECOMMEND_FILL_PRESETS = [
  { label: "每组 6 部", count: 6 },
  { label: "每组 12 部", count: 12 },
  { label: "每组 18 部", count: 18 },
  { label: "清零", count: 0 },
];

export default function RecommendQuickModal({
  open,
  sourceId,
  groups,
  onClose,
  onGenerated,
}: RecommendQuickModalProps) {
  const { message } = useAppMessage();
  const [strategy, setStrategy] = useState<QuickStrategy>("douban");
  const [items, setItems] = useState<CategoryQuotaItem[]>([]);
  const [running, setRunning] = useState(false);

  useEffect(() => {
    if (!open) return;
    setItems(
      groups.map((g, idx) => ({
        id: idx + 1,
        key: g.id,
        name: g.title,
        count: g.syncLimit || 12,
      })),
    );
  }, [open, groups]);

  const totalCount = useMemo(
    () => items.reduce((sum, it) => sum + (it.count || 0), 0),
    [items],
  );

  const handleStart = async () => {
    const quotas = items
      .filter((it) => (it.count || 0) > 0)
      .map((it) => ({ groupId: String(it.key || it.id), count: it.count }));
    if (quotas.length === 0) {
      message.warning("请至少为一个分组设置排片数量");
      return;
    }
    setRunning(true);
    try {
      const res = await ApiPost<{ groups: RecommendPreviewGroup[] }>("/manage/recommends/generate", {
        sourceId,
        strategy,
        quotas,
      });
      if (res.code === 0 && res.data) {
        onGenerated(res.data.groups || [], strategy);
      } else {
        message.error(res.msg || "生成排片失败");
      }
    } catch {
      message.error("生成排片失败");
    } finally {
      setRunning(false);
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
      width={620}
      centered
      destroyOnHidden
      onCancel={onClose}
      footer={
        <Space>
          <Button disabled={running} onClick={onClose}>
            取消
          </Button>
          <Button
            type="primary"
            loading={running}
            disabled={totalCount <= 0}
            onClick={() => void handleStart()}
          >
            开始排片
          </Button>
        </Space>
      }
    >
      <BannerQuickConfig
        strategy={strategy}
        onStrategyChange={setStrategy}
        categories={items}
        onCategoriesChange={setItems}
        disabled={running}
        quotaTitle="分组数量配额"
        quotaMax={24}
        fillPresets={RECOMMEND_FILL_PRESETS}
      />
    </Modal>
  );
}
