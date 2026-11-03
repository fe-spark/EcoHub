"use client";

import React from "react";
import Link from "next/link";
import {
  Drawer,
  Tag,
  Button,
  Popconfirm,
  Alert,
  Tooltip,
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
} from "@ant-design/icons";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import dayjs from "dayjs";
import {
  CollectHistoryRecord,
  CollectRecordSourceDetail,
  canRetryHistorySource,
} from "./types";
import {
  formatDuration,
  isSingleFilmRecord,
  RecordStatusMark,
  renderHourTag,
  renderTriggerTag,
} from "./history-columns";
import styles from "./index.module.less";

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
          <RecordStatusMark status={record.status} scope="batch" />
        </div>
      }
      styles={{ body: { padding: "16px 20px" } }}
    >
      <div className={styles.drawerContent}>
        <div className={styles.drawerMetaGrid}>
          <div className={styles.drawerMetaItem}>
            <span className={styles.drawerMetaLabel}>采集方式</span>
            <div className={styles.drawerMetaValue}>
              {renderTriggerTag(record.trigger, record.tag)}
            </div>
          </div>
          <div className={styles.drawerMetaItem}>
            <span className={styles.drawerMetaLabel}>采集时间</span>
            <div className={styles.drawerMetaValue}>
              {renderHourTag(record.hour, record)}
            </div>
          </div>
          <div className={styles.drawerMetaItem}>
            <span className={styles.drawerMetaLabel}>运行耗时</span>
            <span className={styles.drawerMetaValue}>
              <ClockCircleOutlined />
              {formatDuration(effectiveMs)}
            </span>
          </div>
          <div className={styles.drawerMetaItem}>
            <span className={styles.drawerMetaLabel}>总入库影片</span>
            <span className={`${styles.drawerMetaValue} ${record.filmCount > 0 ? styles.filmCount : styles.filmCountEmpty}`}>
              <VideoCameraOutlined />
              {record.filmCount > 0 ? `${record.filmCount} 部` : "无变更"}
            </span>
          </div>
          <div className={styles.drawerMetaItem}>
            <span className={styles.drawerMetaLabel}>站点完成度</span>
            <span className={styles.drawerMetaValue}>
              <CheckCircleOutlined className={styles.statusOk} />
              {record.successSources}/{record.sourceCount} 成功
              {record.failedSources > 0 && (
                <span className={styles.metricFail}>
                  （{record.failedSources} 失败）
                </span>
              )}
            </span>
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
              const films = item.films || [];
              const failedPages = item.failedCnt ?? 0;

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
                      <RecordStatusMark status={item.status} scope="source" />
                      {retry && (
                        <Popconfirm
                          title={filmRetry ? "重试这部影片？" : `重试采集站「${item.sourceName}」？`}
                          description={filmRetry ? "将按原影片重新采集" : "将按本次采集的时间窗口重新执行采集"}
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
                            {filmRetry ? "重试该片" : "重试该站"}
                          </Button>
                        </Popconfirm>
                      )}
                    </div>
                  </div>

                  <div className={styles.sourceCardStats}>
                    <span className={styles.withIcon}>
                      <FileTextOutlined />
                      <span>页码：{item.pageCurrent}/{item.pageTotal}</span>
                      {failedPages > 0 && (
                        <span className={styles.metricFail}>（失败 {failedPages} 页）</span>
                      )}
                    </span>
                    <span className={styles.withIcon}>
                      <VideoCameraOutlined />
                      <span>入库更新：</span>
                      <span className={item.filmsTotal > 0 ? styles.filmCount : styles.filmCountEmpty}>
                        {item.filmsTotal > 0 ? `${item.filmsTotal} 部` : "无变更"}
                      </span>
                    </span>
                  </div>

                  {item.error?.trim() && (
                    <div className={styles.sourceCardError}>
                      <CloseCircleOutlined className={styles.metricFail} />
                      <span>{item.error}</span>
                    </div>
                  )}

                  <div className={styles.sourceFilmsContainer}>
                    <div className={styles.sourceFilmsTitle}>
                      <VideoCameraOutlined />
                      <span>
                        本站更新影片（
                        {item.filmsTotal > 0
                          ? `${item.filmsTotal.toLocaleString("zh-CN")} 部`
                          : "0 部"}
                        ）：
                      </span>
                    </div>

                    {films.length > 0 ? (
                      <>
                        <div className={styles.filmTagGrid}>
                          {films.map((film) => (
                            <Tooltip key={film.mid} title={`点击进入播放页 (MID: ${film.mid})`}>
                              <Link
                                href={resolvePlayEntryPath(film.mid)}
                                target="_blank"
                                className={styles.filmTagLink}
                              >
                                <Tag icon={<VideoCameraOutlined />} className={styles.filmTag}>
                                  {film.name}
                                </Tag>
                              </Link>
                            </Tooltip>
                          ))}
                        </div>
                        {item.filmsTotal > films.length && (
                          <div className={styles.sourceMoreFilms}>
                            <Text type="secondary" className={styles.sourceMoreTip}>
                              已展示前 {films.length} 部样例，本站共更新{" "}
                              {item.filmsTotal.toLocaleString("zh-CN")} 部。
                            </Text>
                            <Link
                              href={`/manage/film?sourceId=${encodeURIComponent(item.sourceId)}`}
                              target="_blank"
                              className={styles.viewInFilmManageLink}
                            >
                              前往影片库分页查看该站全部影片 →
                            </Link>
                          </div>
                        )}
                      </>
                    ) : item.filmsTotal > 0 ? (
                      <div className={styles.sourceMoreFilms}>
                        <Text type="secondary" className={styles.sourceMoreTip}>
                          已入库 {item.filmsTotal.toLocaleString("zh-CN")}{" "}
                          部影片（存量快照未记录影片名称）。
                        </Text>
                        <Link
                          href={`/manage/film?sourceId=${encodeURIComponent(item.sourceId)}`}
                          target="_blank"
                          className={styles.viewInFilmManageLink}
                        >
                          前往影片库分页查看 →
                        </Link>
                      </div>
                    ) : item.status === "failed" ? (
                      <Text type="secondary" className={styles.sourceNoFilms}>
                        采集失败，未采集到影片
                      </Text>
                    ) : (
                      <Text type="secondary" className={styles.sourceNoFilms}>
                        本轮采集完成，该站未产生新增或更新的影片
                      </Text>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </div>
    </Drawer>
  );
}
