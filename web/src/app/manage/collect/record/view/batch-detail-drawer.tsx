"use client";

import React, { useEffect, useState } from "react";
import {
  Drawer,
  Button,
  Popconfirm,
  Typography,
} from "antd";
import {
  CloudServerOutlined,
  ReloadOutlined,
  SyncOutlined,
  VideoCameraOutlined,
  CloseCircleOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import {
  CollectHistoryRecord,
  CollectRecordSourceDetail,
  canRetryHistorySource,
} from "./types";
import { formatCollectFilms, formatSourceWindow } from "./record-format";
import {
  RecordStatusMark,
  resolveBatchStatus,
  sourceActivityLabel,
} from "./history-columns";
import { BatchFilmsModal } from "./batch-films-modal";
import { BatchOverviewCards } from "./batch-overview-cards";
import { ErrorDetailModal } from "./error-detail-button";
import styles from "./batch-detail-drawer.module.less";

const { Text } = Typography;

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
  const [errorSource, setErrorSource] = useState<CollectRecordSourceDetail | null>(null);
  useEffect(() => {
    if (open) return;
    setModalSource(null);
    setErrorSource(null);
  }, [open]);
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

  const filmRetry = record.trigger === "single_update";

  return (
    <Drawer
      open={open}
      onClose={onClose}
      size={720}
      title={
        <div className={styles.drawerTitleRow}>
          <span className={styles.withIcon}>
            <CloudServerOutlined />
            <span>采集记录</span>
          </span>
          <RecordStatusMark
            status={resolveBatchStatus(record)}
            scope="batch"
          />
        </div>
      }
      styles={{ body: { padding: "16px 20px" } }}
    >
      <div className={styles.drawerContent}>
        <BatchOverviewCards
          record={record}
          details={details}
          onOpenFilmsModal={() =>
            setModalSource({
              id: "",
              name: "全部站点",
            })
          }
        />

        <div className={styles.drawerSectionHeader}>
          <div className={styles.tableTitle}>各站结果</div>
          <Text type="secondary" style={{ fontSize: 13 }}>
            共 {sortedDetails.length || record.sourceCount} 个站点参与采集
          </Text>
        </div>

        {sortedDetails.length === 0 ? (
          <div className={record.status === "running" ? styles.sourceCardCollecting : styles.detailEmpty}>
            {record.status === "running" ? (
              <>
                <SyncOutlined spin />
                <span>采集中</span>
              </>
            ) : (
              "暂无单站采集明细"
            )}
          </div>
        ) : (
          <div className={styles.sourceCardList}>
            {sortedDetails.map((item, index) => {
              const sourceKey = `${record.batchId}:${item.sourceId}`;
              const isRetrying = retryingSourceKeys.has(sourceKey);
              const isSourceInterrupted =
                record.status === "interrupted" && (item.status === "running" || item.status === "starting");
              const shownStatus = isSourceInterrupted ? "interrupted" : item.status;
              const activity = sourceActivityLabel(shownStatus);
              const retry = (item.status === "failed" || item.status === "partial") && canRetryHistorySource(record);
              const canOpenFilms = record.ledger === true;
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
                      <RecordStatusMark
                        status={shownStatus}
                        scope="source"
                      />
                      {!activity && retry && (
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

                  {activity ? (
                    <div className={styles.sourceCardCollecting}>
                      {shownStatus === "paused" || shownStatus === "sealing" ? null : <SyncOutlined spin />}
                      <span>{activity}</span>
                    </div>
                  ) : (
                    <>
                      <div className={styles.sourceCardStats}>
                        <span className={styles.withIcon}>
                          <VideoCameraOutlined />
                          {item.filmsTotal > 0 ? (
                            <>
                              <span>更新：</span>
                              {canOpenFilms ? (
                                <a
                                  className={styles.filmCountLink}
                                  onClick={() =>
                                    setModalSource({
                                      id: item.sourceId,
                                      name: item.sourceName || item.sourceId,
                                    })
                                  }
                                  title="查看影片清单"
                                >
                                  +{item.filmsTotal.toLocaleString("zh-CN")} 部
                                </a>
                              ) : (
                                <span className={styles.filmCount}>+{item.filmsTotal.toLocaleString("zh-CN")} 部</span>
                              )}
                            </>
                          ) : (
                            <span className={styles.filmCountEmpty}>{formatCollectFilms(0, { status: shownStatus })}</span>
                          )}
                          {windowLabel ? <span className={styles.filmCountEmpty}>{windowLabel}</span> : null}
                        </span>
                      </div>

                      {(item.error?.trim() || (item.failedPages?.length ?? 0) > 0) && (
                        <button
                          type="button"
                          className={styles.sourceCardError}
                          onClick={() => setErrorSource(item)}
                        >
                          <span className={styles.sourceCardErrorIcon}>
                            <CloseCircleOutlined />
                          </span>
                          <span className={styles.sourceCardErrorText}>
                            {item.error?.trim() || `共 ${item.failedPages?.length ?? 0} 页失败`}
                          </span>
                          <span className={styles.sourceCardErrorAction}>查看</span>
                        </button>
                      )}
                    </>
                  )}
                </div>
              );
            })}
          </div>
        )}
      </div>

      <BatchFilmsModal
        open={open && !!modalSource}
        batchId={record.batchId}
        sourceId={modalSource?.id || ""}
        sourceName={modalSource?.name || ""}
        onClose={() => setModalSource(null)}
      />
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
