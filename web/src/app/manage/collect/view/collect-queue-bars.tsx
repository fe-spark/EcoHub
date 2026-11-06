"use client";

import React from "react";
import { Button, Popconfirm, Progress, Space, Tag } from "antd";
import { CloseOutlined } from "@ant-design/icons";
import type { CollectQueueProgressView, SearchSealBarView } from "./collect-queue";
import { queueProgressTitle, tourProgressPhase } from "./collect-queue";
import styles from "./collect-queue-bars.module.less";

export interface CollectQueueBarItem {
  queueId: string;
  sourceIds: string[];
  seq: number;
  view: CollectQueueProgressView;
  canStop: boolean;
}

interface CollectQueueBarsProps {
  items: CollectQueueBarItem[];
  sealBar?: SearchSealBarView | null;
  stoppingQueueId: string | null;
  canWrite: boolean;
  onStopQueue: (queueId: string, sourceIds: string[]) => void;
}

function pageCount(value: number) {
  return value.toLocaleString("zh-CN");
}

function SearchSealBar({ bar }: { bar: SearchSealBarView }) {
  const indexing = bar.phase === "indexing";
  const percent = indexing ? bar.percent : 0;
  return (
    <div className={`${styles.queueCard} ${styles.queueSealing}`}>
      <div className={styles.queueMain}>
        <div className={styles.queueHeader}>
          <span className={`${styles.statusPill} ${styles.statusSealing}`}>
            <span className={styles.statusDot} />
            {indexing ? "检索封口中" : "待检索"}
          </span>
          <span className={styles.queueTitle}>检索进度</span>
          <span className={styles.queuePages}>
            <span className={styles.pageTotalNum}>
              {indexing && bar.total > 0
                ? `${pageCount(bar.current)} / ${pageCount(bar.total)} 部`
                : "待检索"}
            </span>
          </span>
          <Space size={4} wrap className={styles.queueTags}>
            <Tag color="processing" variant="filled" className={styles.microTag}>
              待检索 {bar.sealingCount} 站
            </Tag>
          </Space>
        </div>
        <div className={styles.progressRow}>
          <Progress
            percent={percent}
            showInfo={false}
            size={{ height: 6 }}
            status="active"
            strokeColor="#1677ff"
            railColor="var(--ant-color-fill-secondary)"
            className={styles.progressBar}
          />
          <span className={`${styles.percentNum} ${styles.percentSealing}`}>
            {indexing ? `${percent}%` : "…"}
          </span>
        </div>
      </div>
    </div>
  );
}

export default function CollectQueueBars(props: CollectQueueBarsProps) {
  const { items, sealBar, stoppingQueueId, canWrite, onStopQueue } = props;
  if (items.length === 0 && !sealBar) {
    return null;
  }
  const tourIndex = items.findIndex((item) => item.view.running);
  const tourTarget = tourIndex >= 0 ? tourIndex : 0;

  return (
    <div className={styles.queueList}>
      {sealBar ? <SearchSealBar bar={sealBar} /> : null}
      {items.map((item, index) => {
        const isRunning = item.view.running;
        const waitingSeal = !isRunning && item.view.sealing;
        const stopping = item.view.stoppingCount > 0;
        // 只有全部在跑的站都进入停止中，队列才显示停止中；个别站停止不影响队列。
        const isStopping = stopping && item.view.runningCount === 0;
        const hasFailed = item.view.failedCount > 0;
        // 只有整队都没跑完才算「已停止」；个别站终止仍按完成收尾。
        const stoppedOnly = item.view.stoppedCount > 0 && item.view.doneCount === 0;
        const busy = isRunning || waitingSeal;

        const cardStateClass = isRunning
          ? styles.queueRunning
          : waitingSeal
            ? styles.queueSealing
            : hasFailed
              ? styles.queueException
              : stoppedOnly
                ? styles.queueStopped
                : styles.queueSuccess;

        const percentStateClass = isRunning
          ? item.view.failed > 0
            ? styles.percentException
            : styles.percentRunning
          : waitingSeal
            ? styles.percentSealing
            : hasFailed
              ? styles.percentException
              : stoppedOnly
                ? styles.percentStopped
                : styles.percentSuccess;

        const progressStrokeColor = isRunning
          ? item.view.failed > 0
            ? { "0%": "#fa8c16", "100%": "#ff4d4f" }
            : { "0%": "#fa8c16", "100%": "#ffa940" }
          : waitingSeal
            ? "#1677ff"
            : hasFailed
              ? "#ff4d4f"
              : stoppedOnly
                ? "#faad14"
                : "#52c41a";

        const statusText = isRunning
          ? isStopping
            ? "停止中"
            : "采集中"
          : waitingSeal
            ? "待检索"
            : hasFailed
              ? "异常"
              : stoppedOnly
                ? "已停止"
                : "已完成";

        const statusClass = isRunning
          ? isStopping
            ? styles.statusStopping
            : item.view.failed > 0
              ? styles.statusRunningFailed
              : styles.statusRunning
          : waitingSeal
            ? styles.statusSealing
            : hasFailed
              ? styles.statusException
              : stoppedOnly
                ? styles.statusStopped
                : styles.statusSuccess;

        const title = queueProgressTitle(busy, items.length, index + 1);
        const progressPercent = item.view.percent;
        const progressText = `${item.view.percent}%`;
        const progressStatus = isRunning || waitingSeal
          ? "active"
          : hasFailed
            ? "exception"
            : stoppedOnly
              ? "normal"
              : "success";

        return (
          <div
            // seq 跟队列号绑定，进度条保持同一节点
            key={item.seq}
            className={`${styles.queueCard} ${cardStateClass}`}
            data-tour={index === tourTarget ? "collect-progress" : undefined}
            data-tour-progress={
              index === tourTarget ? tourProgressPhase(item.view) : undefined
            }
          >
            {/* 左侧主要区域：包含标题状态行与下方的进度条 */}
            <div className={styles.queueMain}>
              <div className={styles.queueHeader}>
                <span className={`${styles.statusPill} ${statusClass}`}>
                  <span className={styles.statusDot} />
                  {statusText}
                </span>
                <span className={styles.queueTitle}>{title}</span>

                <span className={styles.queuePages}>
                  <strong className={styles.pageDoneNum}>
                    {pageCount(item.view.pageDone)}
                  </strong>
                  <span className={styles.pageTotalNum}>
                    / {pageCount(item.view.pageTotal)} 页
                  </span>
                </span>

                <Space size={4} wrap className={styles.queueTags}>
                  <Tag
                    variant="filled"
                    className={`${styles.microTag} ${styles.stationTag}`}
                  >
                    共 {item.view.total} 站
                  </Tag>
                  {item.view.doneCount > 0 && (item.view.running || item.view.sealing) ? (
                    <Tag
                      color="success"
                      variant="filled"
                      className={styles.microTag}
                    >
                      已完成 {item.view.doneCount} 站
                    </Tag>
                  ) : null}
                  {item.view.sealingCount > 0 ? (
                    <Tag
                      color="processing"
                      variant="filled"
                      className={styles.microTag}
                    >
                      待检索 {item.view.sealingCount} 站
                    </Tag>
                  ) : null}
                  {item.view.failedCount > 0 ? (
                    <Tag
                      color="error"
                      variant="filled"
                      className={styles.microTag}
                    >
                      异常 {item.view.failedCount} 站
                    </Tag>
                  ) : null}
                  {item.view.stoppedCount > 0 ? (
                    <Tag variant="filled" className={styles.microTag}>
                      终止 {item.view.stoppedCount} 站
                    </Tag>
                  ) : null}
                  {item.view.failed > 0 ? (
                    <Tag
                      color="warning"
                      variant="filled"
                      className={styles.microTag}
                    >
                      失败 {pageCount(item.view.failed)} 页
                    </Tag>
                  ) : null}
                </Space>
              </div>

              <div className={styles.progressRow}>
                <Progress
                  percent={progressPercent}
                  showInfo={false}
                  size={{ height: 6 }}
                  status={progressStatus}
                  strokeColor={progressStrokeColor}
                  railColor="var(--ant-color-fill-secondary)"
                  className={styles.progressBar}
                />
                <span className={`${styles.percentNum} ${percentStateClass}`}>
                  {progressText}
                </span>
              </div>
            </div>

            {/* 整个横幅的最右侧：关闭样式的终止操作 */}
            {isRunning && item.canStop ? (
              <div className={styles.queueCloseWrap}>
                <Popconfirm
                  title="终止该队列？"
                  onConfirm={() => onStopQueue(item.queueId, item.sourceIds)}
                  okText="终止"
                  cancelText="取消"
                  okButtonProps={{
                    danger: true,
                    loading: stoppingQueueId === item.queueId || stopping,
                  }}
                >
                  <Button
                    type="text"
                    size="small"
                    aria-label="终止"
                    icon={<CloseOutlined />}
                    loading={stoppingQueueId === item.queueId || stopping}
                    disabled={!canWrite}
                    className={styles.queueCloseBtn}
                  />
                </Popconfirm>
              </div>
            ) : null}
          </div>
        );
      })}
    </div>
  );
}
