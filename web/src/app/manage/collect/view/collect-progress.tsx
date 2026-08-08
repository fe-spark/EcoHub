import { Progress } from "antd";
import { resolveCollectStatusText, type CollectProgress } from "./types";
import styles from "./index.module.less";

interface CollectProgressViewProps {
  progress?: CollectProgress | null;
  /** 主采集站横条等窄空间：更紧凑的排版 */
  compact?: boolean;
}

export default function CollectProgressView({
  progress,
  compact = false,
}: CollectProgressViewProps) {
  // 无进度数据时不渲染任何占位
  if (!progress) {
    return null;
  }

  const total = Math.max(progress.total, 0);
  const finished = Math.max(progress.success + progress.failed, 0);
  const done = Math.min(finished, total || finished);
  const isDone = progress.status === "done";
  const rawPercent = total > 0 ? Math.floor((done / total) * 100) : 0;
  // 未 done 前封顶 99%；等待收尾/发布阶段固定 99%，避免“满页却像没结束”的误解。
  const inPostPagePhase =
    progress.status === "page_done" ||
    progress.status === "waiting_publish" ||
    progress.status === "finalizing";
  // 0 页完成（无新内容）：收尾前 99%，完成后 100%
  const zeroPageFinished =
    total === 0 &&
    (inPostPagePhase || isDone || progress.status === "failed" || progress.status === "stopped");
  const percent = isDone
    ? 100
    : inPostPagePhase || zeroPageFinished
      ? 99
      : progress.status === "starting"
        ? 0
        : Math.min(rawPercent, 99);
  // 与旧表格列一致：展示「等待收尾 / 收尾发布中」等阶段文案
  const statusText = resolveCollectStatusText(progress.status);
  const progressText = total > 0
    ? `${done}/${total}`
    : zeroPageFinished
      ? "无新内容"
      : done > 0
        ? `${done}`
        : progress.status === "starting"
          ? "排队中"
          : "即将开始采集";
  const progressStatus =
    progress.status === "running" ||
    progress.status === "finalizing" ||
    progress.status === "waiting_publish"
      ? "active"
      : progress.status === "failed"
        ? "exception"
        : progress.status === "done"
          ? "success"
          : "normal";
  const progressStrokeColor = progress.failed > 0 ? "#faad14" : undefined;

  const countLine = `${progressText}${progress.failed > 0 ? ` · 失败 ${progress.failed}` : ""}`;
  // 收尾阶段：状态优先；计数作补充，避免只剩 9/9 看不出在发布
  const metaLine = inPostPagePhase
    ? `${statusText}${total > 0 || zeroPageFinished ? ` · ${countLine}` : ""}`
    : countLine;

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
