"use client";

import { Button, Popconfirm, Progress } from "antd";
import type { CollectQueueProgressView } from "./collect-queue";
import { queueProgressTitle, tourProgressPhase } from "./collect-queue";
import styles from "./index.module.less";

export interface CollectQueueBarItem {
  queueId: string;
  sourceIds: string[];
  seq: number;
  view: CollectQueueProgressView;
  countdown: number | null;
  canStop: boolean;
}

interface CollectQueueBarsProps {
  items: CollectQueueBarItem[];
  tourHoldProgress: boolean;
  stoppingQueueId: string | null;
  canWrite: boolean;
  onStopQueue: (queueId: string, sourceIds: string[]) => void;
}

export default function CollectQueueBars(props: CollectQueueBarsProps) {
  const { items, tourHoldProgress, stoppingQueueId, canWrite, onStopQueue } = props;
  if (items.length === 0) {
    return null;
  }
  const tourIndex = items.findIndex((item) => item.view.running);
  const tourTarget = tourIndex >= 0 ? tourIndex : 0;

  return (
    <div className={styles.batchProgressList}>
      {items.map((item, index) => (
        <div
          key={item.queueId}
          className={styles.batchProgressBar}
          data-tour={index === tourTarget ? "collect-progress" : undefined}
          data-tour-progress={
            index === tourTarget ? tourProgressPhase(item.view) : undefined
          }
        >
          <div className={styles.batchProgressMain}>
            <div className={styles.batchProgressHead}>
              <span className={styles.batchProgressTitle}>
                {queueProgressTitle(item.view.running, items.length, index + 1)}
              </span>
              <span className={styles.batchProgressHeadRight}>
                <span className={styles.batchProgressStats}>
                  {item.view.statsText}
                  {!item.view.running && !tourHoldProgress && item.countdown != null
                    ? ` · ${item.countdown}s 后关闭`
                    : ""}
                </span>
                {item.view.running && item.canStop ? (
                  <Popconfirm
                    title="终止该采集队列？"
                    description="将停止该队列中仍在抓取的采集站；已抓取数据会继续处理完成。其它队列不受影响。"
                    onConfirm={() => onStopQueue(item.queueId, item.sourceIds)}
                    okText="确认终止"
                    cancelText="取消"
                    okButtonProps={{
                      danger: true,
                      loading: stoppingQueueId === item.queueId,
                    }}
                  >
                    <Button
                      danger
                      size="small"
                      loading={stoppingQueueId === item.queueId}
                      disabled={!canWrite}
                      className={styles.batchStopBtn}
                    >
                      终止
                    </Button>
                  </Popconfirm>
                ) : null}
              </span>
            </div>
            <div className={styles.batchProgressRow}>
              <Progress
                percent={item.view.percent}
                status={
                  item.view.running
                    ? "active"
                    : item.view.failedCount > 0
                      ? "normal"
                      : "success"
                }
                strokeColor={item.view.failed > 0 ? "#faad14" : undefined}
                size="small"
                className={styles.batchProgressFill}
              />
            </div>
          </div>
        </div>
      ))}
    </div>
  );
}
