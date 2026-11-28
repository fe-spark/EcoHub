"use client";

import React, { useEffect, useState } from "react";
import { Drawer, Button, Popconfirm, Tag, Empty } from "antd";
import {
  CloudServerOutlined,
  ReloadOutlined,
  SyncOutlined,
  CloseCircleOutlined,
  RightOutlined,
  DownOutlined,
  FieldTimeOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import {
  CollectHistoryRecord,
  CollectRecordSourceDetail,
  canRetryHistoryRecord,
  canRetryHistorySource,
  isSingleFilmRecord,
  resolveBatchStatus,
} from "./types";
import {
  effectiveDurationMs,
  formatBatchPageSummary,
  formatCollectFilms,
  formatDuration,
  formatSourceWindow,
  formatStartedAt,
  getHourLabel,
  parseCollectError,
  parseSourceDetails,
  resolveBatchFilmIngress,
} from "./record-format";
import { RecordStatusMark, renderTriggerTag } from "./history-columns";
import { ErrorDetailModal } from "./error-detail-modal";
import styles from "./batch-detail-drawer.module.less";

export interface BatchDetailDrawerProps {
  open: boolean;
  record: CollectHistoryRecord | null;
  primaryId?: string;
  canWrite: boolean;
  retryingBatchIds: Set<string>;
  retryingSourceKeys: Set<string>;
  onClose: () => void;
  onRetryBatch: (record: CollectHistoryRecord) => void;
  onRetrySingleSource: (batchId: string, sourceId: string) => void;
}

function normalizeSourceStatus(
  item: CollectRecordSourceDetail,
  batchStatus: string,
): string {
  const status = item.status as string;
  if (batchStatus === "interrupted" && (status === "running" || status === "starting")) {
    return "interrupted";
  }
  return item.status;
}

function isAbnormalStatus(status: string): boolean {
  return status === "failed" || status === "partial" || status === "interrupted";
}

function summaryAccentClass(status: string): string {
  switch (status) {
    case "success":
      return styles.accentSuccess;
    case "partial":
    case "partial_stopped":
      return styles.accentWarning;
    case "failed":
    case "interrupted":
      return styles.accentError;
    case "running":
      return styles.accentPrimary;
    default:
      return styles.accentMuted;
  }
}

export function BatchDetailDrawer({
  open,
  record,
  primaryId,
  canWrite,
  retryingBatchIds,
  retryingSourceKeys,
  onClose,
  onRetryBatch,
  onRetrySingleSource,
}: BatchDetailDrawerProps) {
  const [errorSource, setErrorSource] = useState<CollectRecordSourceDetail | null>(null);
  const [normalExpanded, setNormalExpanded] = useState(false);

  useEffect(() => {
    if (!open) {
      setErrorSource(null);
      setNormalExpanded(false);
    }
  }, [open]);

  if (!record) return null;

  const batchStatus = resolveBatchStatus(record);
  const isRunning = batchStatus === "running";
  const singleFilm = isSingleFilmRecord(record);
  const details = parseSourceDetails(record.detailJson);

  const normalizedDetails = details.map((item) => ({
    item,
    shownStatus: normalizeSourceStatus(item, batchStatus),
  }));

  const abnormalDetails = normalizedDetails.filter((d) => isAbnormalStatus(d.shownStatus));
  const normalDetails = normalizedDetails.filter((d) => !isAbnormalStatus(d.shownStatus));
  const showNormalList = abnormalDetails.length === 0 || normalExpanded;

  const totalSites = details.length || record.sourceCount;
  const doneCount = normalDetails.filter(
    (d) => d.shownStatus === "done" || d.shownStatus === "success",
  ).length;
  const stoppedCount = normalDetails.filter((d) => d.shownStatus === "stopped").length;
  const abnormalCount = abnormalDetails.length;

  const donePct = totalSites > 0 ? Math.round((doneCount / totalSites) * 100) : 0;
  const abnormalPct = totalSites > 0 ? Math.round((abnormalCount / totalSites) * 100) : 0;
  const stoppedPct = totalSites > 0 ? Math.max(0, 100 - donePct - abnormalPct) : 0;

  const hourLabel = getHourLabel(record.hour, record);
  const started = formatStartedAt(record.startedAt);
  const duration = formatDuration(effectiveDurationMs(record));

  const canRetryBatch = canRetryHistoryRecord(record);
  const isRetryingBatch = retryingBatchIds.has(record.batchId);

  const refTime = record.finishedAt || record.startedAt;
  const isWithinHalfHour = refTime ? dayjs().diff(dayjs(refTime), "minute") < 30 : false;

  const ingress = resolveBatchFilmIngress(record, details, primaryId);
  const pageSummary = formatBatchPageSummary(record, details);
  const ingressTitle = singleFilm
    ? "单片入库"
    : details.length === 1
      ? "单站入库"
      : "主站入库";
  const ingressNote = ingress.subLabel.replace(/^主站「(.+)」$/, "$1");

  return (
    <Drawer
      open={open}
      onClose={onClose}
      size={680}
      title={
        <div className={styles.drawerTitleRow}>
          <span className={styles.titleLeft}>
            <CloudServerOutlined />
            <span>采集详情</span>
            <RecordStatusMark status={batchStatus} scope="batch" />
          </span>
          {canRetryBatch ? (
            <Popconfirm
              title={singleFilm ? "重试该片" : "重试失败站点"}
              onConfirm={() => onRetryBatch(record)}
              disabled={!canWrite || isRetryingBatch}
            >
              <Button
                type="primary"
                size="small"
                icon={<ReloadOutlined />}
                loading={isRetryingBatch}
                disabled={!canWrite}
              >
                {singleFilm ? "重试该片" : "重试失败站点"}
              </Button>
            </Popconfirm>
          ) : null}
        </div>
      }
      styles={{ body: { padding: "16px 20px" } }}
    >
      <div className={styles.drawerContent}>
        <div className={`${styles.summaryBanner} ${summaryAccentClass(batchStatus)}`}>
          <div className={styles.metaLine}>
            <span className={styles.metaTime}>{started.primary}</span>
            <span className={styles.metaDot}>·</span>
            {renderTriggerTag(record.trigger)}
            {hourLabel ? (
              <>
                <span className={styles.metaDot}>·</span>
                <span className={styles.metaTag}>{hourLabel}</span>
              </>
            ) : null}
            <span className={styles.metaDot}>·</span>
            <span className={styles.metaDuration}>
              <FieldTimeOutlined />
              {isRunning ? `已耗时 ${duration}` : `耗时 ${duration}`}
            </span>
          </div>

          <div className={styles.summaryMetrics}>
            <div className={styles.summaryMetric}>
              <span className={styles.metricLabel}>{ingressTitle}</span>
              <div className={styles.metricValRow}>
                <span className={ingress.tone === "active" ? styles.metricValue : styles.metricMuted}>
                  {ingress.label}
                </span>
                {ingressNote ? (
                  <span className={styles.metricInlineNote}>{ingressNote}</span>
                ) : null}
              </div>
            </div>

            <div className={styles.summaryMetric}>
              <span className={styles.metricLabel}>分页完成</span>
              <div className={styles.metricValRow}>
                <span
                  className={
                    pageSummary.tone === "muted" ? styles.metricMuted : styles.metricValue
                  }
                >
                  {pageSummary.primary}
                </span>
                {pageSummary.failedText || pageSummary.secondary ? (
                  <span
                    className={
                      pageSummary.tone === "fail"
                        ? `${styles.metricInlineNote} ${styles.breakdownError}`
                        : styles.metricInlineNote
                    }
                  >
                    {[pageSummary.failedText, pageSummary.secondary].filter(Boolean).join(" · ")}
                  </span>
                ) : null}
              </div>
            </div>
          </div>

          <div className={styles.siteSummarySection}>
            <div className={styles.siteSummaryHeader}>
              <span className={styles.metricLabel}>站点汇总</span>
              {!isRunning && details.length > 0 ? (
                <div className={styles.siteBreakdownText}>
                  <span className={styles.metricCountTotal}>共 {totalSites} 站</span>
                  <span>{doneCount} 成功</span>
                  {abnormalCount > 0 ? (
                    <span className={styles.breakdownError}>{abnormalCount} 异常</span>
                  ) : null}
                  {stoppedCount > 0 ? <span>{stoppedCount} 已停止</span> : null}
                </div>
              ) : (
                <span className={styles.metricCountTotal}>
                  {isRunning ? "采集中" : totalSites > 0 ? `共 ${totalSites} 站` : "—"}
                </span>
              )}
            </div>
            {!isRunning && details.length > 0 ? (
              <div className={styles.siteProgressBar}>
                {donePct > 0 ? (
                  <div className={styles.segSuccess} style={{ width: `${donePct}%` }} />
                ) : null}
                {abnormalPct > 0 ? (
                  <div className={styles.segError} style={{ width: `${abnormalPct}%` }} />
                ) : null}
                {stoppedCount > 0 && stoppedPct > 0 ? (
                  <div className={styles.segMuted} style={{ width: `${stoppedPct}%` }} />
                ) : null}
              </div>
            ) : null}
          </div>
        </div>

        {isRunning || details.length === 0 ? (
          <div className={styles.emptyBlock}>
            {isRunning ? (
              <span className={styles.runningInline}>
                <SyncOutlined spin />
                <span>采集中</span>
              </span>
            ) : (
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无站点明细" />
            )}
          </div>
        ) : (
          <>
            {abnormalDetails.length > 0 ? (
              <section className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <span className={styles.sectionTitle}>异常站点 ({abnormalDetails.length})</span>
                </div>
                <div className={styles.abnormalList}>
                  {abnormalDetails.map(({ item, shownStatus }, index) => {
                    const sourceKey = `${record.batchId}:${item.sourceId}`;
                    const isRetrying = retryingSourceKeys.has(sourceKey);
                    const canRetry =
                      (item.status === "failed" || item.status === "partial") &&
                      canRetryHistorySource(record);
                    const windowLabel = formatSourceWindow(item.hour, record.hour);
                    const canRetryPages =
                      isWithinHalfHour && Array.isArray(item.failedPages) && item.failedPages.length > 0;
                    const parsedErr = item.error ? parseCollectError(item.error) : null;
                    const hasErrorModal = Boolean(
                      item.error?.trim() || (item.failedPages?.length ?? 0) > 0,
                    );

                    return (
                      <div key={item.sourceId || index} className={styles.abnormalCard}>
                        <div className={styles.cardHeader}>
                          <div className={styles.cardTitleGroup}>
                            <span className={styles.sourceName}>{item.sourceName || item.sourceId}</span>
                            {primaryId && item.sourceId === primaryId ? (
                              <Tag color="gold" variant="filled" style={{ marginInlineEnd: 0 }}>
                                主站
                              </Tag>
                            ) : null}
                            {windowLabel ? <Tag variant="filled">{windowLabel}</Tag> : null}
                            <RecordStatusMark status={shownStatus} scope="source" />
                          </div>
                          {canRetry ? (
                            <Popconfirm
                              title={
                                singleFilm
                                  ? "重试该片"
                                  : canRetryPages
                                    ? `重试 ${item.failedPages!.length} 个失败页`
                                    : "重采该站"
                              }
                              onConfirm={() => onRetrySingleSource(record.batchId, item.sourceId)}
                              disabled={!canWrite || isRetrying}
                            >
                              <Button
                                type="primary"
                                size="small"
                                icon={<ReloadOutlined />}
                                loading={isRetrying}
                                disabled={!canWrite}
                              >
                                {singleFilm
                                  ? "重试该片"
                                  : canRetryPages
                                    ? `重试失败页 (${item.failedPages!.length})`
                                    : "重采该站"}
                              </Button>
                            </Popconfirm>
                          ) : null}
                        </div>

                        <div className={styles.cardStats}>
                          {item.successCnt > 0 || item.failedCnt > 0 ? (
                            <span>
                              页数 {item.successCnt} 成功 / {item.failedCnt} 失败
                            </span>
                          ) : null}
                          {item.filmsTotal > 0 ? (
                            <span>更新 +{item.filmsTotal.toLocaleString("zh-CN")} 部</span>
                          ) : (
                            <span>{formatCollectFilms(0, { status: shownStatus })}</span>
                          )}
                        </div>

                        {hasErrorModal ? (
                          <button
                            type="button"
                            className={styles.errorBar}
                            onClick={() => setErrorSource(item)}
                          >
                            <CloseCircleOutlined className={styles.errorBarIcon} />
                            {parsedErr?.summary ? (
                              <span className={styles.errorBarBadge}>{parsedErr.summary}</span>
                            ) : null}
                            <span className={styles.errorBarText}>
                              {parsedErr?.detail || `失败 ${item.failedPages?.length ?? 0} 页`}
                            </span>
                            <span className={styles.errorBarAction}>详情</span>
                          </button>
                        ) : null}
                      </div>
                    );
                  })}
                </div>
              </section>
            ) : null}

            {normalDetails.length > 0 ? (
              <section className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <span className={styles.sectionTitle}>
                    {abnormalDetails.length > 0
                      ? `其他站点 (${normalDetails.length})`
                      : `站点明细 (${normalDetails.length})`}
                  </span>
                  {abnormalDetails.length > 0 ? (
                    <Button
                      type="link"
                      size="small"
                      icon={normalExpanded ? <DownOutlined /> : <RightOutlined />}
                      onClick={() => setNormalExpanded((v) => !v)}
                    >
                      {normalExpanded ? "收起" : "展开"}
                    </Button>
                  ) : null}
                </div>

                {showNormalList ? (
                  <div className={styles.compactList}>
                    {normalDetails.map(({ item, shownStatus }, index) => {
                      const windowLabel = formatSourceWindow(item.hour, record.hour);
                      const sourceRunning = shownStatus === "running";
                      return (
                        <div key={item.sourceId || index} className={styles.compactRow}>
                          <div className={styles.compactLeft}>
                            <span
                              className={styles.sourceName}
                              title={item.sourceName || item.sourceId}
                            >
                              {item.sourceName || item.sourceId}
                            </span>
                            {primaryId && item.sourceId === primaryId ? (
                              <Tag color="gold" variant="filled" style={{ marginInlineEnd: 0 }}>
                                主站
                              </Tag>
                            ) : null}
                            {windowLabel ? <span className={styles.windowBadge}>{windowLabel}</span> : null}
                          </div>
                          <div className={styles.compactRight}>
                            <span className={styles.compactPages}>
                              {!sourceRunning && item.successCnt > 0 ? `${item.successCnt} 页` : "—"}
                            </span>
                            <div className={styles.compactStatus}>
                              <RecordStatusMark status={shownStatus} scope="source" />
                            </div>
                            <span className={styles.compactFilms}>
                              {sourceRunning ? (
                                "—"
                              ) : item.filmsTotal > 0 ? (
                                `+${item.filmsTotal.toLocaleString("zh-CN")} 部`
                              ) : (
                                formatCollectFilms(0, { status: shownStatus })
                              )}
                            </span>
                          </div>
                        </div>
                      );
                    })}
                  </div>
                ) : null}
              </section>
            ) : null}
          </>
        )}
      </div>

      <ErrorDetailModal
        open={open && !!errorSource}
        title={errorSource?.sourceName ? `${errorSource.sourceName} 异常原因` : "异常原因"}
        text={errorSource?.error || ""}
        pages={errorSource?.failedPages}
        onClose={() => setErrorSource(null)}
      />
    </Drawer>
  );
}
