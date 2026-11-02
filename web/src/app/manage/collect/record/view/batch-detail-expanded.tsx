import React from "react";
import { Button, Popconfirm, Tooltip } from "antd";
import {
  CheckCircleOutlined,
  CloseCircleOutlined,
  CloudServerOutlined,
  FileTextOutlined,
  ReloadOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { canRetryHistorySource, CollectHistoryRecord, CollectRecordSourceDetail } from "./types";
import { ErrorDetailButton } from "./error-detail-button";
import { formatCollectFilms, formatCollectPages, isSingleFilmRecord, RecordStatusMark } from "./history-columns";
import styles from "./index.module.less";

export interface BatchDetailExpandedProps {
  record: CollectHistoryRecord;
  canWrite: boolean;
  retryingSourceKeys: Set<string>;
  onRetrySingleSource: (batchId: string, sourceId: string) => void;
}

function canRetrySource(record: CollectHistoryRecord, item: CollectRecordSourceDetail) {
  return (item.status === "failed" || item.status === "partial") && canRetryHistorySource(record);
}

export function BatchDetailExpanded({
  record,
  canWrite,
  retryingSourceKeys,
  onRetrySingleSource,
}: BatchDetailExpandedProps) {
  let details: CollectRecordSourceDetail[] = [];
  if (record.detailJson) {
    try {
      details = JSON.parse(record.detailJson) as CollectRecordSourceDetail[];
    } catch {
      details = [];
    }
  }

  if (details.length === 0) {
    return <div className={styles.detailEmpty}>暂无单站执行明细快照</div>;
  }

  const sorted = [...details].sort((a, b) => {
    const rank = (status: string) => (status === "failed" || status === "partial" ? 1 : 0);
    return rank(b.status) - rank(a.status);
  });

  return (
    <div className={styles.detailList}>
      <div className={styles.detailHead}>
        <span className={styles.withIcon}>
          <CloudServerOutlined />
          站点
        </span>
        <span className={styles.withIcon}>
          <CheckCircleOutlined />
          状态
        </span>
        <span className={styles.withIcon}>
          <FileTextOutlined />
          页码
        </span>
        <span className={styles.withIcon}>
          <VideoCameraOutlined />
          入库
        </span>
        <span className={styles.withIcon}>
          <ReloadOutlined />
          操作
        </span>
      </div>
      {sorted.map((item, index) => {
        const failedPages = item.failedCnt ?? 0;
        const films = item.filmsTotal ?? 0;
        const retry = canRetrySource(record, item);
        const filmRetry = record.trigger === "single_update";
        const sourceKey = `${record.batchId}:${item.sourceId}`;
        const error = String(item.error || "").trim();
        const retryLabel = filmRetry ? "重试该片" : "重试该站";
        return (
          <div className={styles.detailRow} key={item.sourceId || index}>
            <Tooltip title={item.sourceId ? `站点编号 ${item.sourceId}` : undefined}>
              <span className={`${styles.detailName} ${styles.withIcon}`}>
                <CloudServerOutlined />
                <span className={styles.detailNameText}>{item.sourceName}</span>
              </span>
            </Tooltip>
            <RecordStatusMark status={item.status} scope="source" />
            <span className={`${styles.withIcon} ${styles.metric}`}>
              <FileTextOutlined />
              {isSingleFilmRecord(record)
                ? formatCollectPages(
                    { trigger: record.trigger, tag: record.tag, status: item.status },
                    item.pageCurrent,
                    item.pageTotal,
                    failedPages,
                  )
                : `${item.pageCurrent}/${item.pageTotal}`}
              {failedPages > 0 && (
                <span className={`${styles.withIcon} ${styles.metricFail}`}>
                  <CloseCircleOutlined />
                  {failedPages}
                </span>
              )}
            </span>
            <span className={`${styles.withIcon} ${films > 0 ? styles.filmCount : styles.filmCountEmpty}`}>
              <VideoCameraOutlined />
              {formatCollectFilms(films, { ...record, status: item.status })}
            </span>
            <span className={styles.detailActions}>
              {error ? (
                <ErrorDetailButton text={error} title={item.sourceName ? `${item.sourceName} 异常原因` : "异常原因"} />
              ) : null}
              {retry ? (
                <Popconfirm
                  title={filmRetry ? "重试这部影片？" : `重试采集站「${item.sourceName}」？`}
                  description={filmRetry ? "将按原影片重新采集" : "将按本次采集的时间窗口重新执行采集"}
                  onConfirm={() => onRetrySingleSource(record.batchId, item.sourceId)}
                  disabled={!canWrite || retryingSourceKeys.has(sourceKey)}
                >
                  <Button
                    type="text"
                    size="small"
                    icon={<ReloadOutlined />}
                    loading={retryingSourceKeys.has(sourceKey)}
                    disabled={!canWrite}
                    className={styles.actionButton}
                  >
                    {retryLabel}
                  </Button>
                </Popconfirm>
              ) : null}
            </span>
          </div>
        );
      })}
    </div>
  );
}
