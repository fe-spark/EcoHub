import React from "react";
import Link from "next/link";
import { Button, Popconfirm, Space, Tag, Tooltip, Typography } from "antd";
import {
  ApiOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  CloudDownloadOutlined,
  ExclamationCircleOutlined,
  FieldTimeOutlined,
  PauseCircleOutlined,
  ReloadOutlined,
  ScheduleOutlined,
  SyncOutlined,
  UserOutlined,
  VideoCameraOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { canRetryHistoryRecord, CollectHistoryRecord, CollectRecordSourceDetail, resolveBatchStatus } from "./types";
import {
  formatBatchSourceStat,
  formatCollectFilms,
  formatDuration,
  formatStartedAt,
  liveSourceStatusLabel,
} from "./record-format";
import styles from "./index.module.less";

export const COLLECT_TRIGGER_MAP: Record<string, { label: string; color: string; icon: React.ReactNode }> = {
  manual: { label: "手动采集", color: "blue", icon: <UserOutlined /> },
  cron: { label: "定时任务", color: "cyan", icon: <ScheduleOutlined /> },
  retry: { label: "失败重试", color: "purple", icon: <ReloadOutlined /> },
  single_update: { label: "单片更新", color: "geekblue", icon: <VideoCameraOutlined /> },
};

export const COLLECT_TAG_MAP: Record<string, { label: string; color: string; icon: React.ReactNode }> = {
  batch_collect: { label: "批量采集", color: "blue", icon: <CloudDownloadOutlined /> },
  single_collect: { label: "单站采集", color: "blue", icon: <ApiOutlined /> },
  retry_collect: { label: "失败重试", color: "purple", icon: <ReloadOutlined /> },
  cron_collect: { label: "定时任务", color: "cyan", icon: <ScheduleOutlined /> },
  single_update: { label: "单片更新", color: "geekblue", icon: <VideoCameraOutlined /> },
};

const tagStyle = { marginInlineEnd: 0 };

export function triggerLabel(trigger: string, tag?: string) {
  if (tag && COLLECT_TAG_MAP[tag]) return COLLECT_TAG_MAP[tag].label;
  if (trigger && COLLECT_TRIGGER_MAP[trigger]) return COLLECT_TRIGGER_MAP[trigger].label;
  return tag || trigger || "采集";
}

export function renderTriggerTag(trigger: string, tag?: string) {
  if (tag && COLLECT_TAG_MAP[tag]) {
    const item = COLLECT_TAG_MAP[tag];
    return (
      <Tag color={item.color} icon={item.icon} style={tagStyle}>
        {item.label}
      </Tag>
    );
  }
  if (trigger && COLLECT_TRIGGER_MAP[trigger]) {
    const item = COLLECT_TRIGGER_MAP[trigger];
    return (
      <Tag color={item.color} icon={item.icon} style={tagStyle}>
        {item.label}
      </Tag>
    );
  }
  return <Tag style={tagStyle}>{tag || trigger || "未知"}</Tag>;
}

export function isSingleFilmRecord(record: { trigger?: string; tag?: string }) {
  return record.trigger === "single_update" || record.tag === "single_update" || record.tag === "单片更新";
}

export function singleFilmTargets(record: {
  trigger?: string;
  tag?: string;
  filmName?: string;
  films?: { mid: number; name: string }[];
}) {
  if (!isSingleFilmRecord(record)) return [];
  const linked = (record.films || [])
    .map((film) => ({ mid: film.mid, name: film.name?.trim() || "" }))
    .filter((film) => film.mid > 0 && film.name);
  if (linked.length > 0) return linked;
  const name = record.filmName?.trim();
  if (!name) return [];
  return name
    .split("、")
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => ({ mid: 0, name: item }));
}

export const COLLECT_DURATION_MAP: Record<number, string> = {
  24: "采集今日",
  72: "采集三天",
  168: "采集一周",
  360: "采集半月",
  720: "采集一月",
  2160: "采集三月",
  4320: "采集半年",
  [-1]: "全量采集",
};

export function getHourLabel(hour: number, record?: { trigger?: string; tag?: string }): string {
  if (record && isSingleFilmRecord(record)) {
    return "单片重放";
  }
  if (COLLECT_DURATION_MAP[hour]) {
    return COLLECT_DURATION_MAP[hour];
  }
  if (hour === -1) return "全量采集";
  if (hour > 0) return `采集 ${hour} 小时`;
  return "默认";
}

export function renderHourTag(hour: number, record?: { trigger?: string; tag?: string }) {
  const label = getHourLabel(hour, record);
  const icon = record && isSingleFilmRecord(record) ? <VideoCameraOutlined /> : <FieldTimeOutlined />;
  return (
    <span className={`${styles.modeText} ${styles.withIcon}`}>
      {icon}
      {label}
    </span>
  );
}

export { resolveBatchStatus };

export function RecordStatusMark({
  status,
  scope,
  live = false,
  successCnt = 0,
  failedCnt = 0,
}: {
  status: string;
  scope: "batch" | "source";
  live?: boolean;
  successCnt?: number;
  failedCnt?: number;
}) {
  const batch = scope === "batch";
  let icon = <ExclamationCircleOutlined />;
  let label = status || "未知";
  let className = styles.detailMuted;
  switch (status) {
    case "success":
    case "done":
      icon = <CheckCircleOutlined />;
      label = "成功";
      className = styles.statusOk;
      break;
    case "partial":
      icon = <ExclamationCircleOutlined />;
      label = "部分失败";
      className = styles.detailWarn;
      break;
    case "failed":
      icon = <CloseCircleOutlined />;
      label = "失败";
      className = styles.detailFail;
      break;
    case "running":
      icon = <SyncOutlined spin />;
      label = batch ? "进行中" : "采集中";
      className = styles.detailRun;
      break;
    case "starting":
    case "waiting":
      icon = <ClockCircleOutlined />;
      label = "等待中";
      className = styles.detailRun;
      break;
    case "paused":
      icon = <PauseCircleOutlined />;
      label = "限流中";
      className = styles.detailWarn;
      break;
    case "stopping":
      icon = <SyncOutlined spin />;
      label = "停止中";
      className = styles.detailWarn;
      break;
    case "sealing":
      icon = <ClockCircleOutlined />;
      label = "待检索";
      className = styles.detailWarn;
      break;
    case "stopped":
      icon = <PauseCircleOutlined />;
      label = "已停止";
      break;
    case "interrupted":
      icon = <WarningOutlined />;
      label = "异常中断";
      className = styles.detailFail;
      break;
    default:
      break;
  }
  if (live && !batch) {
    const liveLabel = liveSourceStatusLabel(status, successCnt, failedCnt);
    if (liveLabel) {
      label = liveLabel;
      if (liveLabel === "部分失败" || liveLabel === "部分完成" || liveLabel === "采集失败") {
        className = liveLabel === "采集失败" ? styles.detailFail : styles.detailWarn;
      }
    }
  }
  return (
    <span className={`${styles.withIcon} ${className}`}>
      {icon}
      <span>{label}</span>
    </span>
  );
}

export function sourceActivityLabel(status: string): string | null {
  switch (status) {
    case "starting":
    case "waiting":
      return "等待中";
    case "running":
      return "采集中";
    case "paused":
      return "限流中";
    case "stopping":
      return "停止中";
    case "sealing":
      return "待检索";
    default:
      return null;
  }
}

export function renderStatusBadge(status: string) {
  return <RecordStatusMark status={status} scope="batch" />;
}

export interface GetHistoryColumnsOptions {
  canWrite: boolean;
  retryingBatchIds: Set<string>;
  onRetryBatch: (record: CollectHistoryRecord) => void;
  onOpenDetail: (record: CollectHistoryRecord) => void;
}

export function getHistoryColumns(opts: GetHistoryColumnsOptions): ColumnsType<CollectHistoryRecord> {
  const { canWrite, retryingBatchIds, onRetryBatch, onOpenDetail } = opts;

  return [
    {
      title: "采集时间",
      key: "round",
      render: (_, record) => {
        const effectiveMs =
          record.status === "running" && record.startedAt && record.durationMs <= 0
            ? Math.max(0, Date.now() - new Date(record.startedAt).getTime())
            : record.durationMs;
        const started = formatStartedAt(record.startedAt);
        const films = singleFilmTargets(record);
        return (
          <Space orientation="vertical" size={0}>
            <Tooltip title={started.full ? `开始于 ${started.full}` : undefined}>
              <span>{started.primary}</span>
            </Tooltip>
            <Typography.Text type="secondary">
              {triggerLabel(record.trigger, record.tag)}
              {" · "}
              {getHourLabel(record.hour, record)}
              {" · "}
              {formatDuration(effectiveMs)}
            </Typography.Text>
            {films.length > 0 ? (
              <Space size={4} wrap>
                {films.map((film) => {
                  const tag = <Tag style={tagStyle}>{film.name}</Tag>;
                  if (film.mid <= 0) return <span key={film.name}>{tag}</span>;
                  return (
                    <Link
                      key={film.mid}
                      href={resolvePlayEntryPath(film.mid)}
                      title={film.name}
                      onClick={(event) => event.stopPropagation()}
                    >
                      {tag}
                    </Link>
                  );
                })}
              </Space>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: "更新影片",
      key: "films",
      render: (_, record) => {
        const count = Math.max(0, record.filmCount ?? 0);
        return <span>{formatCollectFilms(count, record)}</span>;
      },
    },
    {
      title: "结果",
      key: "outcome",
      render: (_, record) => {
        let details: CollectRecordSourceDetail[] = [];
        if (record.detailJson) {
          try {
            details = JSON.parse(record.detailJson) as CollectRecordSourceDetail[];
          } catch {
            details = [];
          }
        }
        const stat = formatBatchSourceStat(record, details);
        const status = resolveBatchStatus(record);
        const tone =
          status === "success" ? "success"
          : status === "failed" || status === "interrupted" ? "error"
          : status === "partial" ? "warning"
          : status === "running" ? "processing"
          : "default";
        const label =
          status === "success" ? "成功"
          : status === "partial" ? "部分失败"
          : status === "failed" ? "失败"
          : status === "running" ? "进行中"
          : status === "stopped" ? "已停止"
          : status === "interrupted" ? "异常中断"
          : status;
        const siteLine = stat.secondary && stat.secondary !== label
          ? `${stat.primary} · ${stat.secondary}`
          : stat.primary;
        return (
          <Space orientation="vertical" size={0}>
            <Tag color={tone} style={tagStyle}>{label}</Tag>
            <Typography.Text type="secondary">{siteLine}</Typography.Text>
          </Space>
        );
      },
    },
    {
      title: "操作",
      key: "action",
      render: (_, record) => {
        const isRetrying = retryingBatchIds.has(record.batchId);
        const canRetry = canRetryHistoryRecord(record);
        const filmRetry = record.trigger === "single_update";
        const retryLabel = filmRetry ? "重试该片" : "重试失败";
        return (
          <Space size={0} onClick={(event) => event.stopPropagation()}>
            <Button type="link" size="small" onClick={() => onOpenDetail(record)}>
              查看
            </Button>
            {canRetry ? (
              <Popconfirm
                title={filmRetry ? "重试该片" : "重试未完成站点"}
                onConfirm={() => onRetryBatch(record)}
                disabled={!canWrite || isRetrying}
              >
                <Button type="link" size="small" danger loading={isRetrying} disabled={!canWrite}>
                  {retryLabel}
                </Button>
              </Popconfirm>
            ) : null}
          </Space>
        );
      },
    },
  ];
}
