"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Card, Col, Descriptions, Row, Statistic, Tag, Typography } from "antd";
import Link from "next/link";
import { ApiGet } from "@/lib/client-api";
import type { FilmSource } from "./types";
import styles from "./collect-overview.module.less";

interface CollectListItemResponse extends Partial<FilmSource> {
  id: string;
  name: string;
  uri: string;
}

const POLL_INTERVAL = 8000;

function normalizeSource(item: CollectListItemResponse): FilmSource {
  return {
    id: item.id,
    name: item.name,
    uri: item.uri,
    syncPictures: Boolean(item.syncPictures),
    state: Boolean(item.state),
    grade: Number(item.grade ?? 1),
    interval: Number(item.interval ?? 0),
    cd: Number(item.cd ?? 24),
    lastCollectTime: item.lastCollectTime,
    progress: item.progress ?? null,
  };
}

/** 工作台：运行概览 + 当前主采集站（自拉取列表，轻量轮询） */
export default function CollectOverview() {
  const [siteList, setSiteList] = useState<FilmSource[]>([]);
  const [loading, setLoading] = useState(true);
  const mountedRef = useRef(false);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const load = useCallback(async (silent = false) => {
    if (!silent) {
      setLoading(true);
    }
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
      if (mountedRef.current && !silent) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    mountedRef.current = true;
    void load();
    const tick = () => {
      timerRef.current = setTimeout(() => {
        void load(true).finally(() => {
          if (mountedRef.current) {
            tick();
          }
        });
      }, POLL_INTERVAL);
    };
    tick();
    return () => {
      mountedRef.current = false;
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [load]);

  const stats = useMemo(
    () => ({
      total: siteList.length,
      enabled: siteList.filter((item) => item.state).length,
      running: siteList.filter((item) => item.progress?.status === "running").length,
      waiting: siteList.filter(
        (item) =>
          item.progress?.status === "starting" ||
          item.progress?.status === "page_done" ||
          item.progress?.status === "waiting_publish" ||
          item.progress?.status === "finalizing",
      ).length,
      masters: siteList.filter((item) => item.grade === 0).length,
    }),
    [siteList],
  );

  const masterSite = useMemo(
    () => siteList.find((item) => item.grade === 0) ?? null,
    [siteList],
  );

  const masterStatus = useMemo(() => {
    if (stats.masters === 1) {
      return { text: "正常", color: "success" as const };
    }
    if (stats.masters === 0) {
      return { text: "缺少主采集站", color: "warning" as const };
    }
    return { text: `${stats.masters} 个主采集站`, color: "error" as const };
  }, [stats.masters]);

  return (
    <div className={styles.overviewGrid}>
      <Card
        size="small"
        title="运行概览"
        loading={loading && siteList.length === 0}
        className={styles.summaryCard}
        extra={
          <Link href="/manage/collect" style={{ fontSize: 13 }}>
            采集中心
          </Link>
        }
        styles={{ body: { height: "100%" } }}
      >
        <Row gutter={[16, 16]} className={styles.overviewRow}>
          <Col xs={12} sm={8} lg={4} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="采集站总数" value={stats.total} />
            </div>
          </Col>
          <Col xs={12} sm={8} lg={4} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="已启用" value={stats.enabled} />
            </div>
          </Col>
          <Col xs={12} sm={8} lg={4} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="采集中" value={stats.running} />
            </div>
          </Col>
          <Col xs={12} sm={8} lg={4} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic title="收尾/排队" value={stats.waiting} />
            </div>
          </Col>
          <Col xs={12} sm={8} lg={4} className={styles.overviewCol}>
            <div className={styles.overviewStat}>
              <Statistic
                title="主采集站"
                value={stats.masters}
                suffix={<Tag color={masterStatus.color}>{masterStatus.text}</Tag>}
              />
            </div>
          </Col>
        </Row>
      </Card>

      <Card
        size="small"
        title="当前主采集站"
        loading={loading && siteList.length === 0}
        className={styles.summaryCard}
        styles={{ body: { height: "100%" } }}
        extra={masterSite ? <Tag color="gold">已生效</Tag> : <Tag color="error">未配置</Tag>}
      >
        {masterSite ? (
          <Descriptions column={1} size="small" className={styles.masterDescriptions}>
            <Descriptions.Item label="名称">{masterSite.name}</Descriptions.Item>
            <Descriptions.Item label="接口地址">
              <Typography.Link
                href={masterSite.uri}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.masterLink}
              >
                {masterSite.uri}
              </Typography.Link>
            </Descriptions.Item>
            <Descriptions.Item label="启用状态">
              <Tag color={masterSite.state ? "success" : "default"} bordered={false}>
                {masterSite.state ? "启用中" : "已停用"}
              </Tag>
            </Descriptions.Item>
            <Descriptions.Item label="图片同步">
              <Tag color={masterSite.syncPictures ? "processing" : "default"} bordered={false}>
                {masterSite.syncPictures ? "开启" : "关闭"}
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
              <Link href="/manage/collect">配置主采集站</Link>
            </Descriptions.Item>
          </Descriptions>
        )}
      </Card>
    </div>
  );
}
