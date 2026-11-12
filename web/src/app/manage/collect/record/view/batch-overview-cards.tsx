"use client";

import React from "react";
import { Tag } from "antd";
import {
  CloudServerOutlined,
  VideoCameraOutlined,
  ClockCircleOutlined,
  FileTextOutlined,
  FieldTimeOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import { CollectHistoryRecord, CollectRecordSourceDetail } from "./types";
import { formatDuration } from "./record-format";
import {
  isSingleFilmRecord,
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

interface PageCell {
  key: string;
  label: string;
  value: string;
  tone?: MetricTone;
}

interface PageBoard {
  tag?: React.ReactNode;
  cells: PageCell[];
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

function PageBoardView({ board }: { board: PageBoard }) {
  return (
    <div className={styles.pageBoard}>
      <div className={styles.metricLabel}>
        <span className={styles.metricLabelMain}>
          <FileTextOutlined className={styles.metricIcon} />
          <span>拉取页码</span>
        </span>
        {board.tag}
      </div>
      <div className={styles.pageBoardGrid} data-count={board.cells.length}>
        {board.cells.map((cell) => (
          <div key={cell.key} className={styles.pageCell}>
            <span className={styles.pageCellLabel}>{cell.label}</span>
            <span className={`${styles.metricValue} ${toneClass(cell.tone)}`.trim()}>{cell.value}</span>
          </div>
        ))}
      </div>
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
    tag = statusTag((record.pageFailed ?? 0) > 0 ? "error" : "processing", "采集中");
    hint = "尚未结算";
  } else if (isOk) {
    tag = statusTag("success", "全部成功");
    hint = "全部完成";
  } else if (isFailed) {
    tag = statusTag("error", "采集失败");
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

function buildPageView(record: CollectHistoryRecord): { summary?: OverviewMetric; board?: PageBoard } {
  if (isSingleFilmRecord(record)) {
    return {
      summary: {
        key: "pages",
        label: "拉取页码",
        icon: <FileTextOutlined className={styles.metricIcon} />,
        tag: statusTag("purple", "单片重放"),
        value: "1",
        unit: "部影片",
        hint: "单条记录重试",
      },
    };
  }

  const succ = Math.max(0, record.pageSuccess ?? 0);
  const total = Math.max(0, record.pageTotal ?? 0);
  const fail = Math.max(0, record.pageFailed ?? 0);
  const missed = total > 0 ? Math.max(0, total - succ - fail) : 0;
  const isOngoing = isBatchOngoing(record.status);
  const isStopped = record.status === "stopped";
  const isInterrupted = record.status === "interrupted";
  const isFailed = record.status === "failed";
  const percent = total > 0 ? Math.min(100, Math.floor((succ / total) * 100)) : 0;

  if (isOngoing && fail === 0) {
    return {
      summary: {
        key: "pages",
        label: "拉取页码",
        icon: <FileTextOutlined className={styles.metricIcon} />,
        tag: statusTag("processing", "拉取中"),
        value: formatNum(succ),
        unit: total > 0 ? `/ ${formatNum(total)} 页` : "页",
        hint: total > 0 ? `进度 ${percent}%` : "正在探测页码",
      },
    };
  }

  if (isFailed && succ === 0 && fail === 0) {
    return {
      summary: {
        key: "pages",
        label: "拉取页码",
        icon: <FileTextOutlined className={styles.metricIcon} />,
        tag: statusTag("error", "拉取失败"),
        value: "0",
        unit: total > 0 ? `/ ${formatNum(total)} 页` : "页",
        hint: total === 0 ? "探测失败" : undefined,
        tone: "danger",
      },
    };
  }

  const needsBoard = isOngoing || isStopped || isInterrupted || fail > 0 || missed > 0;
  if (!needsBoard) {
    return {
      summary: {
        key: "pages",
        label: "拉取页码",
        icon: <FileTextOutlined className={styles.metricIcon} />,
        tag: statusTag("success", "全部完成"),
        value: formatNum(total > 0 ? total : succ),
        unit: "页",
        hint: "全部拉取成功",
        tone: "success",
      },
    };
  }

  const cells: PageCell[] = [{ key: "succ", label: isOngoing ? "已拉" : "成功", value: formatNum(succ) }];
  if (fail > 0) cells.push({ key: "fail", label: "失败", value: formatNum(fail), tone: "danger" });
  if (!isOngoing && missed > 0) cells.push({ key: "miss", label: "未抓", value: formatNum(missed), tone: "warning" });
  if (total > 0) cells.push({ key: "total", label: "共", value: formatNum(total) });
  if (isOngoing && total > 0) cells.push({ key: "progress", label: "进度", value: `${percent}%` });

  let tag: React.ReactNode = null;
  if (isOngoing) tag = statusTag(fail > 0 ? "error" : "processing", "拉取中");
  else if (isInterrupted) tag = statusTag("error", "异常中断");
  else if (isStopped) tag = statusTag("default", "已停止");
  else if (fail > 0) tag = statusTag("warning", "有失败");
  else tag = statusTag("warning", "未抓完");

  if (cells.length < 2) {
    return {
      summary: {
        key: "pages",
        label: "拉取页码",
        icon: <FileTextOutlined className={styles.metricIcon} />,
        tag,
        value: cells[0]?.value ?? "0",
        unit: "页",
        tone: cells[0]?.tone,
      },
    };
  }

  return { board: { tag, cells } };
}

function buildFilmMetric(record: CollectHistoryRecord, onOpenModal: () => void): OverviewMetric {
  const count = Math.max(0, record.filmCount ?? 0);
  const isOngoing = isBatchOngoing(record.status);
  const openHint = (
    <a className={styles.metricHintLink} onClick={onOpenModal}>
      查看清单
    </a>
  );

  if (isOngoing) {
    return {
      key: "films",
      label: "更新影片",
      icon: <VideoCameraOutlined className={styles.metricIcon} />,
      tag: statusTag("processing", count > 0 ? "有更新" : "采集中"),
      value: count > 0 ? `+${formatNum(count)}` : "0",
      unit: "部",
      hint: count > 0 ? openHint : "更新中",
      tone: count > 0 ? undefined : "muted",
      onClick: count > 0 ? onOpenModal : undefined,
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
      onClick: onOpenModal,
    };
  }

  return {
    key: "films",
    label: "更新影片",
    icon: <VideoCameraOutlined className={styles.metricIcon} />,
    tag: statusTag("default", "无更新"),
    value: "0",
    unit: "部",
    hint: "无影片更新",
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
  const page = buildPageView(record);
  const metrics = [
    buildSiteMetric(record, abnormalCount, stoppedSources, doneSources, interruptedSources.length),
    page.summary,
    buildFilmMetric(record, onOpenFilmsModal),
  ].filter((metric): metric is OverviewMetric => metric != null);

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
      {page.board ? <PageBoardView board={page.board} /> : null}
    </div>
  );
}
