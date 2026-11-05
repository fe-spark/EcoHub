"use client";

import React, { useState } from "react";
import {
  Drawer,
  Button,
  Popconfirm,
  Alert,
  Typography,
} from "antd";
import {
  CloudServerOutlined,
  ReloadOutlined,
  VideoCameraOutlined,
  ClockCircleOutlined,
  FileTextOutlined,
  FieldTimeOutlined,
  CheckCircleOutlined,
  CloseCircleOutlined,
  ExclamationCircleOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import {
  CollectHistoryRecord,
  CollectRecordSourceDetail,
  canRetryHistorySource,
} from "./types";
import {
  formatCollectFilms,
  formatCollectPages,
  formatDuration,
  formatSourcePages,
  formatSourceWindow,
} from "./record-format";
import {
  isSingleFilmRecord,
  RecordStatusMark,
  renderHourTag,
  renderTriggerTag,
  resolveBatchStatus,
} from "./history-columns";
import { BatchFilmsModal } from "./batch-films-modal";
import styles from "./batch-detail-drawer.module.less";

const { Text } = Typography;

function PageProgressBody({ record }: { record: CollectHistoryRecord }) {
  const pages = formatCollectPages(record);
  const stopped = record.status === "stopped" || record.status === "interrupted";
  const valueClass = pages.alert ? styles.kpiValueFail : stopped ? styles.kpiValueWarn : styles.kpiValueOk;
  const Icon = pages.alert ? CloseCircleOutlined : stopped ? ExclamationCircleOutlined : CheckCircleOutlined;
  return (
    <>
      <span className={valueClass}>
        <Icon /> {pages.primary}
      </span>
      <span className={styles.kpiSub}>{pages.secondary || "全部完成"}</span>
    </>
  );
}

export interface BatchDetailDrawerProps {
  open: boolean;
  record: CollectHistoryRecord | null;
  canWrite: boolean;
  retryingSourceKeys: Set<string>;
  onClose: () => void;
  onRetrySingleSource: (batchId: string, sourceId: string) => void;
}

export function BatchDetailDrawer({
  open,
  record,
  canWrite,
  retryingSourceKeys,
  onClose,
  onRetrySingleSource,
}: BatchDetailDrawerProps) {
  const [modalSource, setModalSource] = useState<{ id: string; name: string } | null>(null);
  if (!record) return null;

  let details: CollectRecordSourceDetail[] = [];
  if (record.detailJson) {
    try {
      details = JSON.parse(record.detailJson) as CollectRecordSourceDetail[];
    } catch {
      details = [];
    }
  }

  const sortedDetails = [...details].sort((a, b) => {
    const rank = (status: string) => (status === "failed" || status === "partial" ? 1 : 0);
    return rank(b.status) - rank(a.status);
  });

  const failedSources = details.filter((d) => d.status === "failed");
  const partialSources = details.filter((d) => d.status === "partial");
  const stoppedSources = details.filter((d) => d.status === "stopped");
  const doneSources = details.filter((d) => d.status === "done" || d.status === "success");
  const abnormalCount = failedSources.length + partialSources.length;

  const filmRetry = record.trigger === "single_update";
  const effectiveMs =
    record.durationMs > 0
      ? record.durationMs
      : record.finishedAt && record.startedAt
        ? Math.max(0, dayjs(record.finishedAt).diff(dayjs(record.startedAt)))
        : 0;

  return (
    <Drawer
      open={open}
      onClose={onClose}
      width={720}
      title={
        <div className={styles.drawerTitleRow}>
          <span className={styles.withIcon}>
            <CloudServerOutlined />
            <span>批次采集成果明细</span>
          </span>
          <RecordStatusMark status={resolveBatchStatus(record)} scope="batch" />
        </div>
      }
      styles={{ body: { padding: "16px 20px" } }}
    >
      <div className={styles.drawerContent}>
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

          <div className={styles.drawerKpiRow}>
            <div className={styles.drawerKpiCard}>
              <div className={styles.drawerKpiHeader}>
                <CloudServerOutlined className={styles.kpiIcon} />
                <span>覆盖站点</span>
              </div>
              <div className={styles.drawerKpiBody}>
                {abnormalCount === 0 && stoppedSources.length === 0 && record.failedSources === 0 && record.pageFailed === 0 && record.status !== "running" && record.status !== "stopped" && record.status !== "interrupted" ? (
                  <>
                    <span className={styles.kpiValueOk}>
                      <CheckCircleOutlined /> {record.sourceCount} 站成功
                    </span>
                    <span className={styles.kpiSub}>全部完成</span>
                  </>
                ) : resolveBatchStatus(record) === "failed" ? (
                  <>
                    <span className={styles.kpiValueFail}>
                      <CloseCircleOutlined /> {abnormalCount || record.failedSources || record.sourceCount} 站失败
                    </span>
                    <span className={styles.kpiSub}>未能采集</span>
                  </>
                ) : abnormalCount === 0 && record.status === "running" ? (
                  <>
                    <span className={styles.kpiValueWarn}>
                      <ExclamationCircleOutlined /> {record.sourceCount} 站采集中
                    </span>
                    <span className={styles.kpiSub}>尚未结算</span>
                  </>
                ) : abnormalCount === 0 ? (
                  <>
                    <span className={styles.kpiValueWarn}>
                      <ExclamationCircleOutlined /> {stoppedSources.length || record.sourceCount} 站已停止
                    </span>
                    <span className={styles.kpiSub}>
                      （{doneSources.length} / {record.sourceCount} 站成功）
                    </span>
                  </>
                ) : record.sourceCount === 1 ? (
                  <>
                    <span className={styles.kpiValueWarn}>
                      <ExclamationCircleOutlined /> 1 站未完成
                    </span>
                    <span className={styles.kpiSub}>部分受阻</span>
                  </>
                ) : (
                  <>
                    <span className={styles.kpiValueWarn}>
                      <ExclamationCircleOutlined />
                      {abnormalCount || record.failedSources || 1} 站未完成
                    </span>
                    <span className={styles.kpiSub}>
                      （{doneSources.length} / {record.sourceCount} 站成功）
                    </span>
                  </>
                )}
              </div>
            </div>

            <div className={styles.drawerKpiCard}>
              <div className={styles.drawerKpiHeader}>
                <FileTextOutlined className={styles.kpiIcon} />
                <span>拉取页码</span>
              </div>
              <div className={styles.drawerKpiBody}>
                {isSingleFilmRecord(record) ? (
                  <>
                    <span className={styles.kpiValueDefault}>单片重放</span>
                    <span className={styles.kpiSub}>1 部</span>
                  </>
                ) : (
                  <PageProgressBody record={record} />
                )}
              </div>
            </div>

            <div className={styles.drawerKpiCard}>
              <div className={styles.drawerKpiHeader}>
                <VideoCameraOutlined className={styles.kpiIcon} />
                <span>更新影片</span>
              </div>
              <div className={styles.drawerKpiBody}>
                {record.filmCount > 0 ? (
                  <>
                    <a
                      className={styles.kpiValueHighlightLink}
                      onClick={() =>
                        setModalSource({
                          id: "",
                          name: "全部站点",
                        })
                      }
                      title="点击查看影片清单"
                    >
                      +{record.filmCount.toLocaleString("zh-CN")} 部
                    </a>
                    <span className={styles.kpiSub}>点击查看清单</span>
                  </>
                ) : (
                  <>
                    <span className={styles.kpiValueMuted}>{formatCollectFilms(0, record)}</span>
                    {record.status === "success" ? (
                      <span className={styles.kpiSub}>0 部</span>
                    ) : null}
                  </>
                )}
              </div>
            </div>
          </div>
        </div>

        {record.errorMsg?.trim() && (
          <Alert
            type="error"
            showIcon
            message="批次异常摘要"
            description={record.errorMsg}
            className={styles.drawerAlert}
          />
        )}

        <div className={styles.drawerSectionHeader}>
          <div className={styles.tableTitle}>各采集源成果与影片详情</div>
          <Text type="secondary" style={{ fontSize: 13 }}>
            共 {sortedDetails.length} 个站点参与采集
          </Text>
        </div>

        {sortedDetails.length === 0 ? (
          <div className={styles.detailEmpty}>暂无单站采集明细快照</div>
        ) : (
          <div className={styles.sourceCardList}>
            {sortedDetails.map((item, index) => {
              const sourceKey = `${record.batchId}:${item.sourceId}`;
              const isRetrying = retryingSourceKeys.has(sourceKey);
              const retry = (item.status === "failed" || item.status === "partial") && canRetryHistorySource(record);
              const shownStatus =
                record.status === "interrupted" && (item.status === "running" || item.status === "starting")
                  ? "interrupted"
                  : item.status;
              const windowLabel = formatSourceWindow(item.hour, record.hour);
              const refTime = record.finishedAt || record.startedAt;
              const isWithinHalfHour = refTime ? dayjs().diff(dayjs(refTime), "minute") < 30 : false;
              const canRetryPages = isWithinHalfHour && Array.isArray(item.failedPages) && item.failedPages.length > 0;

              return (
                <div key={item.sourceId || index} className={styles.sourceCard}>
                  <div className={styles.sourceCardHeader}>
                    <div className={styles.sourceCardTitle}>
                      <CloudServerOutlined />
                      <span className={styles.sourceCardName}>{item.sourceName || item.sourceId}</span>
                      <Text type="secondary" className={styles.sourceCardId}>
                        ({item.sourceId})
                      </Text>
                    </div>
                    <div className={styles.sourceCardStatus}>
                      <RecordStatusMark status={shownStatus} scope="source" />
                      {retry && (
                        <Popconfirm
                          title={filmRetry ? "重试影片" : canRetryPages ? "重试失败页" : "重试该站"}
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
                            {filmRetry
                              ? "重试该片"
                              : canRetryPages
                              ? `重试失败页 (${item.failedPages!.length})`
                              : "重试该站"}
                          </Button>
                        </Popconfirm>
                      )}
                    </div>
                  </div>

                  <div className={styles.sourceCardStats}>
                    <span className={styles.withIcon}>
                      <FileTextOutlined />
                      <span>
                        页码：{formatSourcePages({ ...item, status: shownStatus })}
                        {windowLabel ? ` · ${windowLabel}` : ""}
                      </span>
                    </span>
                    <span className={styles.withIcon}>
                      <VideoCameraOutlined />
                      <span>入库更新：</span>
                      {item.filmsTotal > 0 ? (
                        <a
                          className={styles.filmCountLink}
                          onClick={() =>
                            setModalSource({
                              id: item.sourceId,
                              name: item.sourceName || item.sourceId,
                            })
                          }
                          title="点击查看本次入库影片清单"
                        >
                          +{item.filmsTotal.toLocaleString("zh-CN")} 部
                        </a>
                      ) : (
                        <span className={styles.filmCountEmpty}>{formatCollectFilms(0, { status: shownStatus })}</span>
                      )}
                    </span>
                  </div>

                  {item.error?.trim() && (
                    <div className={styles.sourceCardError}>
                      <span className={styles.sourceCardErrorIcon}>
                        <CloseCircleOutlined />
                      </span>
                      <span className={styles.sourceCardErrorText}>{item.error}</span>
                    </div>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      <BatchFilmsModal
        open={!!modalSource}
        batchId={record.batchId}
        sourceId={modalSource?.id || ""}
        sourceName={modalSource?.name || ""}
        onClose={() => setModalSource(null)}
      />
    </Drawer>
  );
}
