"use client";

import React from "react";
import { Tag } from "antd";
import {
  CloudServerOutlined,
  VideoCameraOutlined,
  ClockCircleOutlined,
  FieldTimeOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import { CollectHistoryRecord, CollectRecordSourceDetail } from "./types";
import { formatDuration } from "./record-format";
import {
  renderHourTag,
  renderTriggerTag,
  resolveBatchStatus,
} from "./history-columns";
import styles from "./batch-overview-cards.module.less";

export interface BatchOverviewCardsProps {
  record: CollectHistoryRecord;
  details: CollectRecordSourceDetail[];
  onOpenFilmsModal: () => void;
}

type MetricTone = "success" | "warning" | "danger" | "muted";

interface OverviewMetric {
  key: string;
  label: string;
  icon?: React.ReactNode;
  tag?: React.ReactNode;
  value: string;
  unit?: string;
  hint?: React.ReactNode;
  tone?: MetricTone;
  onClick?: () => void;
}

function isBatchOngoing(status: string | undefined): boolean {
  return status === "running";
}

function formatNum(value: number): string {
  return value.toLocaleString("zh-CN");
}

function statusTag(color: string, text: string) {
  return (
    <Tag color={color} bordered={false} className={styles.metricTag}>
      {text}
    </Tag>
  );
}

function toneClass(tone: MetricTone | undefined): string {
  if (tone === "success") return styles.toneSuccess;
  if (tone === "warning") return styles.toneWarning;
  if (tone === "danger") return styles.toneDanger;
  if (tone === "muted") return styles.toneMuted;
  return "";
}

function MetricCell({ metric }: { metric: OverviewMetric }) {
  const value = metric.onClick ? (
    <a className={styles.metricValueLink} onClick={metric.onClick}>
      {metric.value}
    </a>
  ) : (
    <span className={`${styles.metricValue} ${toneClass(metric.tone)}`.trim()}>{metric.value}</span>
  );

  return (
    <div className={styles.metricCell}>
      <div className={styles.metricLabel}>
        <span className={styles.metricLabelMain}>
          {metric.icon}
          <span>{metric.label}</span>
        </span>
        {metric.tag}
      </div>
      <div className={styles.metricValueRow}>
        {value}
        {metric.unit ? <span className={styles.metricUnit}>{metric.unit}</span> : null}
      </div>
      <div className={styles.metricHint}>{metric.hint}</div>
    </div>
  );
}

function buildSiteMetric(
  record: CollectHistoryRecord,
  abnormalCount: number,
  stoppedSources: CollectRecordSourceDetail[],
  doneSources: CollectRecordSourceDetail[],
  interruptedCount: number,
): OverviewMetric {
  const isOngoing = isBatchOngoing(record.status);
  const isStopped = record.status === "stopped";
  const isInterrupted = record.status === "interrupted";
  const isFailed = resolveBatchStatus(record) === "failed";
  const isOk =
    abnormalCount === 0 &&
    stoppedSources.length === 0 &&
    record.failedSources === 0 &&
    record.pageFailed === 0 &&
    !isOngoing &&
    !isStopped &&
    !isInterrupted;

  let tag: React.ReactNode = null;
  let hint = "";

  if (isOngoing) {
    tag = statusTag("processing", "进行中");
    hint = "尚未结算";
  } else if (isOk) {
    tag = statusTag("success", "成功");
    hint = "全部完成";
  } else if (isFailed) {
    tag = statusTag("error", "失败");
    hint = "未能完成采集";
  } else if (isInterrupted) {
    tag = statusTag("error", "异常中断");
    hint = interruptedCount > 0
      ? `${interruptedCount} 站中断`
      : `${doneSources.length} / ${record.sourceCount} 站成功`;
  } else if (isStopped) {
    tag = statusTag("default", "已停止");
    hint = `${doneSources.length} / ${record.sourceCount} 站成功`;
  } else {
    const abnormal = abnormalCount || record.failedSources || 1;
    tag = statusTag("warning", `${abnormal} 站未完成`);
    hint = `${doneSources.length} / ${record.sourceCount} 站成功`;
  }

  return {
    key: "sites",
    label: "覆盖站点",
    icon: <CloudServerOutlined className={styles.metricIcon} />,
    tag,
    value: formatNum(record.sourceCount),
    unit: "站点",
    hint,
  };
}

function buildFilmMetric(record: CollectHistoryRecord, onOpenModal: () => void): OverviewMetric {
  const count = Math.max(0, record.filmCount ?? 0);
  const isOngoing = isBatchOngoing(record.status);
  const canOpen = record.ledger === true;
  const openHint = canOpen ? (
    <a className={styles.metricHintLink} onClick={onOpenModal}>
      查看清单
    </a>
  ) : null;

  if (isOngoing) {
    return {
      key: "films",
      label: "更新影片",
      icon: <VideoCameraOutlined className={styles.metricIcon} />,
      tag: statusTag("processing", count > 0 ? "有更新" : "进行中"),
      value: count > 0 ? `+${formatNum(count)}` : "0",
      unit: "部",
      hint: "更新中",
      tone: count > 0 ? undefined : "muted",
    };
  }

  if (count > 0) {
    return {
      key: "films",
      label: "更新影片",
      icon: <VideoCameraOutlined className={styles.metricIcon} />,
      tag: statusTag("cyan", "已更新"),
      value: `+${formatNum(count)}`,
      unit: "部",
      hint: openHint,
      onClick: canOpen ? onOpenModal : undefined,
    };
  }

  const unsettled = record.status === "interrupted";
  return {
    key: "films",
    label: "更新影片",
    icon: <VideoCameraOutlined className={styles.metricIcon} />,
    tag: statusTag("default", unsettled ? "未结算" : "无更新"),
    value: "0",
    unit: "部",
    hint: unsettled ? "未结算" : "无影片更新",
    tone: "muted",
  };
}

export function BatchOverviewCards({ record, details, onOpenFilmsModal }: BatchOverviewCardsProps) {
  const failedSources = details.filter((d) => d.status === "failed");
  const partialSources = details.filter((d) => d.status === "partial");
  const stoppedSources = details.filter((d) => d.status === "stopped");
  const interruptedSources = details.filter((d) => d.status === "interrupted");
  const doneSources = details.filter((d) => d.status === "done" || d.status === "success");
  const abnormalCount = failedSources.length + partialSources.length;
  const isOngoing = isBatchOngoing(record.status);
  const siteMetric = buildSiteMetric(record, abnormalCount, stoppedSources, doneSources, interruptedSources.length);
  const metrics = isOngoing ? [siteMetric] : [buildFilmMetric(record, onOpenFilmsModal), siteMetric];

  const effectiveMs =
    record.durationMs > 0
      ? record.durationMs
      : record.finishedAt && record.startedAt
        ? Math.max(0, dayjs(record.finishedAt).diff(dayjs(record.startedAt)))
        : isOngoing && record.startedAt
          ? Math.max(0, dayjs().diff(dayjs(record.startedAt)))
          : 0;

  return (
    <div className={styles.drawerOverviewCard}>
      <div className={styles.drawerMetaStrip}>
        <div className={styles.drawerMetaItem}>
          <span className={styles.drawerMetaLabel}>执行时间</span>
          <span className={styles.drawerMetaValue}>
            <ClockCircleOutlined className={styles.metaIcon} />
            {record.startedAt ? dayjs(record.startedAt).format("YYYY-MM-DD HH:mm:ss") : "-"}
          </span>
        </div>
        <div className={styles.drawerMetaItem}>
          <span className={styles.drawerMetaLabel}>采集方式</span>
          <div className={styles.drawerMetaValue}>
            {renderTriggerTag(record.trigger, record.tag)}
          </div>
        </div>
        <div className={styles.drawerMetaItem}>
          <span className={styles.drawerMetaLabel}>采集跨度</span>
          <div className={styles.drawerMetaValue}>
            {renderHourTag(record.hour, record)}
          </div>
        </div>
        <div className={styles.drawerMetaItem}>
          <span className={styles.drawerMetaLabel}>运行耗时</span>
          <span className={styles.drawerMetaValue}>
            <FieldTimeOutlined className={styles.metaIcon} />
            {formatDuration(effectiveMs)}
          </span>
        </div>
      </div>

      <div className={styles.metricGrid} data-count={metrics.length}>
        {metrics.map((metric) => (
          <MetricCell key={metric.key} metric={metric} />
        ))}
      </div>
    </div>
  );
}
