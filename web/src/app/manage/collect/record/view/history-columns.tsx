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
  EyeOutlined,
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
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { canRetryHistoryRecord, CollectHistoryRecord, CollectRecordSourceDetail, resolveBatchStatus } from "./types";
import { formatCollectFilms, formatCollectPages, formatDuration, formatStartedAt } from "./record-format";
import { ErrorDetailButton } from "./error-detail-button";
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
  onOpenDetail: (record: CollectHistoryRecord) => void;
}

export function getHistoryColumns(opts: GetHistoryColumnsOptions): ColumnsType<CollectHistoryRecord> {
  const { canWrite, retryingBatchIds, onRetryBatch, onOpenDetail } = opts;

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
          <Tooltip title={started.full ? `开始时间：${started.full}` : undefined}>
            <div className={styles.timeCell}>
              <span className={styles.timePrimary}>{started.primary}</span>
              <span className={styles.timeMeta}>耗时 {formatDuration(effectiveMs)}</span>
            </div>
          </Tooltip>
        );
      },
    },
    {
      title: columnTitle(<CloudDownloadOutlined />, "采集方式"),
      dataIndex: "trigger",
      key: "trigger",
      width: "14%",
      render: (trigger: string, record) => {
        const films = singleFilmTargets(record);
        return (
          <div className={`${styles.collectCell} ${films.length > 0 ? styles.collectStack : ""}`}>
            <span className={styles.collectMain}>
              {renderTriggerTag(trigger, record.tag)}
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
      title: columnTitle(<FieldTimeOutlined />, "采集跨度"),
      key: "hour",
      width: "11%",
      render: (_, record) => renderHourTag(record.hour, record),
    },
    {
      title: columnTitle(<CheckCircleOutlined />, "状态"),
      dataIndex: "status",
      key: "status",
      width: "10%",
      render: (_: string, record) => renderStatusBadge(resolveBatchStatus(record)),
    },
    {
      title: columnTitle(<CloudServerOutlined />, "站点"),
      key: "sources",
      width: "12%",
      render: (_, record) => {
        const total = record.sourceCount;
        let details: CollectRecordSourceDetail[] = [];
        if (record.detailJson) {
          try {
            details = JSON.parse(record.detailJson) as CollectRecordSourceDetail[];
          } catch {
            details = [];
          }
        }
        const hasFailed = details.some((d) => d.status === "failed");
        const hasPartial = details.some((d) => d.status === "partial");
        const stoppedCount = details.filter((d) => d.status === "stopped").length;
        const batchStatus = resolveBatchStatus(record);
        const hasAbnormal =
          hasFailed ||
          hasPartial ||
          stoppedCount > 0 ||
          batchStatus === "stopped" ||
          batchStatus === "interrupted" ||
          batchStatus === "running" ||
          record.failedSources > 0 ||
          record.pageFailed > 0;

        if (!hasAbnormal) {
          return (
            <div className={styles.statCell}>
              <span className={`${styles.statRow} ${styles.statOk}`}>
                <CheckCircleOutlined />
                {total} 站成功
              </span>
            </div>
          );
        }

        // 红/黄与状态列同一口径，避免明细缺失时另算一套把部分失败翻成失败
        const allFailed = resolveBatchStatus(record) === "failed";

        if (total === 1) {
          const singleLabel = allFailed
            ? "1 站失败"
            : batchStatus === "stopped" || batchStatus === "interrupted"
              ? "1 站已停止"
              : batchStatus === "running"
                ? "1 站采集中"
                : "1 站未完成";
          return (
            <div className={styles.statCell}>
              {allFailed ? (
                <span className={`${styles.statRow} ${styles.statFail}`}>
                  <CloseCircleOutlined />
                  {singleLabel}
                </span>
              ) : (
                <span className={`${styles.statRow} ${styles.statWarn}`}>
                  <ExclamationCircleOutlined />
                  {singleLabel}
                </span>
              )}
            </div>
          );
        }

        const failedCount =
          details.length > 0
            ? details.filter((d) => d.status === "failed" || d.status === "partial").length
            : Math.max(1, record.failedSources);
        const successCount =
          details.length > 0
            ? details.filter((d) => d.status === "done" || d.status === "success").length
            : Math.max(0, total - failedCount - stoppedCount);
        // 只有停止、没有失败时，按停止数展示，避免把用户主动停止说成未完成。
        const shownCount = failedCount > 0 ? failedCount : stoppedCount || (batchStatus === "running" ? total : 0);
        const shownLabel = allFailed
          ? "失败"
          : failedCount > 0
            ? "未完成"
            : batchStatus === "running"
              ? "采集中"
              : "已停止";

        return (
          <div className={styles.statCell}>
            <span className={`${styles.statRow} ${allFailed ? styles.statFail : styles.statWarn}`}>
              {allFailed ? <CloseCircleOutlined /> : <ExclamationCircleOutlined />}
              {shownCount} 站{shownLabel}
            </span>
            <span className={`${styles.statRow} ${styles.statTotal}`}>
              （{successCount} / {total} 站成功）
            </span>
          </div>
        );
      },
    },
    {
      title: columnTitle(<FileTextOutlined />, "拉取进度"),
      key: "pages",
      width: "14%",
      render: (_, record) => {
        if (isSingleFilmRecord(record)) {
          return (
            <div className={styles.statCell}>
              <span className={`${styles.statRow} ${styles.statTotal}`}>
                <FileTextOutlined />
                {record.status === "failed" ? "0/1 失败" : "1/1 完成"}
              </span>
            </div>
          );
        }

        const pages = formatCollectPages(record);
        return (
          <div className={styles.statCell}>
            <span className={`${styles.statRow} ${pages.alert ? styles.statFail : styles.statTotal}`}>
              {pages.alert ? <CloseCircleOutlined /> : <FileTextOutlined />}
              {pages.primary}
            </span>
            {pages.secondary ? (
              <span className={`${styles.statRow} ${styles.statTotal}`}>{pages.secondary}</span>
            ) : null}
          </div>
        );
      },
    },
    {
      title: columnTitle(<VideoCameraOutlined />, "入库影片"),
      dataIndex: "filmCount",
      key: "filmCount",
      width: "11%",
      render: (count: number, record) => {
        const text = formatCollectFilms(count, record);
        const emphasized = count > 0;
        return (
          <span className={`${styles.withIcon} ${emphasized ? styles.filmCount : styles.filmCountEmpty}`}>
            <VideoCameraOutlined />
            {emphasized ? `+${count} 部` : text}
          </span>
        );
      },
    },
    {
      title: columnTitle(<ReloadOutlined />, "操作"),
      key: "action",
      width: "13%",
      render: (_, record) => {
        const isRetrying = retryingBatchIds.has(record.batchId);
        const canRetry = canRetryHistoryRecord(record);
        const filmRetry = record.trigger === "single_update";
        const retryLabel = filmRetry ? "重试该片" : "重试失败";
        return (
          <span className={styles.detailActions}>
            <Button
              type="primary"
              size="small"
              icon={<EyeOutlined />}
              onClick={() => onOpenDetail(record)}
            >
              详情
            </Button>
            {canRetry ? (
              <Popconfirm
                title={filmRetry ? "重试这部影片？" : "重试本次未完成的站点？"}
                description={
                  filmRetry
                    ? "将按原影片重新采集"
                    : "将重新采集本次未完成的站点"
                }
                onConfirm={() => onRetryBatch(record)}
                disabled={!canWrite || isRetrying}
              >
                <Button
                  danger
                  size="small"
                  icon={<ReloadOutlined />}
                  loading={isRetrying}
                  disabled={!canWrite}
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
