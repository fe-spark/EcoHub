"use client";

import React, { useEffect, useRef, useState } from "react";
import { Card, Spin, Tag } from "antd";
import { DownOutlined } from "@ant-design/icons";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
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
  library: LibraryInventory;
  sources: SourceInventory[];
}

const metricItems: Array<{ key: keyof LibraryInventory; label: string; hint: string; lead?: boolean }> = [
  { key: "films", label: "片库影片", hint: "全部采集站合计", lead: true },
  { key: "categories", label: "分类", hint: "各站已映射且分类还在" },
  { key: "failures", label: "采集失败页数", hint: "当天各站合计" },
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

export default function InventoryStats({ primaryId, pending = false }: { primaryId?: string; pending?: boolean }) {
  const [library, setLibrary] = useState<LibraryInventory | null>(null);
  const [libraryLoading, setLibraryLoading] = useState(true);
  const [libraryFailed, setLibraryFailed] = useState(false);
  const [sources, setSources] = useState<SourceInventory[] | null>(null);
  const [sourcesOpen, setSourcesOpen] = useState(false);
  const [sourcesLoading, setSourcesLoading] = useState(false);
  const [sourcesFailed, setSourcesFailed] = useState(false);
  const primaryIdRef = useRef(primaryId || "");
  const sourcesLoadedRef = useRef(false);
  const [libraryVersion, setLibraryVersion] = useState(0);

  useEffect(() => {
    if (pending) {
      return;
    }
    if (primaryId && primaryId !== primaryIdRef.current) {
      primaryIdRef.current = primaryId;
      setSources((prev) => (prev ? placePrimaryFirst(prev, primaryId) : prev));
      sourcesLoadedRef.current = false;
      setLibraryVersion((version) => version + 1);
    }
  }, [primaryId, pending]);

  useEffect(() => {
    if (pending) {
      return;
    }
    let active = true;
    setLibraryLoading(true);
    setLibraryFailed(false);
    ApiGet<InventoryStatsData>("/manage/spider/clear/stats", { scope: "library" }, 10000)
      .then((resp) => {
        if (!active) {
          return;
        }
        if (resp.code !== 0 || !resp.data?.library) {
          setLibraryFailed(true);
          return;
        }
        setLibrary(resp.data.library);
      })
      .catch(() => {
        if (active) {
          setLibraryFailed(true);
        }
      })
      .finally(() => {
        if (active) {
          setLibraryLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [libraryVersion, pending]);

  useEffect(() => {
    if (pending || !sourcesOpen || sourcesLoadedRef.current) {
      return;
    }
    let active = true;
    setSourcesLoading(true);
    setSourcesFailed(false);
    ApiGet<InventoryStatsData>("/manage/spider/clear/stats", { scope: "sources" }, 10000)
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
  }, [sourcesOpen, libraryVersion, pending]);

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
      {libraryLoading || pending ? (
        <div className={styles.loading} role="status" aria-busy="true">
          <Spin />
        </div>
      ) : libraryFailed || library === null ? (
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
                {sourcesOpen ? "各站片库影片、分类和当天失败页" : "展开后查看各站片库影片、分类和当天失败页"}
              </span>
            </span>
          </span>
          <span className={styles.stationAction}>{sourcesOpen ? "收起" : "展开"}</span>
        </button>
        {sourcesOpen ? (
          sourcesLoading || pending ? (
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
