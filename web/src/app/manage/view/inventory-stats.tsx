"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button, Card, Spin, Tag } from "antd";
import { DownOutlined, ReloadOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./inventory-stats.module.less";

interface LibraryInventory {
  films: number;
  categories: number;
  failures: number;
}

interface SourceInventory {
  id: string;
  name: string;
  enabled: boolean;
  isPrimary: boolean;
  films: number;
  categories: number;
  failures: number;
}

interface InventoryStatsData {
  status: "ready" | "computing" | "collecting";
  library: LibraryInventory;
  sources: SourceInventory[];
}

type StatsPhase = "loading" | "collecting" | "computing" | "ready" | "error";

const POLL_INTERVAL_MS = 2000;

const metricItems: Array<{ key: keyof LibraryInventory; label: string; hint: string; lead?: boolean }> = [
  { key: "films", label: "片库影片", hint: "全部已启用站点合计", lead: true },
  { key: "categories", label: "分类", hint: "已启用站已映射且分类还在" },
  { key: "failures", label: "采集失败页数", hint: "当天已启用站点合计" },
];

function formatCount(value: number | undefined) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "—";
  }
  return value.toLocaleString("zh-CN");
}

function placePrimaryFirst(sources: SourceInventory[], id: string) {
  const next = sources.map((item) => ({ ...item, isPrimary: Boolean(id) && item.id === id }));
  if (!id) {
    return next;
  }
  const index = next.findIndex((item) => item.id === id);
  if (index <= 0) {
    return next;
  }
  const [chosen] = next.splice(index, 1);
  return [chosen, ...next];
}

export default function InventoryStats() {
  const { message } = useAppMessage();
  const { primary, loading: primaryLoading } = usePrimarySource();
  const primaryId = primary?.id || "";
  const [library, setLibrary] = useState<LibraryInventory | null>(null);
  const [sources, setSources] = useState<SourceInventory[] | null>(null);
  const [loading, setLoading] = useState(true);
  const [phase, setPhase] = useState<StatsPhase>("loading");
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const requestSeqRef = useRef(0);
  const primaryIdRef = useRef(primaryId);
  const hasLoadedRef = useRef(false);
  const pollTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const unmountedRef = useRef(false);

  primaryIdRef.current = primaryId;

  const clearPollTimer = useCallback(() => {
    if (pollTimerRef.current !== null) {
      clearTimeout(pollTimerRef.current);
      pollTimerRef.current = null;
    }
  }, []);

  const loadStats = useCallback(async (forceRefresh = false) => {
    clearPollTimer();
    const seq = ++requestSeqRef.current;
    setLoading(true);
    try {
      const params = forceRefresh ? { refresh: 1 } : undefined;
      const resp = await ApiGet<InventoryStatsData>("/manage/spider/clear/stats", params, 10000);
      if (unmountedRef.current || seq !== requestSeqRef.current) {
        return;
      }
      if (resp.code !== 0 || !resp.data) {
        if (hasLoadedRef.current) {
          setPhase("ready");
          message.error(resp.msg || "片库统计刷新失败，已保留当前数据");
        } else {
          setPhase("error");
        }
        return;
      }
      if (resp.data.status === "collecting") {
        // 采集中不展示过期数字，直接切换为数据入库中并轮询等待采集结束
        hasLoadedRef.current = false;
        setLibrary(null);
        setSources(null);
        setPhase("collecting");
        pollTimerRef.current = setTimeout(() => {
          void loadStats(false);
        }, POLL_INTERVAL_MS);
        return;
      }
      if (resp.data.status === "computing") {
        setPhase("computing");
        pollTimerRef.current = setTimeout(() => {
          void loadStats(false);
        }, POLL_INTERVAL_MS);
        return;
      }
      if (!resp.data.library) {
        if (hasLoadedRef.current) {
          setPhase("ready");
          message.error(resp.msg || "片库统计刷新失败，已保留当前数据");
        } else {
          setPhase("error");
        }
        return;
      }
      const id = primaryIdRef.current;
      const nextSources = resp.data.sources ?? [];
      hasLoadedRef.current = true;
      setPhase("ready");
      setLibrary(resp.data.library);
      setSources(id ? placePrimaryFirst(nextSources, id) : nextSources);
    } catch {
      if (unmountedRef.current || seq !== requestSeqRef.current) {
        return;
      }
      if (hasLoadedRef.current) {
        setPhase("ready");
        message.error("片库统计刷新失败，已保留当前数据");
      } else {
        setPhase("error");
      }
    } finally {
      if (!unmountedRef.current && seq === requestSeqRef.current) {
        setLoading(false);
      }
    }
  }, [clearPollTimer, message]);

  useEffect(() => {
    unmountedRef.current = false;
    void loadStats(false);
    return () => {
      unmountedRef.current = true;
      clearPollTimer();
    };
  }, [clearPollTimer, loadStats]);

  useEffect(() => {
    if (primaryLoading) {
      return;
    }
    // 切换主站不影响各站真实片库规模，仅在本地重排主站标识与顺序，避免触发百万级全表重查
    setSources((prev) => (prev ? placePrimaryFirst(prev, primaryId) : prev));
  }, [primaryId, primaryLoading]);

  const isCollecting = phase === "collecting";
  const isComputing = phase === "computing";
  const isBusy = loading || isCollecting || isComputing;
  const statusNotice = isCollecting
    ? "数据入库中，采集完成后将自动统计..."
    : isComputing
      ? "片库规模计算中，请稍候..."
      : "加载中...";
  const stationStatusNotice = isCollecting
    ? "数据入库中，采集完成后将统计各站规模..."
    : isComputing
      ? "各站规模计算中，请稍候..."
      : "加载中...";

  return (
    <Card
      className={styles.panel}
      classNames={{ header: styles.header, body: styles.body, title: styles.title }}
      title="片库规模"
      extra={
        <div className={styles.headerExtra}>
          {isCollecting ? <Tag color="warning">数据入库中</Tag> : null}
          {isComputing && library !== null ? <Tag color="processing">计算中</Tag> : null}
          <Button
            type="text"
            size="small"
            icon={<ReloadOutlined />}
            loading={isBusy}
            disabled={isCollecting}
            onClick={() => void loadStats(true)}
          >
            {isCollecting ? "入库中" : isComputing ? "计算中" : "刷新"}
          </Button>
        </div>
      }
    >
      {isCollecting || (isBusy && library === null) ? (
        <div className={styles.loading} role="status" aria-busy="true">
          <Spin />
          <span className={styles.loadingText}>{statusNotice}</span>
        </div>
      ) : library === null ? (
        <div className={styles.empty}>统计失败</div>
      ) : (
        <div className={styles.metrics}>
          {metricItems.map((item) => (
            <div key={item.key} className={`${styles.metric} ${item.lead ? styles.metricLead : ""}`}>
              <div className={styles.metricValue}>{formatCount(library[item.key])}</div>
              <div className={styles.metricLabel}>{item.label}</div>
              <div className={styles.metricHint}>{item.hint}</div>
            </div>
          ))}
        </div>
      )}

      <section className={styles.stations} aria-label="各采集站">
        <button
          type="button"
          className={`${styles.stationToggle} ${sourcesOpen ? styles.stationToggleOpen : ""}`}
          aria-expanded={sourcesOpen}
          onClick={() => setSourcesOpen((open) => !open)}
        >
          <span className={styles.stationLead}>
            <span className={styles.chevronWrap} aria-hidden="true">
              <DownOutlined className={`${styles.chevron} ${sourcesOpen ? styles.chevronOpen : ""}`} />
            </span>
            <span className={styles.stationCopy}>
              <span className={styles.stationTitle}>各采集站</span>
              <span className={styles.stationHint}>
                {sourcesOpen ? "各站真实已采集影片、分类和当天失败页" : "展开后查看各站真实已采集影片、分类和当天失败页"}
              </span>
            </span>
          </span>
          <span className={styles.stationAction}>{sourcesOpen ? "收起" : "展开"}</span>
        </button>
        {sourcesOpen ? (
          isCollecting || (isBusy && sources === null) ? (
            <div className={styles.loading} role="status" aria-busy="true">
              <Spin />
              <span className={styles.loadingText}>{stationStatusNotice}</span>
            </div>
          ) : sources === null ? (
            <div className={styles.stationEmpty}>统计失败</div>
          ) : sources.length === 0 ? (
            <div className={styles.stationEmpty}>还没有采集站</div>
          ) : (
            <div className={styles.stationGrid}>
              {sources.map((row) => (
                <article
                  key={row.id}
                  className={`${styles.stationCard} ${row.isPrimary ? styles.stationPrimary : ""} ${row.enabled ? "" : styles.stationOff}`}
                >
                  <div className={styles.identity}>
                    <span className={styles.sourceName}>{row.name}</span>
                    {row.isPrimary ? <Tag color="gold">主站</Tag> : null}
                    {!row.enabled ? <Tag>已停用</Tag> : null}
                  </div>
                  <div className={styles.stationStats}>
                    <div className={styles.stationStat}>
                      <span className={styles.stationStatLabel}>片库影片</span>
                      <span className={styles.stationStatValue}>{formatCount(row.films)}</span>
                    </div>
                    <div className={styles.stationStat}>
                      <span className={styles.stationStatLabel}>分类</span>
                      <span className={styles.stationStatValue}>
                        {row.categories === 0 ? <Tag color="warning">缺分类</Tag> : formatCount(row.categories)}
                      </span>
                    </div>
                    <div className={styles.stationStat}>
                      <span className={styles.stationStatLabel}>失败页</span>
                      <span className={`${styles.stationStatValue} ${row.failures > 0 ? styles.figureWarn : styles.figureQuiet}`}>
                        {formatCount(row.failures)}
                      </span>
                    </div>
                  </div>
                </article>
              ))}
            </div>
          )
        ) : null}
      </section>
    </Card>
  );
}
