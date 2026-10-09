"use client";

import React, { useEffect, useRef, useState } from "react";
import { Card, Spin, Tag } from "antd";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
import { useManagePermission } from "@/lib/manage-permission";
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
  const { isAdmin } = useManagePermission();
  const [stats, setStats] = useState<InventoryStatsData | null>(null);
  const [loading, setLoading] = useState(true);
  const primaryIdRef = useRef("");

  useEffect(() => {
    const onPrimary = (event: Event) => {
      const id = (event as CustomEvent<string>).detail;
      if (!id) {
        return;
      }
      primaryIdRef.current = id;
      setStats((prev) => {
        if (!prev) {
          return prev;
        }
        return { ...prev, sources: placePrimaryFirst(prev.sources, id) };
      });
    };
    window.addEventListener("ecohub:primary-source", onPrimary);
    return () => window.removeEventListener("ecohub:primary-source", onPrimary);
  }, []);

  useEffect(() => {
    let active = true;
    ApiGet<InventoryStatsData>("/manage/spider/clear/stats")
      .then((resp) => {
        if (active && resp.code === 0 && resp.data) {
          const id = primaryIdRef.current;
          setStats({
            ...resp.data,
            sources: id ? placePrimaryFirst(resp.data.sources, id) : resp.data.sources,
          });
        }
      })
      .catch(() => {})
      .finally(() => {
        if (active) {
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, []);

  const sources = stats?.sources ?? [];

  return (
    <Card
      className={styles.panel}
      classNames={{ header: styles.header, body: styles.body, title: styles.title }}
      title="片库规模"
      extra={
        isAdmin ? (
          <Link href="/manage/system?tab=security" className={styles.securityLink}>
            数据安全
          </Link>
        ) : null
      }
    >
      {loading ? (
        <div className={styles.loading} role="status" aria-busy="true">
          <Spin />
        </div>
      ) : (
        <>
          <div className={styles.metrics}>
            {metricItems.map((item) => (
              <div key={item.key} className={`${styles.metric} ${item.lead ? styles.metricLead : ""}`}>
                <div className={styles.metricValue}>{formatCount(stats?.library?.[item.key])}</div>
                <div className={styles.metricLabel}>{item.label}</div>
                <div className={styles.metricHint}>{item.hint}</div>
              </div>
            ))}
          </div>

          <section className={styles.stations} aria-label="各采集站">
            <div className={styles.stationTitle}>各采集站</div>
            {sources.length === 0 ? (
              <div className={styles.stationEmpty}>{stats ? "还没有采集站" : "统计失败"}</div>
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
            )}
          </section>
        </>
      )}
    </Card>
  );
}
