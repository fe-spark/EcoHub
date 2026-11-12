"use client";

import React, { useState } from "react";
import {
  Drawer,
  Button,
  Popconfirm,
  Typography,
} from "antd";
import {
  CloudServerOutlined,
  ReloadOutlined,
  VideoCameraOutlined,
  FileTextOutlined,
  CloseCircleOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import {
  CollectHistoryRecord,
  CollectRecordSourceDetail,
  canRetryHistorySource,
} from "./types";
import {
  formatCollectFilms,
  formatSourcePages,
  formatSourceWindow,
} from "./record-format";
import {
  MarkedFailureText,
  RecordStatusMark,
  resolveBatchStatus,
} from "./history-columns";
import { BatchFilmsModal } from "./batch-films-modal";
import { BatchOverviewCards } from "./batch-overview-cards";
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
            <span>批次采集成果明细</span>
          </span>
          <RecordStatusMark
            status={resolveBatchStatus(record)}
            scope="batch"
            hasPageFailed={(record.pageFailed ?? 0) > 0}
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
          <div className={styles.tableTitle}>各采集源成果与影片详情</div>
          <Text type="secondary" style={{ fontSize: 13 }}>
            共 {sortedDetails.length || record.sourceCount} 个站点参与采集
          </Text>
        </div>

        {sortedDetails.length === 0 ? (
          <div className={styles.detailEmpty}>
            {record.status === "running" ? "正在初始化单站采集数据..." : "暂无单站采集明细快照"}
          </div>
        ) : (
          <div className={styles.sourceCardList}>
            {sortedDetails.map((item, index) => {
              const sourceKey = `${record.batchId}:${item.sourceId}`;
              const isRetrying = retryingSourceKeys.has(sourceKey);
              const retry = (item.status === "failed" || item.status === "partial") && canRetryHistorySource(record);
              const isSourceInterrupted =
                record.status === "interrupted" && (item.status === "running" || item.status === "starting");
              const shownStatus = isSourceInterrupted ? "interrupted" : item.status;
              const isSourceOngoing =
                shownStatus === "running" || shownStatus === "starting" || shownStatus === "waiting";
              const windowLabel = formatSourceWindow(item.hour, record.hour);
              const refTime = record.finishedAt || record.startedAt;
              const isWithinHalfHour = refTime ? dayjs().diff(dayjs(refTime), "minute") < 30 : false;
              const failedPages = Math.max(0, item.failedCnt ?? 0, item.failedPages?.length ?? 0);
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
                        hasPageFailed={failedPages > 0}
                      />
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
                        页码：
                        <MarkedFailureText
                          text={`${formatSourcePages({ ...item, status: shownStatus })}${windowLabel ? ` · ${windowLabel}` : ""}`}
                          failClassName={styles.metricFail}
                        />
                      </span>
                    </span>
                    <span className={styles.withIcon}>
                      <VideoCameraOutlined />
                      {item.filmsTotal > 0 ? (
                        <>
                          <span>更新：</span>
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
                        </>
                      ) : isSourceOngoing ? (
                        <span className={styles.filmCountEmpty}>更新中</span>
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
