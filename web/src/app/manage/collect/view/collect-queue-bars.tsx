"use client";

import React from "react";
import { Button, Popconfirm, Progress, Space, Tag } from "antd";
import { CloseOutlined } from "@ant-design/icons";
import type { CollectQueueProgressView } from "./collect-queue";
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
  stoppingQueueId: string | null;
  canWrite: boolean;
  onStopQueue: (queueId: string, sourceIds: string[]) => void;
}

function pageCount(value: number) {
  return value.toLocaleString("zh-CN");
}

export default function CollectQueueBars(props: CollectQueueBarsProps) {
  const { items, stoppingQueueId, canWrite, onStopQueue } = props;
  if (items.length === 0) {
    return null;
  }
  const tourIndex = items.findIndex((item) => item.view.running);
  const tourTarget = tourIndex >= 0 ? tourIndex : 0;

  return (
    <div className={styles.queueList}>
      {items.map((item, index) => {
        const isRunning = item.view.running;
        const isFinalizing = Boolean(item.view.finalizing);
        const isStopping = item.view.stoppingCount > 0;
        const hasFailed = item.view.failedCount > 0;
        const cardStateClass = isRunning
          ? styles.queueRunning
          : hasFailed
            ? styles.queueException
            : styles.queueSuccess;

        const percentStateClass = isRunning
          ? styles.percentRunning
          : hasFailed
            ? styles.percentException
            : styles.percentSuccess;

        const progressStrokeColor = isRunning
          ? item.view.failed > 0
            ? { "0%": "#fa8c16", "100%": "#ff4d4f" }
            : { "0%": "#fa8c16", "100%": "#ffa940" }
          : hasFailed
            ? "#ff4d4f"
            : "#52c41a";

        return (
          <div
            // seq 在乐观队列号换成服务端队列号时不变，进度条保持同一节点
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
                <span
                  className={`${styles.statusPill} ${
                    isRunning
                      ? isStopping
                        ? styles.statusStopping
                        : styles.statusRunning
                      : hasFailed
                        ? styles.statusException
                        : styles.statusSuccess
                  }`}
                >
                  <span className={styles.statusDot} />
                  {isFinalizing
                    ? "收尾中"
                    : isRunning
                      ? isStopping
                        ? "停止中"
                        : "采集中"
                      : hasFailed
                        ? "异常"
                        : "已完成"}
                </span>
                <span className={styles.queueTitle}>
                  {isFinalizing
                    ? (items.length > 1 ? `队列 ${index + 1} 总进度 · 收尾中` : "队列总进度 · 收尾中")
                    : queueProgressTitle(item.view.running, items.length, index + 1)}
                </span>

                <span className={styles.queuePages}>
                  {isFinalizing ? (
                    <>
                      <strong className={styles.pageDoneNum}>
                        {item.view.finalizingTotal && item.view.finalizingTotal > 0
                          ? `检索索引 ${pageCount(item.view.finalizingCurrent || 0)}`
                          : "检索索引构建中"}
                      </strong>
                      {item.view.finalizingTotal && item.view.finalizingTotal > 0 ? (
                        <span className={styles.pageTotalNum}>
                          / {pageCount(item.view.finalizingTotal)} 部
                        </span>
                      ) : null}
                    </>
                  ) : (
                    <>
                      <strong className={styles.pageDoneNum}>
                        {pageCount(item.view.pageDone)}
                      </strong>
                      <span className={styles.pageTotalNum}>
                        / {pageCount(item.view.pageTotal)} 页
                      </span>
                    </>
                  )}
                </span>

                <Space size={4} wrap className={styles.queueTags}>
                  {isFinalizing ? (
                    <Tag color="processing" variant="filled" className={styles.microTag}>
                      检索索引 {item.view.finalizingPercent || 0}%
                    </Tag>
                  ) : null}
                  <Tag
                    variant="filled"
                    className={`${styles.microTag} ${styles.stationTag}`}
                  >
                    共 {item.view.total} 站
                  </Tag>
                  {item.view.doneCount > 0 && item.view.fetchingCount > 0 ? (
                    <Tag
                      color="success"
                      variant="filled"
                      className={styles.microTag}
                    >
                      已完成 {item.view.doneCount} 站
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

              {/* 第二行：进度条与末尾的进度百分比 */}
              <div className={styles.progressRow}>
                <Progress
                  percent={item.view.percent}
                  showInfo={false}
                  size={{ height: 6 }}
                  status={
                    isRunning
                      ? "active"
                      : hasFailed
                        ? "exception"
                        : "success"
                  }
                  strokeColor={progressStrokeColor}
                  railColor="var(--ant-color-fill-secondary)"
                  className={styles.progressBar}
                />
                <span className={`${styles.percentNum} ${percentStateClass}`}>
                  {item.view.percent}%
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
                    loading: stoppingQueueId === item.queueId,
                  }}
                >
                  <Button
                    type="text"
                    size="small"
                    aria-label="终止"
                    icon={<CloseOutlined />}
                    loading={stoppingQueueId === item.queueId}
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
