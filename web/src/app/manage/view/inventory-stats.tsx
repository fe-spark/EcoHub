"use client";

import React, { useEffect, useRef, useState } from "react";
import { Card, Spin, Tag } from "antd";
import { DownOutlined } from "@ant-design/icons";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
import styles from "./inventory-stats.module.less";

interface LibraryInventory {
  films: number;
  playable: number;
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
  library: LibraryInventory;
  sources: SourceInventory[];
}

const metricItems: Array<{ key: keyof LibraryInventory; label: string; hint: string; lead?: boolean }> = [
  { key: "films", label: "片库影片", hint: "全部采集站入库", lead: true },
  { key: "playable", label: "有播放线路", hint: "至少一条播放线路" },
  { key: "categories", label: "展示分类", hint: "共享展示树" },
  { key: "failures", label: "失败记录", hint: "全部来源" },
];

function formatCount(value: number | undefined) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "—";
  }
  return value.toLocaleString("zh-CN");
}

function placePrimaryFirst(sources: SourceInventory[], id: string) {
  const index = sources.findIndex((item) => item.id === id);
  const next = sources.map((item) => ({ ...item, isPrimary: item.id === id }));
  if (index <= 0) {
    return next;
  }
  const [chosen] = next.splice(index, 1);
  return [chosen, ...next];
}

export default function InventoryStats() {
  const [library, setLibrary] = useState<LibraryInventory | null>(null);
  const [libraryLoading, setLibraryLoading] = useState(true);
  const [sources, setSources] = useState<SourceInventory[] | null>(null);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const [sourcesLoading, setSourcesLoading] = useState(false);
  const [sourcesFailed, setSourcesFailed] = useState(false);
  const primaryIdRef = useRef("");
  const sourcesLoadedRef = useRef(false);

  useEffect(() => {
    const onPrimary = (event: Event) => {
      const id = (event as CustomEvent<string>).detail;
      if (!id) {
        return;
      }
      primaryIdRef.current = id;
      setSources((prev) => (prev ? placePrimaryFirst(prev, id) : prev));
    };
    window.addEventListener("ecohub:primary-source", onPrimary);
    return () => window.removeEventListener("ecohub:primary-source", onPrimary);
  }, []);

  useEffect(() => {
    let active = true;
    ApiGet<InventoryStatsData>("/manage/spider/clear/stats?scope=library")
      .then((resp) => {
        if (active && resp.code === 0 && resp.data?.library) {
          setLibrary(resp.data.library);
        }
      })
      .catch(() => {})
      .finally(() => {
        if (active) {
          setLibraryLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  useEffect(() => {
    if (!sourcesOpen || sourcesLoadedRef.current) {
      return;
    }
    let active = true;
    setSourcesLoading(true);
    setSourcesFailed(false);
    ApiGet<InventoryStatsData>("/manage/spider/clear/stats?scope=sources")
      .then((resp) => {
        if (!active) {
          return;
        }
        if (resp.code !== 0 || !resp.data) {
          setSourcesFailed(true);
          return;
        }
        const id = primaryIdRef.current;
        const next = resp.data.sources ?? [];
        setSources(id ? placePrimaryFirst(next, id) : next);
        sourcesLoadedRef.current = true;
      })
      .catch(() => {
        if (active) {
          setSourcesFailed(true);
        }
      })
      .finally(() => {
        if (active) {
          setSourcesLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [sourcesOpen]);

  return (
    <Card
      className={styles.panel}
      classNames={{ header: styles.header, body: styles.body, title: styles.title }}
      title="片库规模"
      extra={
        <Link href="/manage/film" className={styles.filmLink}>
          影片列表
        </Link>
      }
    >
      {libraryLoading ? (
        <div className={styles.loading} role="status" aria-busy="true">
          <Spin />
        </div>
      ) : (
        <div className={styles.metrics}>
          {metricItems.map((item) => (
            <div key={item.key} className={`${styles.metric} ${item.lead ? styles.metricLead : ""}`}>
              <div className={styles.metricValue}>{formatCount(library?.[item.key])}</div>
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
          onClick={() =>
            setSourcesOpen((open) => {
              const next = !open;
              if (next && !sourcesLoadedRef.current) {
                setSourcesLoading(true);
                setSourcesFailed(false);
              }
              return next;
            })
          }
        >
          <span className={styles.stationLead}>
            <span className={styles.chevronWrap} aria-hidden="true">
              <DownOutlined className={`${styles.chevron} ${sourcesOpen ? styles.chevronOpen : ""}`} />
            </span>
            <span className={styles.stationCopy}>
              <span className={styles.stationTitle}>各采集站</span>
              <span className={styles.stationHint}>
                {sourcesOpen ? "已展开各站影片、分类和失败" : "展开后查看各站影片、分类和失败"}
              </span>
            </span>
          </span>
          <span className={styles.stationAction}>{sourcesOpen ? "收起" : "展开"}</span>
        </button>
        {sourcesOpen ? (
          sourcesLoading ? (
            <div className={styles.loading} role="status" aria-busy="true">
              <Spin />
            </div>
          ) : sourcesFailed || sources === null ? (
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
                      {row.isPrimary ? <Tag color="gold">首选站</Tag> : null}
                      {!row.enabled ? <Tag>已停用</Tag> : null}
                    </div>
                    <div className={styles.stationStats}>
                      <div className={styles.stationStat}>
                        <span className={styles.stationStatLabel}>影片</span>
                        <span className={styles.stationStatValue}>{formatCount(row.films)}</span>
                      </div>
                      <div className={styles.stationStat}>
                        <span className={styles.stationStatLabel}>分类</span>
                        <span className={styles.stationStatValue}>
                          {row.categories === 0 ? <Tag color="warning">缺分类</Tag> : formatCount(row.categories)}
                        </span>
                      </div>
                      <div className={styles.stationStat}>
                        <span className={styles.stationStatLabel}>失败</span>
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
