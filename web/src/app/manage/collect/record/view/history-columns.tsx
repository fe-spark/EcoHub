import React from "react";
import Link from "next/link";
import { Button, Popconfirm, Space, Tag } from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  ExclamationCircleOutlined,
  FieldTimeOutlined,
  PauseCircleOutlined,
  ReloadOutlined,
  RightOutlined,
  ScheduleOutlined,
  SyncOutlined,
  UserOutlined,
  VideoCameraOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import {
  canRetryHistoryRecord,
  CollectHistoryRecord,
  isSingleFilmRecord,
  resolveBatchStatus,
} from "./types";
import {
  effectiveDurationMs,
  formatBatchSourceStat,
  formatCollectFilms,
  formatDuration,
  formatStartedAt,
  getHourLabel,
  parseSourceDetails,
} from "./record-format";
import styles from "./index.module.less";

export const COLLECT_TRIGGER_MAP: Record<string, { label: string; color: string; icon: React.ReactNode }> = {
  manual: { label: "手动采集", color: "blue", icon: <UserOutlined /> },
  cron: { label: "定时任务", color: "cyan", icon: <ScheduleOutlined /> },
  retry: { label: "失败重试", color: "purple", icon: <ReloadOutlined /> },
  single_update: { label: "单片更新", color: "geekblue", icon: <VideoCameraOutlined /> },
};

const tagStyle = { marginInlineEnd: 0 };

export function renderTriggerTag(trigger: string) {
  const item = COLLECT_TRIGGER_MAP[trigger];
  if (item) {
    return (
      <Tag color={item.color} variant="filled" icon={item.icon} style={tagStyle}>
        {item.label}
      </Tag>
    );
  }
  return (
    <Tag variant="filled" style={tagStyle}>
      {trigger || "未知"}
    </Tag>
  );
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

export function RecordStatusMark({
  status,
  scope,
}: {
  status: string;
  scope: "batch" | "source";
}) {
  const batch = scope === "batch";
  let icon = <ExclamationCircleOutlined />;
  let label = status || "未知";
  let color = "default";

  switch (status) {
    case "success":
    case "done":
      icon = <CheckCircleOutlined />;
      label = "成功";
      color = "success";
      break;
    case "partial":
      icon = <ExclamationCircleOutlined />;
      label = "部分失败";
      color = "warning";
      break;
    case "partial_stopped":
      icon = <PauseCircleOutlined />;
      label = "部分停止";
      color = "warning";
      break;
    case "failed":
      icon = <CloseCircleOutlined />;
      label = "失败";
      color = "error";
      break;
    case "running":
      icon = <SyncOutlined spin />;
      label = batch ? "进行中" : "采集中";
      color = "processing";
      break;
    case "stopped":
      icon = <PauseCircleOutlined />;
      label = "已停止";
      color = "default";
      break;
    case "interrupted":
      icon = <WarningOutlined />;
      label = "异常中断";
      color = "error";
      break;
    default:
      break;
  }

  return (
    <Tag color={color} variant="filled" icon={icon} style={tagStyle}>
      {label}
    </Tag>
  );
}

export interface GetHistoryColumnsOptions {
  canWrite: boolean;
  retryingBatchIds: Set<string>;
  onRetryBatch: (record: CollectHistoryRecord) => void;
  onOpenDetail: (record: CollectHistoryRecord) => void;
  onOpenFilms: (record: CollectHistoryRecord) => void;
}

export function getHistoryColumns(opts: GetHistoryColumnsOptions): ColumnsType<CollectHistoryRecord> {
  const { canWrite, retryingBatchIds, onRetryBatch, onOpenDetail, onOpenFilms } = opts;

  return [
    {
      title: "执行时间",
      key: "startedAt",
      width: 204,
      render: (_, record) => {
        const started = formatStartedAt(record.startedAt);
        const duration = formatDuration(effectiveDurationMs(record));
        const running = record.status === "running";
        return (
          <div className={styles.cellStack}>
            <span className={styles.timePrimary}>{started.primary}</span>
            <span className={`${styles.subText} ${running ? styles.subTextAccent : ""}`}>
              <FieldTimeOutlined />
              {running ? `已耗时 ${duration}` : `耗时 ${duration}`}
            </span>
          </div>
        );
      },
    },
    {
      title: "方式与内容",
      key: "trigger",
      width: 244,
      render: (_, record) => {
        const singleFilm = isSingleFilmRecord(record);
        const films = singleFilm ? singleFilmTargets(record) : [];
        const details = parseSourceDetails(record.detailJson);
        const hourLabel = getHourLabel(record.hour, record);
        const singleSourceName =
          record.tag === "single_collect" && details.length === 1 ? details[0].sourceName : "";
        const scopeText = [singleSourceName, hourLabel].filter(Boolean).join(" · ");

        return (
          <div className={styles.cellStack}>
            <div className={styles.triggerRow}>
              {renderTriggerTag(record.trigger)}
              {!singleFilm && scopeText ? <span className={styles.scopePill}>{scopeText}</span> : null}
            </div>
            {singleFilm && films.length > 0 ? (
              <Space size={4} wrap>
                {films.map((film) => {
                  const tag = (
                    <Tag className={styles.filmChip} style={tagStyle}>
                      {film.name}
                    </Tag>
                  );
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
          </div>
        );
      },
    },
    {
      title: "状态",
      key: "status",
      width: 200,
      render: (_, record) => {
        const status = resolveBatchStatus(record);
        const details = parseSourceDetails(record.detailJson);
        const stat = formatBatchSourceStat(record, details);
        const showSites = stat.primary !== "-";

        return (
          <div className={styles.cellStack}>
            <RecordStatusMark status={status} scope="batch" />
            {showSites ? (
              <span className={styles.subText}>
                <span>{stat.primary}</span>
                {stat.secondary ? (
                  <span className={stat.tone === "fail" ? styles.subTextDanger : styles.subTextWarn}>
                    · {stat.secondary}
                  </span>
                ) : null}
              </span>
            ) : null}
          </div>
        );
      },
    },
    {
      title: "更新影片",
      key: "films",
      width: 144,
      render: (_, record) => {
        const count = Math.max(0, record.filmCount ?? 0);
        if (count <= 0) {
          return <span className={styles.filmNumberMuted}>{formatCollectFilms(0, record)}</span>;
        }
        const formatted = `+${count.toLocaleString("zh-CN")} 部`;
        if (record.ledger === true && record.status !== "running") {
          return (
            <button
              type="button"
              className={styles.filmActionPill}
              onClick={(event) => {
                event.stopPropagation();
                onOpenFilms(record);
              }}
            >
              <span>{formatted}</span>
              <RightOutlined className={styles.filmPillArrow} />
            </button>
          );
        }
        return <span className={styles.filmNumber}>{formatted}</span>;
      },
    },
    {
      title: "操作",
      key: "action",
      width: 136,
      align: "right",
      render: (_, record) => {
        const isRetrying = retryingBatchIds.has(record.batchId);
        const canRetry = canRetryHistoryRecord(record);
        const filmRetry = isSingleFilmRecord(record);
        const retryLabel = filmRetry ? "重试该片" : "重试失败";
        return (
          <Space size={4} onClick={(event) => event.stopPropagation()}>
            <Button type="link" size="small" onClick={() => onOpenDetail(record)}>
              详情
            </Button>
            {canRetry ? (
              <Popconfirm
                title={filmRetry ? "重试该片" : "重试失败站点"}
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
