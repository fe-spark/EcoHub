"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Card, Descriptions, Select, Statistic, Tag, Typography } from "antd";
import Link from "next/link";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import type { FilmSource } from "./types";
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
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd > 0 ? item.cd : 24),
    lastCollectTime: item.lastCollectTime,
    progress: item.progress ?? null,
    createdAt: item.createdAt,
  };
}

/** 工作台：运行概览 + 优先采集站（进入页面拉取一次） */
function orderBySort(list: FilmSource[]) {
  return [...list].sort((a, b) => (a.sort ?? 0) - (b.sort ?? 0) || a.id.localeCompare(b.id));
}

export default function CollectOverview() {
  const { message, modal } = useAppMessage();
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [loading, setLoading] = useState(true);
  const [switching, setSwitching] = useState(false);
  const mountedRef = useRef(false);

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

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      disabled: siteList.filter((item) => !item.state).length,
    }),
    [siteList],
  );

  const topSite = useMemo(
    () => orderBySort(siteList).find((item) => item.state) ?? null,
    [siteList],
  );
  const switchCandidates = useMemo(
    () => orderBySort(siteList).filter((item) => item.state && item.id !== topSite?.id),
    [siteList, topSite?.id],
  );

  const askSwitchPrimary = (sourceId: string) => {
    if (!sourceId || sourceId === topSite?.id || switching) {
      return;
    }
    const chosen = siteList.find((item) => item.id === sourceId && item.state);
    if (!chosen) {
      return;
    }
    modal.confirm({
      title: "切换首选站？",
      content: `首选站改为「${chosen.name}」`,
      okText: "切换",
      cancelText: "取消",
      centered: true,
      onOk: () => switchPrimary(sourceId),
    });
  };

  const switchPrimary = async (sourceId: string) => {
    const chosen = siteList.find((item) => item.id === sourceId && item.state);
    if (!chosen || switching) {
      return;
    }
    const previous = siteList;
    const next = [chosen, ...orderBySort(siteList).filter((item) => item.id !== chosen.id)].map((item, index) => ({
      ...item,
      sort: index,
      isPrimary: item.id === chosen.id,
    }));
    setSiteList(next);
    setSwitching(true);
    try {
      const resp = await ApiPost("/manage/collect/sort", { ids: next.map((item) => item.id) });
      if (!mountedRef.current) {
        return;
      }
      if (resp.code !== 0) {
        setSiteList(previous);
        message.error(resp.msg || "切换首选站失败");
        return;
      }
      window.dispatchEvent(new CustomEvent("ecohub:primary-source", { detail: chosen.id }));
      message.success(`首选站：${chosen.name}`);
    } catch {
      if (mountedRef.current) {
        setSiteList(previous);
        message.error("切换首选站失败");
      }
    } finally {
      if (mountedRef.current) {
        setSwitching(false);
      }
    }
  };

  return (
    <div className={styles.overviewGrid}>
      <Card
        title="运行概览"
        loading={loading && siteList.length === 0}
        className={styles.summaryCard}
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
        title="当前首选站"
        loading={loading && siteList.length === 0}
        className={styles.summaryCard}
        extra={
          topSite ? <Tag color="gold">首选站</Tag> : <Tag color="warning">未配置</Tag>
        }
      >
        {topSite ? (
          <Descriptions column={1} size="small" className={styles.masterDescriptions}>
            <Descriptions.Item label="名称">
              {switchCandidates.length > 0 ? (
                <Select
                  className={styles.sourceSelect}
                  size="middle"
                  value={topSite.id}
                  loading={switching}
                  disabled={switching}
                  popupMatchSelectWidth={false}
                  options={orderBySort(siteList)
                    .filter((item) => item.state)
                    .map((item) => ({ value: item.id, label: item.name }))}
                  onChange={(sourceId) => {
                    askSwitchPrimary(sourceId);
                  }}
                />
              ) : (
                topSite.name
              )}
            </Descriptions.Item>
            <Descriptions.Item label="接口地址">
              <Typography.Link
                href={topSite.uri}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.masterLink}
              >
                {topSite.uri}
              </Typography.Link>
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
