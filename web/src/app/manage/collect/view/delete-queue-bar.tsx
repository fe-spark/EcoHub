"use client";

import React from "react";
import { Button, Progress, Space, Tag } from "antd";
import { CloseOutlined } from "@ant-design/icons";
import styles from "./collect-queue-bars.module.less";

export interface DeletingQueueItem {
  id: string;
  name: string;
  status: "pending" | "deleting" | "deleted" | "failed";
  deletedFilms?: number;
}

interface DeleteQueueBarProps {
  items: DeletingQueueItem[];
  onDismiss: () => void;
}

export default function DeleteQueueBar({ items, onDismiss }: DeleteQueueBarProps) {
  if (items.length === 0) {
    return null;
  }

  const doneCount = items.filter((i) => i.status === "deleted").length;
  const failedCount = items.filter((i) => i.status === "failed").length;
  const isRunning = items.some((i) => i.status === "pending" || i.status === "deleting");
  const currentDeleting =
    items.find((i) => i.status === "deleting") || items.find((i) => i.status === "pending");
  const totalDeletedFilms = items.reduce((sum, i) => sum + (i.deletedFilms || 0), 0);

  const percent =
    items.length > 0 ? Math.min(100, Math.round(((doneCount + failedCount) / items.length) * 100)) : 0;

  const cardStateClass = isRunning
    ? styles.queueDeleting
    : failedCount > 0
      ? styles.queueException
      : styles.queueSuccess;

  return (
    <div className={styles.queueList} style={{ marginBottom: 8 }}>
      <div className={`${styles.queueCard} ${cardStateClass}`}>
        <div className={styles.queueMain}>
          <div className={styles.queueHeader}>
            <span
              className={`${styles.statusPill} ${
                isRunning
                  ? styles.statusDeleting
                  : failedCount > 0
                    ? styles.statusException
                    : styles.statusSuccess
              }`}
            >
              <span className={styles.statusDot} />
              {isRunning ? "删除中" : failedCount > 0 ? "部分失败" : "已完成"}
            </span>

            <span className={styles.queueTitle}>
              删除队列总进度
            </span>

            <span className={styles.queuePages}>
              <strong className={styles.pageDoneNum}>{doneCount}</strong>
              <span className={styles.pageTotalNum}> / {items.length} 站</span>
            </span>

            <Space size={4} wrap className={styles.queueTags}>
              {isRunning && currentDeleting ? (
                <Tag color="error" variant="filled" className={styles.microTag}>
                  正在级联清理: {currentDeleting.name}
                </Tag>
              ) : null}
              {totalDeletedFilms > 0 ? (
                <Tag color="warning" variant="filled" className={styles.microTag}>
                  已清理 {totalDeletedFilms.toLocaleString()} 部影片档案
                </Tag>
              ) : null}
              {failedCount > 0 ? (
                <Tag color="error" variant="filled" className={styles.microTag}>
                  {failedCount} 站删除失败
                </Tag>
              ) : null}
            </Space>
          </div>

          <div className={styles.progressRow}>
            <Progress
              percent={percent}
              status={isRunning ? "active" : failedCount > 0 ? "exception" : "success"}
              strokeColor={
                isRunning
                  ? { "0%": "#ff4d4f", "100%": "#ff7875" }
                  : failedCount > 0
                    ? "#ff4d4f"
                    : "#52c41a"
              }
              showInfo={false}
              className={styles.progressBar}
            />
            <span
              className={`${styles.percentNum} ${
                isRunning
                  ? styles.percentException
                  : failedCount > 0
                    ? styles.percentException
                    : styles.percentSuccess
              }`}
            >
              {percent}%
            </span>
          </div>
        </div>

        <div className={styles.queueCloseWrap}>
          <Button
            type="text"
            size="small"
            icon={<CloseOutlined />}
            onClick={onDismiss}
            className={styles.closeBtn}
            title="关闭队列通知"
          />
        </div>
      </div>
    </div>
  );
}
