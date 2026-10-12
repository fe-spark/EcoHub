import { Progress, Tooltip } from "antd";
import { resolveCollectProgressStatusText, type CollectProgress } from "./types";
import styles from "./index.module.less";

interface CollectProgressViewProps {
  progress?: CollectProgress | null;
  /** 主采集站横条等窄空间：更紧凑的排版 */
  compact?: boolean;
  /** 环形进度：固定占位，无进度时显示空环，保证卡片高度稳定 */
  variant?: "bar" | "ring";
}

export default function CollectProgressView({
  progress,
  compact = false,
  variant = "bar",
}: CollectProgressViewProps) {
  // 无进度数据按 0% 处理，环形仍占位
  const total = Math.max(progress?.total ?? 0, 0);
  const failed = progress?.failed ?? 0;
  const finished = Math.max((progress?.success ?? 0) + failed, 0);
  const done = Math.min(finished, total || finished);
  const isDone = progress?.status === "done";
  const rawPercent = total > 0 ? Math.floor((done / total) * 100) : 0;
  const zeroPageFinished =
    total === 0 &&
    (isDone || progress?.status === "failed" || progress?.status === "stopped");
  const percent = !progress
    ? 0
    : isDone
      ? 100
      : progress.status === "starting"
        ? 0
        : Math.min(rawPercent, 99);
  const statusText = resolveCollectProgressStatusText(progress);
  const progressText = !progress
    ? "未开始"
    : total > 0
      ? `${done}/${total} 页`
      : zeroPageFinished
        ? "无新内容"
        : done > 0
          ? `${done} 页`
          : progress.status === "starting"
            ? "排队中"
            : "即将开始采集";
  const isFullFail = progress?.status === "failed" && (progress.success ?? 0) === 0;
  const progressStatus = !progress
    ? "normal"
    : progress.status === "running"
      ? "active"
      : isFullFail
        ? "exception"
        : progress.status === "done"
          ? "success"
          : "normal";
  const progressStrokeColor = failed > 0 && !isFullFail ? "#faad14" : undefined;

  const countLine = `${progressText}${failed > 0 ? ` · 失败 ${failed} 页` : ""}`;
  const metaLine = countLine;

  // 无进度数据时不渲染任何占位
  if (!progress) {
    return null;
  }

  // 环形进度：仅存在采集任务时渲染，避免无任务卡片出现误导性的 0%
  if (variant === "ring") {
    return (
      <Tooltip title={`${statusText} · ${countLine}`}>
        <Progress
          type="circle"
          percent={percent}
          size={40}
          strokeWidth={6}
          status={progressStatus}
          strokeColor={progressStrokeColor}
          className={styles.progressRing}
        />
      </Tooltip>
    );
  }

  // 横条紧凑：一行「百分比 + 条 + 状态/计数」
  if (compact) {
    return (
      <div className={styles.progressInline}>
        <span className={styles.progressPercent}>{percent}%</span>
        <Progress
          percent={percent}
          size="small"
          status={progressStatus}
          strokeColor={progressStrokeColor}
          showInfo={false}
          className={styles.progressInlineBar}
        />
        <span className={styles.progressMetaText}>{metaLine}</span>
      </div>
    );
  }

  return (
    <div className={styles.progressWrap}>
      <div className={styles.progressHead}>
        <span className={styles.progressLabel}>{statusText}</span>
        <span className={styles.progressPercent}>{percent}%</span>
      </div>
      <Progress
        percent={percent}
        size="small"
        status={progressStatus}
        strokeColor={progressStrokeColor}
        showInfo={false}
        className={styles.progressBar}
      />
      <div className={styles.progressMetaText}>{countLine}</div>
    </div>
  );
}
