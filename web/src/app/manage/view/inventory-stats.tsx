"use client";

import React, { useEffect, useState } from "react";
import { Card, Spin } from "antd";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
import styles from "./inventory-stats.module.less";

interface LibraryInventory {
  films: number;
  categories: number;
  failures: number;
}

const metricItems: Array<{ key: keyof LibraryInventory; label: string; hint: string; lead?: boolean }> = [
  { key: "films", label: "片库影片", hint: "当前主站档案", lead: true },
  { key: "categories", label: "分类", hint: "已映射且分类还在" },
  { key: "failures", label: "采集失败页数", hint: "当天" },
];

function formatCount(value: number | undefined) {
  if (typeof value !== "number" || Number.isNaN(value)) {
    return "—";
  }
  return value.toLocaleString("zh-CN");
}

export default function InventoryStats({ primaryId }: { primaryId?: string }) {
  const [library, setLibrary] = useState<LibraryInventory | null>(null);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setFailed(false);
    ApiGet<LibraryInventory>("/manage/spider/clear/stats")
      .then((resp) => {
        if (!active) {
          return;
        }
        if (resp.code !== 0 || !resp.data) {
          setFailed(true);
          return;
        }
        setLibrary(resp.data);
      })
      .catch(() => {
        if (active) {
          setFailed(true);
        }
      })
      .finally(() => {
        if (active) {
          setLoading(false);
        }
      });
    return () => {
      active = false;
    };
  }, [primaryId]);

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
      {loading ? (
        <div className={styles.loading} role="status" aria-busy="true">
          <Spin />
        </div>
      ) : failed || library === null ? (
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
    </Card>
  );
}
