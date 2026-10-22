"use client";

import React, { useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  Drawer,
  Flex,
  Space,
  Tag,
} from "antd";
import {
  CheckCircleOutlined,
  CompassOutlined,
} from "@ant-design/icons";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerRecord } from "./types";
import { QuickStrategy } from "./banner-quick-config";
import BannerQuickCard from "./banner-quick-card";
import TmdbModal from "@/app/manage/film/components/tmdb-modal";
import styles from "./banner-quick.module.less";

const STRATEGY_LABEL: Record<QuickStrategy, string> = {
  hot: "最热排片",
  score: "评分优先",
  latest: "最新上线",
};

interface BannerQuickDrawerProps {
  open: boolean;
  deck: BannerRecord[];
  strategy: QuickStrategy;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onRedo: () => void;
  onChange: (deck: BannerRecord[]) => void;
  onSuccess: (banners: BannerRecord[]) => void;
}

function withSort(list: BannerRecord[]) {
  return list.map((item, idx) => ({ ...item, sort: idx + 1 }));
}

function hasRealSlide(item: BannerRecord) {
  return Boolean(
    item.pictureSlide &&
      item.pictureSlide.trim() !== "" &&
      item.pictureSlide !== item.picture &&
      item.pictureSlide !== item.poster,
  );
}

export default function BannerQuickDrawer({
  open,
  deck,
  strategy,
  tmdbEnabled = false,
  onClose,
  onRedo,
  onChange,
  onSuccess,
}: BannerQuickDrawerProps) {
  const [publishing, setPublishing] = useState(false);
  const [tmdbTargetItem, setTmdbTargetItem] = useState<BannerRecord | null>(null);
  const { message } = useAppMessage();
  const deckRef = useRef(deck);
  deckRef.current = deck;

  useEffect(() => {
    if (!open) {
      setTmdbTargetItem(null);
    }
  }, [open]);

  const patchDeckFilm = (mid: number, patch: Partial<BannerRecord>) => {
    const next = deckRef.current.map((item) =>
      item.mid === mid ? { ...item, ...patch } : item,
    );
    deckRef.current = next;
    onChange(next);
  };

  const handleSingleScrape = (mid: number) => {
    if (publishing) return;
    if (!tmdbEnabled) {
      message.warning("未启用 TMDB");
      return;
    }
    const target = deckRef.current.find((item) => item.mid === mid);
    if (target) {
      setTmdbTargetItem(target);
    }
  };

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    if (!tmdbTargetItem) return;
    const shouldFill = (key: string, ...aliases: string[]) => {
      if (!fields || fields.length === 0) return true;
      return [key, ...aliases].some((k) => fields.includes(k));
    };

    const patch: Partial<BannerRecord> = {
      pendingScrape: true,
      scrapeState: "scraped",
    };
    if (data.id || data.tmdbId) {
      patch.tmdbId = Number(data.id || data.tmdbId);
    }
    if (data.mediaType) {
      patch.mediaType = data.mediaType;
    }
    if (shouldFill("poster", "picture") && data.picture) {
      patch.picture = data.picture;
      patch.poster = data.picture;
    }
    if (shouldFill("backdrop", "pictureSlide") && data.pictureSlide) {
      patch.pictureSlide = data.pictureSlide;
    }
    if (shouldFill("score") && (data.dbScore || data.score)) {
      const s = parseFloat(String(data.dbScore || data.score));
      if (!isNaN(s) && s > 0) {
        patch.score = s;
      }
    }
    if (shouldFill("overview", "content", "blurb") && (data.content || data.blurb)) {
      patch.blurb = data.content || data.blurb;
    }
    if (shouldFill("year", "releaseDate") && (data.year || data.releaseDate)) {
      const y = parseInt(String(data.year || data.releaseDate || "0"), 10);
      if (y > 0) {
        patch.year = y;
      }
    }

    patchDeckFilm(tmdbTargetItem.mid, patch);
    setTmdbTargetItem(null);
    message.success("更新完成");
  };

  const handleMove = (index: number, direction: "up" | "down") => {
    const targetIndex = direction === "up" ? index - 1 : index + 1;
    if (targetIndex < 0 || targetIndex >= deck.length) return;
    const next = [...deck];
    const current = next[index];
    next[index] = next[targetIndex];
    next[targetIndex] = current;
    onChange(withSort(next));
  };

  const handleRemove = (index: number) => {
    onChange(withSort(deck.filter((_, idx) => idx !== index)));
    message.info("已移除");
  };

  const handlePublish = async () => {
    if (deck.length === 0) {
      message.warning("暂无排片内容");
      return;
    }
    setPublishing(true);
    try {
      const resp = await ApiPost("/manage/banner/deck", deck);
      if (resp.code === 0) {
        message.success("发布成功");
        const next = Array.isArray(resp.data) ? (resp.data as BannerRecord[]) : deck;
        onSuccess(next);
        return;
      }
      message.error(resp.msg || "发布失败");
    } catch {
      message.error("发布失败");
    } finally {
      setPublishing(false);
    }
  };

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width="min(1180px, 96vw)"
      destroyOnHidden
      mask={{ closable: !publishing }}
      title={
        <Flex align="center" justify="space-between" style={{ width: "100%", paddingRight: 8 }}>
          <Space size={10} align="center">
            <CompassOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
            <span style={{ fontSize: 16, fontWeight: 600 }}>排片审核结果</span>
            <Tag color="blue" style={{ borderRadius: 4, margin: 0 }}>
              {STRATEGY_LABEL[strategy]}
            </Tag>
          </Space>
          <Tag color="success" icon={<CheckCircleOutlined />} style={{ borderRadius: 4, margin: 0 }}>
            待发布
          </Tag>
        </Flex>
      }
      footer={
        <Flex justify="space-between" align="center">
          <Button onClick={onRedo} disabled={publishing}>
            重新排片
          </Button>
          <Space size={8}>
            <Button onClick={onClose} disabled={publishing}>
              取消
            </Button>
            <Button
              type="primary"
              loading={publishing}
              disabled={publishing || deck.length === 0}
              onClick={() => void handlePublish()}
            >
              发布
            </Button>
          </Space>
        </Flex>
      }
    >
      <div className={styles.drawerStack}>
        {/* 顶部概览栏 */}
        <div className={styles.drawerSummaryBar}>
          <div className={styles.summaryLeft}>
            <span>
              总排片数: <strong>{deck.length}</strong>
            </span>
            <span>
              策略: <strong>{STRATEGY_LABEL[strategy]}</strong>
            </span>
          </div>
        </div>

        {deck.length === 0 ? (
          <Alert type="warning" showIcon title="没有可排片的影片" style={{ borderRadius: 8 }} />
        ) : null}

        {/* 宽屏 3 列网格排布 */}
        <div className={styles.previewDeckGrid}>
          {deck.map((item, idx) => (
            <BannerQuickCard
              key={item.id || item.mid || idx}
              item={item}
              index={idx}
              total={deck.length}
              isScrapingThis={false}
              isAnyBusy={publishing}
              onMove={handleMove}
              onRemove={handleRemove}
              onScrape={handleSingleScrape}
            />
          ))}
        </div>
      </div>

      {tmdbEnabled && tmdbTargetItem && (
        <TmdbModal
          open={Boolean(tmdbTargetItem)}
          mid={tmdbTargetItem.mid}
          initialName={tmdbTargetItem.name || ""}
          onClose={() => setTmdbTargetItem(null)}
          onPrefill={handleTmdbPrefill}
        />
      )}
    </Drawer>
  );
}
