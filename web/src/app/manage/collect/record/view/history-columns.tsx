import React from "react";
import Link from "next/link";
import { Tag, Tooltip, Button, Popconfirm } from "antd";
import {
  ApiOutlined,
  CheckCircleOutlined,
  ClockCircleOutlined,
  CloseCircleOutlined,
  CloudDownloadOutlined,
  CloudServerOutlined,
  ExclamationCircleOutlined,
  FieldTimeOutlined,
  FileTextOutlined,
  PauseCircleOutlined,
  ReloadOutlined,
  ScheduleOutlined,
  SyncOutlined,
  UserOutlined,
  VideoCameraOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { canRetryHistoryRecord, CollectHistoryRecord } from "./types";
import { ErrorDetailButton } from "./error-detail-button";
import styles from "./index.module.less";

export function formatDuration(ms: number): string {
  if (ms <= 0) return "< 1 秒";
  const seconds = Math.floor(ms / 1000);
  if (seconds < 60) return `${seconds} 秒`;
  const minutes = Math.floor(seconds / 60);
  const remainingSec = seconds % 60;
  if (minutes < 60) {
    return remainingSec > 0 ? `${minutes} 分 ${remainingSec} 秒` : `${minutes} 分钟`;
  }
  const hours = Math.floor(minutes / 60);
  const remainingMin = minutes % 60;
  return `${hours} 小时 ${remainingMin} 分`;
}

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
  // 兼容历史存量数据
  "Batch-Collect": { label: "批量采集", color: "blue", icon: <CloudDownloadOutlined /> },
  "Single-Collect": { label: "单站采集", color: "blue", icon: <ApiOutlined /> },
  批量采集: { label: "批量采集", color: "blue", icon: <CloudDownloadOutlined /> },
  单站采集: { label: "单站采集", color: "blue", icon: <ApiOutlined /> },
  失败重试: { label: "失败重试", color: "purple", icon: <ReloadOutlined /> },
  定时任务: { label: "定时任务", color: "cyan", icon: <ScheduleOutlined /> },
  单片更新: { label: "单片更新", color: "geekblue", icon: <VideoCameraOutlined /> },
};

function columnTitle(icon: React.ReactNode, label: string) {
  return (
    <span className={styles.withIcon}>
      {icon}
      {label}
    </span>
  );
}

const tagStyle = { marginInlineEnd: 0 };

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

export function renderHourTag(hour: number) {
  let label = "默认";
  if (hour === -1) label = "全量";
  else if (hour > 0) label = `${hour} 小时`;
  return (
    <span className={`${styles.modeText} ${styles.withIcon}`}>
      <FieldTimeOutlined />
      {label}
    </span>
  );
}

export function formatCollectPages(
  record: { trigger?: string; tag?: string; status?: string },
  pageSuccess: number,
  pageTotal: number,
  pageFailed: number,
) {
  // 旧的单片更新没有写入页进度。一次更新就是一次详情请求。
  if (isSingleFilmRecord(record) && pageTotal <= 0 && pageSuccess <= 0 && pageFailed <= 0) {
    if (record.status === "failed") return "0/1";
    if (record.status === "success" || record.status === "done") return "1/1";
  }
  return `${pageSuccess}/${pageTotal}`;
}

export function formatCollectFilms(count: number, record: { status?: string }) {
  if (count <= 0 && (record.status === "success" || record.status === "done")) {
    return "无变更";
  }
  return `${Math.max(0, count)} 部`;
}

export function formatStartedAt(val: string, now = dayjs()) {
  if (!val) return { primary: "-", full: "" };
  const t = dayjs(val);
  if (!t.isValid()) return { primary: val, full: val };
  const full = t.format("YYYY-MM-DD HH:mm:ss");
  if (t.isSame(now, "day")) return { primary: t.format("HH:mm"), full };
  if (t.isSame(now, "year")) return { primary: t.format("MM-DD HH:mm"), full };
  return { primary: t.format("YYYY-MM-DD HH:mm"), full };
}

export function RecordStatusMark({ status, scope }: { status: string; scope: "batch" | "source" }) {
  const batch = scope === "batch";
  let icon = <ExclamationCircleOutlined />;
  let label = status || "未知";
  let className = styles.detailMuted;
  switch (status) {
    case "success":
      icon = <CheckCircleOutlined />;
      label = batch ? "全部成功" : "成功";
      className = styles.statusOk;
      break;
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
      label = batch ? "采集失败" : "失败";
      className = styles.detailFail;
      break;
    case "running":
      icon = <SyncOutlined spin />;
      label = batch ? "执行中" : "采集中";
      className = styles.detailRun;
      break;
    case "starting":
    case "waiting":
      icon = <ClockCircleOutlined />;
      label = "排队中";
      break;
    case "stopped":
      icon = <PauseCircleOutlined />;
      label = batch ? "已终止" : "已停止";
      break;
    case "interrupted":
      icon = <WarningOutlined />;
      label = "异常中断";
      className = styles.detailFail;
      break;
    default:
      break;
  }
  return (
    <span className={`${styles.withIcon} ${className}`}>
      {icon}
      <span>{label}</span>
    </span>
  );
}

export function renderStatusBadge(status: string) {
  return <RecordStatusMark status={status} scope="batch" />;
}

export interface GetHistoryColumnsOptions {
  canWrite: boolean;
  retryingBatchIds: Set<string>;
  onRetryBatch: (record: CollectHistoryRecord) => void;
}

export function getHistoryColumns(opts: GetHistoryColumnsOptions): ColumnsType<CollectHistoryRecord> {
  const { canWrite, retryingBatchIds, onRetryBatch } = opts;

  return [
    {
      title: columnTitle(<ClockCircleOutlined />, "时间"),
      dataIndex: "startedAt",
      key: "startedAt",
      width: "16%",
      render: (val: string, record) => {
        const effectiveMs =
          record.status === "running" && record.startedAt && record.durationMs <= 0
            ? Math.max(0, Date.now() - new Date(record.startedAt).getTime())
            : record.durationMs;
        const started = formatStartedAt(val);
        return (
          <Tooltip title={started.full || undefined}>
            <div className={styles.timeCell}>
              <ClockCircleOutlined className={styles.timeMeta} />
              <span className={styles.timePrimary}>{started.primary}</span>
              <span className={styles.timeMeta}>{formatDuration(effectiveMs)}</span>
            </div>
          </Tooltip>
        );
      },
    },
    {
      title: columnTitle(<CloudDownloadOutlined />, "采集"),
      dataIndex: "trigger",
      key: "trigger",
      width: "22%",
      render: (trigger: string, record) => {
        const films = singleFilmTargets(record);
        return (
          <div className={`${styles.collectCell} ${films.length > 0 ? styles.collectStack : ""}`}>
            <span className={styles.collectMain}>
              {renderTriggerTag(trigger, record.tag)}
              {isSingleFilmRecord(record) ? null : renderHourTag(record.hour)}
            </span>
            {films.length > 0 ? (
              <span className={styles.filmTags}>
                {films.map((film) => {
                  const tag = (
                    <Tag icon={<VideoCameraOutlined />} className={styles.filmTag} style={tagStyle}>
                      {film.name}
                    </Tag>
                  );
                  if (film.mid <= 0) return <span key={film.name}>{tag}</span>;
                  return (
                    <Link
                      key={film.mid}
                      href={resolvePlayEntryPath(film.mid)}
                      className={styles.filmTagLink}
                      title={film.name}
                      onClick={(event) => event.stopPropagation()}
                    >
                      {tag}
                    </Link>
                  );
                })}
              </span>
            ) : null}
          </div>
        );
      },
    },
    {
      title: columnTitle(<CheckCircleOutlined />, "状态"),
      dataIndex: "status",
      key: "status",
      width: "12%",
      render: (status: string) => renderStatusBadge(status),
    },
    {
      title: columnTitle(<CloudServerOutlined />, "站点"),
      key: "sources",
      width: "11%",
      render: (_, record) => (
        <span className={`${styles.withIcon} ${styles.metric}`}>
          <CloudServerOutlined />
          {record.successSources}/{record.sourceCount}
          {record.failedSources > 0 && (
            <span className={`${styles.withIcon} ${styles.metricFail}`}>
              <CloseCircleOutlined />
              {record.failedSources}
            </span>
          )}
        </span>
      ),
    },
    {
      title: columnTitle(<FileTextOutlined />, "页码"),
      key: "pages",
      width: "13%",
      render: (_, record) => (
        <span className={`${styles.withIcon} ${styles.metric}`}>
          <FileTextOutlined />
          {formatCollectPages(record, record.pageSuccess, record.pageTotal, record.pageFailed)}
          {record.pageFailed > 0 && (
            <span className={`${styles.withIcon} ${styles.metricFail}`}>
              <CloseCircleOutlined />
              {record.pageFailed}
            </span>
          )}
        </span>
      ),
    },
    {
      title: columnTitle(<VideoCameraOutlined />, "入库影片"),
      dataIndex: "filmCount",
      key: "filmCount",
      width: "12%",
      render: (count: number, record) => {
        const text = formatCollectFilms(count, record);
        const emphasized = count > 0;
        return (
          <span className={`${styles.withIcon} ${emphasized ? styles.filmCount : styles.filmCountEmpty}`}>
            <VideoCameraOutlined />
            {text}
          </span>
        );
      },
    },
    {
      title: columnTitle(<ReloadOutlined />, "操作"),
      key: "action",
      width: "14%",
      render: (_, record) => {
        const isRetrying = retryingBatchIds.has(record.batchId);
        const canRetry = canRetryHistoryRecord(record);
        const error = record.errorMsg?.trim();
        if (!canRetry && !error) return null;
        const filmRetry = record.trigger === "single_update";
        const retryLabel = filmRetry ? "重试该片" : "重试失败站点";
        return (
          <span className={styles.detailActions}>
            {error ? <ErrorDetailButton text={error} /> : null}
            {canRetry ? (
              <Popconfirm
                title={filmRetry ? "重试这部影片？" : "重试本次失败的站点？"}
                description={
                  filmRetry
                    ? "将按原影片重新采集"
                    : `将重新采集本次失败的 ${record.failedSources} 个站点`
                }
                onConfirm={() => onRetryBatch(record)}
                disabled={!canWrite || isRetrying}
              >
                <Button
                  type="text"
                  size="small"
                  icon={<ReloadOutlined />}
                  loading={isRetrying}
                  disabled={!canWrite}
                  className={styles.actionButton}
                >
                  {retryLabel}
                </Button>
              </Popconfirm>
            ) : null}
          </span>
        );
      },
    },
  ];
}
