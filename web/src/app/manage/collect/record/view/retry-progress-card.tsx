import React from "react";
import { Progress, Tag } from "antd";
import { LoadingOutlined, CheckCircleOutlined } from "@ant-design/icons";
import { RetryProgressState } from "./types";
import styles from "./index.module.less";

interface RetryProgressCardProps {
  progressState: RetryProgressState | null;
}

export default function RetryProgressCard({ progressState }: RetryProgressCardProps) {
  if (!progressState?.active) {
    return null;
  }

  const percent =
    progressState.total > 0
      ? Math.min(100, Math.round((progressState.completed / progressState.total) * 100))
      : 0;

  return (
    <div className={styles.progressCard}>
      <div className={styles.progressHeader}>
        <div className={styles.progressTitle}>
          {percent < 100 ? (
            <LoadingOutlined style={{ color: "var(--ant-color-primary)" }} />
          ) : (
            <CheckCircleOutlined style={{ color: "#52c41a" }} />
          )}
          <span>{progressState.text}</span>
        </div>
        <div className={styles.progressStats}>
          <Tag color="processing">进度: {percent}%</Tag>
          <span>
            {progressState.completed} / {progressState.total} 条
          </span>
        </div>
      </div>
      <Progress
        percent={percent}
        status={percent >= 100 ? "success" : "active"}
        strokeColor={{ "0%": "#108ee9", "100%": "#87d068" }}
        showInfo={false}
      />
    </div>
  );
}
