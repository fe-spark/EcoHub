"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Card, Descriptions, Statistic, Tag, Tooltip, Typography } from "antd";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
import { usePrimarySource } from "@/lib/manage-primary";
import { type FilmSource } from "./types";
import styles from "./collect-overview.module.less";

interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
}

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    state: Boolean(item.state),
    sort: Number(item.sort ?? 0),
    isPrimary: Boolean(item.isPrimary),
    cd: Number(item.cd > 0 ? item.cd : 24),
    lastRunTime: item.lastRunTime,
    lastRunPageCount: item.lastRunPageCount ? Number(item.lastRunPageCount) : 0,
    sitePageTotal: item.sitePageTotal ? Number(item.sitePageTotal) : 0,
    progress: item.progress ?? null,
    createdAt: item.createdAt,
  };
}

/** 工作台：运行概览 + 当前主站（进入页面拉取一次） */
interface CollectOverviewProps {
  onPrimaryChange?: (primary: FilmSource | null) => void;
}

export default function CollectOverview({ onPrimaryChange }: CollectOverviewProps = {}) {
  const { primary, loading: primaryLoading } = usePrimarySource();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(false);
  const prevPrimaryIdRef = useRef<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/collect/list");
      if (!mountedRef.current) {
        return;
      }
      if (resp.code === 0 && Array.isArray(resp.data)) {
        setSiteList(resp.data.map((item: CollectListItemResponse) => normalizeSource(item)));
      }
    } catch {
      // 工作台失败时保持上次数据，不打断入口区
    } finally {
      if (mountedRef.current) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    void load();
    return () => {
      mountedRef.current = false;
    };
  }, [load]);

  useEffect(() => {
    if (primaryLoading) return;
    const currentId = primary?.id || "empty";
    if (prevPrimaryIdRef.current === null) {
      prevPrimaryIdRef.current = currentId;
      return;
    }
    if (prevPrimaryIdRef.current !== currentId) {
      prevPrimaryIdRef.current = currentId;
      void load();
    }
  }, [load, primary?.id, primaryLoading]);

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      disabled: siteList.filter((item) => !item.state).length,
    }),
    [siteList],
  );

  // 服务端标记的主站；主站为持久化状态，排序不改变主站
  const topSite = useMemo(
    () => siteList.find((item) => item.isPrimary) ?? null,
    [siteList],
  );

  useEffect(() => {
    if (!loading) {
      onPrimaryChange?.(topSite);
    }
  }, [topSite, loading, onPrimaryChange]);

  const summaryCardClassNames = {
    header: styles.summaryCardHead,
    title: styles.summaryCardTitle,
    body: styles.summaryCardBody,
  };

  return (
    <div className={styles.overviewGrid}>
      <Card
        title="运行概览"
        loading={loading && siteList.length === 0}
        className={styles.summaryCard}
        classNames={summaryCardClassNames}
        extra={
          <Link href="/manage/collect" style={{ fontSize: 13, color: "#fa8c16" }}>
            采集中心
          </Link>
        }
      >
        <div className={styles.overviewRow}>
          <div className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="采集站总数" value={stats.total} />
            </div>
          </div>
          <div className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="已启用" value={stats.enabled} />
            </div>
          </div>
          <div className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="已停用" value={stats.disabled} />
            </div>
          </div>
        </div>
      </Card>

      <Card
        title="当前主站"
        loading={loading && siteList.length === 0}
        className={styles.summaryCard}
        classNames={summaryCardClassNames}
        extra={
          topSite ? <Tag color="gold">主站</Tag> : <Tag color="warning">未配置</Tag>
        }
      >
        {topSite ? (
          <Descriptions column={1} size="small" className={styles.masterDescriptions}>
            <Descriptions.Item label="名称">{topSite.name}</Descriptions.Item>
            <Descriptions.Item label="接口地址">
              <Tooltip title={topSite.uri}>
                <Typography.Link
                  href={topSite.uri}
                  target="_blank"
                  rel="noopener noreferrer"
                  className={styles.masterLink}
                >
                  {topSite.uri}
                </Typography.Link>
              </Tooltip>
            </Descriptions.Item>
            <Descriptions.Item label="启用状态">
              <Tag color={topSite.state ? "success" : "default"} variant="filled">
                {topSite.state ? "启用中" : "已停用"}
              </Tag>
            </Descriptions.Item>
          </Descriptions>
        ) : (
          <Descriptions column={1} size="small">
            <Descriptions.Item label="状态">
              <Tag color="warning">未配置</Tag>
            </Descriptions.Item>
            <Descriptions.Item label="说明">
              需要先{" "}
              <Link href="/manage/collect">添加采集站</Link>
            </Descriptions.Item>
          </Descriptions>
        )}
      </Card>
    </div>
  );
}
