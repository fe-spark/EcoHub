"use client";

import React from "react";
import { Tag } from "antd";
import {
  CloudServerOutlined,
  VideoCameraOutlined,
  ClockCircleOutlined,
  FileTextOutlined,
  FieldTimeOutlined,
} from "@ant-design/icons";
import dayjs from "dayjs";
import { CollectHistoryRecord, CollectRecordSourceDetail } from "./types";
import { formatDuration } from "./record-format";
import {
  isSingleFilmRecord,
  renderHourTag,
  renderTriggerTag,
  resolveBatchStatus,
} from "./history-columns";
import styles from "./batch-overview-cards.module.less";

export interface BatchOverviewCardsProps {
  record: CollectHistoryRecord;
  details: CollectRecordSourceDetail[];
  onOpenFilmsModal: () => void;
}

function isBatchOngoing(status: string | undefined): boolean {
  return status === "running";
}

function renderSourceKpi(
  record: CollectHistoryRecord,
  abnormalCount: number,
  stoppedSources: CollectRecordSourceDetail[],
  doneSources: CollectRecordSourceDetail[]
) {
  const isOngoing = isBatchOngoing(record.status);
  const isStopped = record.status === "stopped" || record.status === "interrupted";
  const isFailed = resolveBatchStatus(record) === "failed";
  const isOk =
    abnormalCount === 0 &&
    stoppedSources.length === 0 &&
    record.failedSources === 0 &&
    record.pageFailed === 0 &&
    !isOngoing &&
    !isStopped;

  let tagNode: React.ReactNode = null;
  let subText = "";

  if (isOngoing) {
    tagNode = <Tag color="processing" bordered={false} className={styles.kpiTag}>采集中</Tag>;
    subText = "尚未结算";
  } else if (isOk) {
    tagNode = <Tag color="success" bordered={false} className={styles.kpiTag}>全部成功</Tag>;
    subText = "全部完成";
  } else if (isFailed) {
    tagNode = <Tag color="error" bordered={false} className={styles.kpiTag}>采集失败</Tag>;
    subText = "未能完成采集";
  } else if (isStopped) {
    tagNode = <Tag color="default" bordered={false} className={styles.kpiTag}>已停止</Tag>;
    subText = `${doneSources.length} / ${record.sourceCount} 站成功`;
  } else {
    const abnormal = abnormalCount || record.failedSources || 1;
    tagNode = <Tag color="warning" bordered={false} className={styles.kpiTag}>{abnormal} 站未完成</Tag>;
    subText = `${doneSources.length} / ${record.sourceCount} 站成功`;
  }

  return (
    <div className={styles.drawerKpiCard}>
      <div className={styles.drawerKpiHeader}>
        <span className={styles.kpiTitleGroup}>
          <CloudServerOutlined className={styles.kpiIcon} />
          <span>覆盖站点</span>
        </span>
        {tagNode}
      </div>
      <div className={styles.drawerKpiBody}>
        <div className={styles.kpiMainNumberRow}>
          <span className={styles.kpiHeroNumber}>{record.sourceCount}</span>
          <span className={styles.kpiUnitSecondary}>站点</span>
        </div>
        <div className={styles.kpiFooterSub} title={subText}>
          {subText}
        </div>
      </div>
    </div>
  );
}

function renderPageKpi(record: CollectHistoryRecord) {
  if (isSingleFilmRecord(record)) {
    return (
      <div className={styles.drawerKpiCard}>
        <div className={styles.drawerKpiHeader}>
          <span className={styles.kpiTitleGroup}>
            <FileTextOutlined className={styles.kpiIcon} />
            <span>拉取页码</span>
          </span>
          <Tag color="purple" bordered={false} className={styles.kpiTag}>单片重放</Tag>
        </div>
        <div className={styles.drawerKpiBody}>
          <div className={styles.kpiMainNumberRow}>
            <span className={styles.kpiHeroNumber}>1</span>
            <span className={styles.kpiUnitSecondary}>部影片</span>
          </div>
          <div className={styles.kpiFooterSub}>单条记录重试</div>
        </div>
      </div>
    );
  }

  const succ = Math.max(0, record.pageSuccess ?? 0);
  const total = Math.max(0, record.pageTotal ?? 0);
  const fail = Math.max(0, record.pageFailed ?? 0);
  const isOngoing = isBatchOngoing(record.status);
  const isStopped = record.status === "stopped" || record.status === "interrupted";
  const isFailed = record.status === "failed";

  let tagNode: React.ReactNode = null;
  let heroNumberNode: React.ReactNode = null;
  let subText = "";

  if (isOngoing) {
    tagNode = <Tag color="processing" bordered={false} className={styles.kpiTag}>拉取中</Tag>;
    if (total > 0) {
      heroNumberNode = (
        <>
          <span className={styles.kpiHeroNumber}>{succ.toLocaleString()}</span>
          <span className={styles.kpiUnitSecondary}>/ {total.toLocaleString()} 页</span>
        </>
      );
      const percent = Math.min(100, Math.floor((succ / total) * 100));
      subText = `进度 ${percent}%${fail > 0 ? ` · ${fail} 页失败` : ""}`;
    } else {
      heroNumberNode = (
        <>
          <span className={styles.kpiHeroNumber}>{succ.toLocaleString()}</span>
          <span className={styles.kpiUnitSecondary}>页</span>
        </>
      );
      subText = "正在探测页码";
    }
  } else if (fail > 0) {
    tagNode = <Tag color="warning" bordered={false} className={styles.kpiTag}>{fail} 页失败</Tag>;
    heroNumberNode = (
      <>
        <span className={styles.kpiHeroNumberWarn}>{succ.toLocaleString()}</span>
        <span className={styles.kpiUnitSecondary}>/ {total > 0 ? total.toLocaleString() : succ.toLocaleString()} 页</span>
      </>
    );
    const percent = total > 0 ? Math.min(100, Number(((succ / total) * 100).toFixed(1))) : 0;
    subText = `成功率 ${percent}%`;
  } else if (isFailed) {
    tagNode = <Tag color="error" bordered={false} className={styles.kpiTag}>拉取失败</Tag>;
    heroNumberNode = (
      <>
        <span className={styles.kpiHeroNumberFail}>0</span>
        <span className={styles.kpiUnitSecondary}>/ {total > 0 ? total.toLocaleString() : "-"} 页</span>
      </>
    );
    subText = total === 0 ? "探测失败" : "抓取失败";
  } else if (isStopped) {
    tagNode = <Tag color="default" bordered={false} className={styles.kpiTag}>已停止</Tag>;
    heroNumberNode = (
      <>
        <span className={styles.kpiHeroNumber}>{succ.toLocaleString()}</span>
        <span className={styles.kpiUnitSecondary}>/ {total > 0 ? total.toLocaleString() : succ.toLocaleString()} 页</span>
      </>
    );
    subText = "中途停止";
  } else {
    tagNode = <Tag color="success" bordered={false} className={styles.kpiTag}>全部完成</Tag>;
    heroNumberNode = (
      <>
        <span className={styles.kpiHeroNumberSuccess}>{total > 0 ? total.toLocaleString() : succ.toLocaleString()}</span>
        <span className={styles.kpiUnitSecondary}>页</span>
      </>
    );
    subText = "全部拉取成功";
  }

  return (
    <div className={styles.drawerKpiCard}>
      <div className={styles.drawerKpiHeader}>
        <span className={styles.kpiTitleGroup}>
          <FileTextOutlined className={styles.kpiIcon} />
          <span>拉取页码</span>
        </span>
        {tagNode}
      </div>
      <div className={styles.drawerKpiBody}>
        <div className={styles.kpiMainNumberRow}>
          {heroNumberNode}
        </div>
        <div className={styles.kpiFooterSub} title={subText}>
          {subText}
        </div>
      </div>
    </div>
  );
}

function renderFilmKpi(record: CollectHistoryRecord, onOpenModal: () => void) {
  const count = record.filmCount ?? 0;
  const isOngoing = isBatchOngoing(record.status);

  let tagNode: React.ReactNode = null;
  let subNode: React.ReactNode = null;

  if (isOngoing) {
    tagNode = (
      <Tag color="processing" bordered={false} className={styles.kpiTag}>
        {count > 0 ? "有更新" : "采集中"}
      </Tag>
    );
    subNode = count > 0 ? (
      <a className={styles.kpiSubLink} onClick={onOpenModal}>
        点击查看影片清单
      </a>
    ) : (
      "正在统计影片"
    );
  } else if (count > 0) {
    tagNode = <Tag color="cyan" bordered={false} className={styles.kpiTag}>已更新</Tag>;
    subNode = (
      <a className={styles.kpiSubLink} onClick={onOpenModal}>
        点击查看清单
      </a>
    );
  } else {
    tagNode = <Tag color="default" bordered={false} className={styles.kpiTag}>无更新</Tag>;
    subNode = "无影片更新";
  }

  return (
    <div className={styles.drawerKpiCard}>
      <div className={styles.drawerKpiHeader}>
        <span className={styles.kpiTitleGroup}>
          <VideoCameraOutlined className={styles.kpiIcon} />
          <span>更新影片</span>
        </span>
        {tagNode}
      </div>
      <div className={styles.drawerKpiBody}>
        <div className={styles.kpiMainNumberRow}>
          {count > 0 ? (
            <a
              className={styles.kpiHeroNumberLink}
              onClick={onOpenModal}
              title="点击查看影片清单"
            >
              +{count.toLocaleString()}
            </a>
          ) : (
            <span className={styles.kpiHeroNumberMuted}>0</span>
          )}
          <span className={styles.kpiUnitSecondary}>部</span>
        </div>
        <div className={styles.kpiFooterSub}>
          {subNode}
        </div>
      </div>
    </div>
  );
}

export function BatchOverviewCards({ record, details, onOpenFilmsModal }: BatchOverviewCardsProps) {
  const failedSources = details.filter((d) => d.status === "failed");
  const partialSources = details.filter((d) => d.status === "partial");
  const stoppedSources = details.filter((d) => d.status === "stopped");
  const doneSources = details.filter((d) => d.status === "done" || d.status === "success");
  const abnormalCount = failedSources.length + partialSources.length;
  const isOngoing = isBatchOngoing(record.status);

  const effectiveMs =
    record.durationMs > 0
      ? record.durationMs
      : record.finishedAt && record.startedAt
        ? Math.max(0, dayjs(record.finishedAt).diff(dayjs(record.startedAt)))
        : isOngoing && record.startedAt
          ? Math.max(0, dayjs().diff(dayjs(record.startedAt)))
          : 0;

  return (
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
        {renderSourceKpi(record, abnormalCount, stoppedSources, doneSources)}
        {renderPageKpi(record)}
        {renderFilmKpi(record, onOpenFilmsModal)}
      </div>
    </div>
  );
}
