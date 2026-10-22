"use client";

import React, { useEffect, useRef, useState } from "react";
import {
  Alert,
  Button,
  Drawer,
  Flex,
  Space,
  Spin,
  Tag,
} from "antd";
import {
  CheckCircleOutlined,
  CompassOutlined,
  LoadingOutlined,
  ThunderboltOutlined,
} from "@ant-design/icons";
import { ApiPost } from "@/lib/client-api";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerRecord } from "./types";
import { QuickStrategy } from "./banner-quick-config";
import BannerQuickCard from "./banner-quick-card";
import TmdbModal from "@/app/manage/film/components/tmdb-modal";
import styles from "./index.module.less";

const STRATEGY_LABEL: Record<QuickStrategy, string> = {
  hot: "最热",
  score: "评分",
  latest: "最新",
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

function needsPreviewScrape(item: BannerRecord) {
  const state =
    item.scrapeState ||
    (item.pendingScrape ? "pending" : item.isScraped ? "scraped" : "none");
  return state === "none" || state === "miss";
}

function withSort(list: BannerRecord[]) {
  return list.map((item, idx) => ({ ...item, sort: idx + 1 }));
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
  const [scraping, setScraping] = useState(false);
  const [activeScrapeMid, setActiveScrapeMid] = useState<number | null>(null);
  const [scrapeProgress, setScrapeProgress] = useState<{
    current: number;
    total: number;
    currentName?: string;
  } | null>(null);
  const [tmdbTargetItem, setTmdbTargetItem] = useState<BannerRecord | null>(null);
  const { message } = useAppMessage();
  const deckRef = useRef(deck);
  deckRef.current = deck;
  const abortRef = useRef<AbortController | null>(null);
  const cancelledRef = useRef(false);
  const busy = publishing || scraping;
  const scrapeTargets = deck.filter(needsPreviewScrape);

  useEffect(() => {
    if (open) return;
    cancelledRef.current = true;
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setActiveScrapeMid(null);
    setScraping(false);
    setScrapeProgress(null);
    setTmdbTargetItem(null);
  }, [open]);

  const stopScrape = () => {
    cancelledRef.current = true;
    if (abortRef.current) {
      abortRef.current.abort();
      abortRef.current = null;
    }
    setActiveScrapeMid(null);
    setScraping(false);
    setScrapeProgress(null);
  };

  const patchDeckFilm = (mid: number, patch: Partial<BannerRecord>) => {
    const next = deckRef.current.map((item) =>
      item.mid === mid ? { ...item, ...patch } : item,
    );
    deckRef.current = next;
    onChange(next);
  };

  const runScrape = async (mids: number[]) => {
    if (mids.length === 0) return;
    const targetMap = new Map(deckRef.current.map((item) => [item.mid, item.name]));
    cancelledRef.current = false;
    setScraping(true);
    setActiveScrapeMid(mids[0] ?? null);
    setScrapeProgress({ current: 0, total: mids.length, currentName: targetMap.get(mids[0]) });
    const controller = new AbortController();
    abortRef.current = controller;
    let matched = 0;
    try {
      await fetchEventStream("/api/manage/banner/preview-scrape", {
        method: "POST",
        body: { mids },
        signal: controller.signal,
        onMessage: (evt: any) => {
          if (cancelledRef.current || !evt || typeof evt !== "object") return;
          if (evt.event === "start") {
            setScrapeProgress({
              current: 0,
              total: evt.total ?? mids.length,
              currentName: targetMap.get(mids[0]),
            });
            if (mids.length > 0) {
              setActiveScrapeMid(mids[0]);
            }
            return;
          }
          if (evt.event === "item_start") {
            const currentMid = Number(evt.mid);
            setActiveScrapeMid(currentMid);
            setScrapeProgress({
              current: typeof evt.current === "number" ? evt.current : 0,
              total: typeof evt.total === "number" ? evt.total : mids.length,
              currentName: evt.name,
            });
            return;
          }
          if (evt.event === "progress") {
            setScrapeProgress((prev) => ({
              current: typeof evt.current === "number" ? evt.current : (prev?.current ?? 0),
              total: typeof evt.total === "number" ? evt.total : (prev?.total ?? mids.length),
              currentName: evt.name || prev?.currentName,
            }));
            if (evt.success) {
              matched += 1;
              const patch: Partial<BannerRecord> = {
                pictureSlide: evt.pictureSlide || "",
                tmdbId: evt.tmdbId,
                mediaType: evt.mediaType,
                pendingScrape: true,
                scrapeState: "pending",
              };
              if (evt.picture) {
                patch.picture = evt.picture;
                patch.poster = evt.picture;
              }
              if (evt.score) patch.score = evt.score;
              if (evt.blurb) patch.blurb = evt.blurb;
              if (evt.year) patch.year = evt.year;
              patchDeckFilm(Number(evt.mid), patch);
            } else if (evt.mid) {
              patchDeckFilm(Number(evt.mid), { pendingScrape: false, scrapeState: "miss" });
            }
            return;
          }
          if (evt.event === "done") {
            abortRef.current = null;
            setActiveScrapeMid(null);
            setScrapeProgress(null);
            const count = evt.successCount ?? matched;
            if (mids.length === 1) {
              if (count > 0) {
                message.success("刮削成功");
              } else {
                message.warning("未匹配到信息");
              }
            } else {
              message.success(count > 0 ? `已匹配 ${count} 部` : "未匹配到信息");
            }
            return;
          }
          if (evt.event === "error") {
            setActiveScrapeMid(null);
            message.error(evt.error || "刮削失败");
          }
        },
      });
    } catch (err: any) {
      if (cancelledRef.current || err?.name === "AbortError") {
        message.info("已取消");
      } else {
        message.error(err?.message || "刮削失败");
      }
    } finally {
      setScraping(false);
      setActiveScrapeMid(null);
      setScrapeProgress(null);
      if (abortRef.current === controller) abortRef.current = null;
    }
  };

  const handleBatchScrape = async () => {
    const targets = deckRef.current.filter(needsPreviewScrape);
    const mids = targets.map((item) => item.mid);
    if (mids.length === 0) {
      message.info("暂无待刮削影片");
      return;
    }
    await runScrape(mids);
  };

  const handleSingleScrape = (mid: number) => {
    if (busy) return;
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
      scrapeState: "pending",
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
      onClose={() => {
        if (scraping) stopScrape();
        onClose();
      }}
      size={840}
      destroyOnHidden
      mask={{ closable: !busy }}
      title={
        <Flex align="center" justify="space-between" style={{ width: "100%", paddingRight: 8 }}>
          <Space size={10} align="center">
            <CompassOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
            <span style={{ fontSize: 16, fontWeight: 600 }}>排片结果</span>
          </Space>
          <Tag color="success" icon={<CheckCircleOutlined />} style={{ borderRadius: 4, margin: 0 }}>
            待发布
          </Tag>
        </Flex>
      }
      footer={
        <Flex justify="space-between" align="center">
          <Button onClick={onRedo} disabled={busy}>
            重新排片
          </Button>
          <Space size={8}>
            <Button
              onClick={() => {
                if (scraping) stopScrape();
                onClose();
              }}
              disabled={publishing}
            >
              取消
            </Button>
            <Button
              type="primary"
              loading={publishing}
              disabled={busy || deck.length === 0}
              onClick={() => void handlePublish()}
              style={{ background: "#52c41a", borderColor: "#52c41a" }}
            >
              发布
            </Button>
          </Space>
        </Flex>
      }
    >
      <div className={styles.drawerStack}>
        <div className={styles.drawerSummaryBar}>
          <div className={styles.summaryLeft}>
            <span>
              总数: <strong>{deck.length}</strong>
            </span>
            <span>
              策略: <strong>{STRATEGY_LABEL[strategy]}</strong>
            </span>
            {scrapeTargets.length > 0 ? (
              <Tag color="purple" style={{ margin: 0, borderRadius: 4, fontSize: 12 }}>
                待刮削 {scrapeTargets.length}
              </Tag>
            ) : null}
          </div>
          <div className={styles.summaryRight}>
            {!tmdbEnabled ? (
              <Tag style={{ margin: 0, borderRadius: 4 }}>未启用 TMDB</Tag>
            ) : scraping ? (
              <div className={styles.summaryScrapeActive}>
                <Spin indicator={<LoadingOutlined style={{ fontSize: 13, color: "#722ed1" }} spin />} />
                <span className={styles.summaryScrapeProgress}>
                  刮削中 {scrapeProgress?.current ?? 0}/{scrapeProgress?.total ?? scrapeTargets.length}
                </span>
                {scrapeProgress?.currentName ? (
                  <span className={styles.summaryScrapeCurrentName} title={scrapeProgress.currentName}>
                    《{scrapeProgress.currentName}》
                  </span>
                ) : null}
                <Button
                  type="text"
                  size="small"
                  danger
                  className={styles.summaryScrapeCancelBtn}
                  onClick={stopScrape}
                >
                  取消
                </Button>
              </div>
            ) : scrapeTargets.length > 0 ? (
              <Button
                type="primary"
                size="small"
                className={styles.batchScrapeBtn}
                icon={<ThunderboltOutlined />}
                disabled={publishing}
                onClick={() => void handleBatchScrape()}
              >
                批量刮削 ({scrapeTargets.length})
              </Button>
            ) : (
              <Tag
                color="success"
                icon={<CheckCircleOutlined />}
                style={{ margin: 0, borderRadius: 4, fontSize: 12 }}
              >
                全部已刮削
              </Tag>
            )}
          </div>
        </div>
        {deck.length === 0 ? (
          <Alert type="warning" showIcon title="没有可排片的影片" style={{ borderRadius: 8 }} />
        ) : null}
        <div className={styles.previewDeckGrid}>
          {deck.map((item, idx) => (
            <BannerQuickCard
              key={item.id || item.mid || idx}
              item={item}
              index={idx}
              total={deck.length}
              isScrapingThis={scraping && activeScrapeMid === item.mid}
              isAnyBusy={busy}
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
