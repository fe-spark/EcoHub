"use client";

import React, { useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  Drawer,
  Space,
  Tag,
} from "antd";
import {
  CheckCircleOutlined,
  CompassOutlined,
} from "@ant-design/icons";
import { API_CONFLICT, ApiPut } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerListView, BannerRecord } from "./types";
import { deckItemFields } from "./banner-modal-utils";
import { QuickStrategy } from "./banner-quick-config";
import BannerQuickCard from "./banner-quick-card";
import TmdbModal from "@/app/manage/film/components/tmdb-modal";
import styles from "./banner-quick.module.less";

const STRATEGY_LABEL: Record<QuickStrategy, string> = {
  latest: "最近更新",
  hot: "最热排片",
  score: "评分优先",
};

interface BannerQuickDrawerProps {
  open: boolean;
  sourceId: string;
  /** 排片时该站的版本号；发布时带上，期间有人改过就返回 409。 */
  revision: string;
  deck: BannerRecord[];
  strategy: QuickStrategy;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onRedo: () => void;
  onChange: (deck: BannerRecord[]) => void;
  onSuccess: (view: BannerListView) => void;
}

function withSort(list: BannerRecord[]) {
  return list.map((item, idx) => ({ ...item, sort: idx + 1 }));
}

export default function BannerQuickDrawer({
  open,
  sourceId,
  revision,
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
    const fillPoster = shouldFill("poster", "picture") && Boolean(data.picture);
    const fillSlide = shouldFill("backdrop", "pictureSlide") && Boolean(data.pictureSlide);
    if (fillPoster || fillSlide) {
      // 用了 TMDB 图片就改为自定义图片，没填的那张沿用当前图。
      patch.imageMode = "custom";
      patch.isCustomPic = true;
      const poster = fillPoster ? data.picture : tmdbTargetItem.picture || tmdbTargetItem.poster;
      patch.picture = poster;
      patch.poster = poster;
      patch.customPicture = poster;
      patch.pictureSlide = fillSlide ? data.pictureSlide : tmdbTargetItem.pictureSlide;
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
      const resp = await ApiPut<BannerListView>("/manage/banners/deck", {
        sourceId,
        revision,
        items: deck.map(deckItemFields),
      });
      if (resp.code === 0 && resp.data) {
        message.success("发布成功");
        onSuccess(resp.data);
        return;
      }
      if (resp.code === API_CONFLICT) {
        message.error("排片期间该站轮播已被修改，请重新排片后再发布");
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
      size="min(1180px, 96vw)"
      destroyOnHidden
      mask={{ closable: !publishing }}
      title={
        <Space size={10} align="center">
          <CompassOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
          <span style={{ fontSize: 16, fontWeight: 600 }}>排片结果</span>
          <Tag color="blue" style={{ borderRadius: 4, margin: 0 }}>
            {STRATEGY_LABEL[strategy]}
          </Tag>
          <Tag color="success" icon={<CheckCircleOutlined />} style={{ borderRadius: 4, margin: 0 }}>
            待发布
          </Tag>
        </Space>
      }
      extra={
        <Space size={8}>
          <Button onClick={onClose} disabled={publishing}>
            取消
          </Button>
          <Button onClick={onRedo} disabled={publishing}>
            重新排片
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
              onScrape={tmdbEnabled ? handleSingleScrape : undefined}
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
