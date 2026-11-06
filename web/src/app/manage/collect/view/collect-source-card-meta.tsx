import React, { useState } from "react";
import { Button, Select, Tag, Tooltip } from "antd";
import {
  GlobalOutlined,
  DisconnectOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { collectDuration } from "./types";
import type { FilmSource } from "./types";
import styles from "./index.module.less";

export interface CollectSourceCardMetaProps {
  record: FilmSource;
  canWrite: boolean;
  isDeleting: boolean;
  busy: boolean;
  deleting: boolean;
  hasProxy: boolean;
  globalSpiderProxyReady?: boolean;
  displaySuccessPages?: number;
  onChangeCollectDuration: (id: string, cd: number) => void;
  onUpdateSite?: (id: string, updater: (prev: FilmSource) => FilmSource) => void;
}

export default function CollectSourceCardMeta({
  record,
  canWrite,
  isDeleting,
  busy,
  deleting,
  hasProxy,
  globalSpiderProxyReady,
  displaySuccessPages,
  onChangeCollectDuration,
  onUpdateSite,
}: CollectSourceCardMetaProps) {
  const { message } = useAppMessage();
  const [refreshingPageTotal, setRefreshingPageTotal] = useState(false);

  const handleRefreshPageTotal = async (event: React.MouseEvent) => {
    event.stopPropagation();
    if (refreshingPageTotal) return;
    setRefreshingPageTotal(true);
    try {
      const resp = await ApiPost<{ pageTotal: number }>("/manage/collect/page-total", {
        id: record.id,
      });
      if (resp.code === 0 && resp.data) {
        const total = resp.data.pageTotal;
        onUpdateSite?.(record.id, (prev) => ({ ...prev, sitePageTotal: total }));
        message.success(`${record.name} 最新采集站总页：${total.toLocaleString()} 页`);
      } else {
        message.error(resp.msg || "获取最新总页失败");
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : "获取最新总页失败";
      message.error(msg);
    } finally {
      setRefreshingPageTotal(false);
    }
  };

  return (
    <div
      className={styles.cardMeta}
      onPointerDown={(event) => event.stopPropagation()}
      onClick={(event) => event.stopPropagation()}
    >
      <div className={styles.metaItem}>
        <dt className={styles.metaLabel}>上次采集：</dt>
        <dd
          className={`${styles.metaValue}${
            record.lastRunTime ? "" : ` ${styles.metaValueMuted}`
          }`}
        >
          {record.lastRunTime
            ? dayjs(record.lastRunTime).format("YYYY-MM-DD HH:mm")
            : "暂无"}
        </dd>
      </div>
      <div className={styles.metaItem}>
        <dt className={styles.metaLabel}>上次采集页：</dt>
        <dd
          className={`${styles.metaValue}${
            displaySuccessPages && displaySuccessPages > 0
              ? ""
              : ` ${styles.metaValueMuted}`
          }`}
        >
          {displaySuccessPages && displaySuccessPages > 0
            ? `${displaySuccessPages.toLocaleString()} 页`
            : "暂无"}
        </dd>
      </div>
      <div className={styles.metaItem}>
        <dt className={styles.metaLabel}>采集站总页：</dt>
        <dd
          className={styles.metaValue}
          style={{ display: "inline-flex", alignItems: "center", gap: 4 }}
        >
          <span
            className={
              record.sitePageTotal && record.sitePageTotal > 0
                ? ""
                : styles.metaValueMuted
            }
          >
            {record.sitePageTotal && record.sitePageTotal > 0
              ? `${record.sitePageTotal.toLocaleString()} 页`
              : "暂无"}
          </span>
          <Tooltip title="刷新获取该采集站最新全量总页数">
            <Button
              type="text"
              size="small"
              icon={<ReloadOutlined spin={refreshingPageTotal} />}
              loading={refreshingPageTotal}
              onClick={handleRefreshPageTotal}
              disabled={!canWrite || isDeleting}
              style={{
                width: 18,
                height: 18,
                padding: 0,
                fontSize: 11,
                minWidth: 18,
                color: "var(--ant-color-text-tertiary)",
              }}
            />
          </Tooltip>
        </dd>
      </div>
      <div className={styles.metaItem}>
        <dt className={styles.metaLabel}>网络代理：</dt>
        <dd className={styles.metaValue}>
          {hasProxy ? (
            <Tooltip
              title={
                globalSpiderProxyReady
                  ? "采集接口走系统网络代理"
                  : "代理未开启（系统网络代理或爬虫模块未启用）"
              }
            >
              <Tag
                icon={globalSpiderProxyReady ? <GlobalOutlined /> : <DisconnectOutlined />}
                color={globalSpiderProxyReady ? "cyan" : "default"}
                variant="filled"
                style={{ marginInlineEnd: 0 }}
              >
                代理
              </Tag>
            </Tooltip>
          ) : (
            <span className={styles.metaValueMuted}>直连</span>
          )}
        </dd>
      </div>
      <div className={styles.metaItem}>
        <dt className={styles.metaLabel}>采集时长：</dt>
        <dd className={styles.metaValue}>
          <Select
            size="small"
            value={record.cd}
            disabled={
              !canWrite ||
              busy ||
              deleting ||
              !record.state ||
              record.categoryReady === false ||
              Boolean(record.categorySyncing)
            }
            style={{ width: "100%" }}
            options={collectDuration.map((item) => ({ value: item.time, label: item.label }))}
            onChange={(value) => {
              onChangeCollectDuration(record.id, value);
            }}
          />
        </dd>
      </div>
    </div>
  );
}
