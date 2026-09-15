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
  const isWebdav = progress?.kind === "webdav";
  const total = Math.max(progress?.total ?? 0, 0);
  const failed = progress?.failed ?? 0;
  const finished = Math.max((progress?.success ?? 0) + failed, 0);
  const done = Math.min(finished, total || finished);
  const isDone = progress?.status === "done";
  const rawPercent = total > 0 ? Math.floor((done / total) * 100) : 0;
  // 未 done 前封顶 99%；等待收尾/发布阶段固定 99%，避免“满页却像没结束”的误解。
  const inPostPagePhase =
    progress?.status === "page_done" ||
    progress?.status === "waiting_publish" ||
    progress?.status === "finalizing";
  // 0 页完成（无新内容）：收尾前 99%，完成后 100%
  const zeroPageFinished =
    total === 0 &&
    (inPostPagePhase || isDone || progress?.status === "failed" || progress?.status === "stopped");

  let percent = 0;
  if (!progress) {
    percent = 0;
  } else if (isDone) {
    percent = 100;
  } else if (isWebdav) {
    if (progress.status === "starting") {
      percent = 0;
    } else if (progress.phase === "listing") {
      percent = 20;
    } else if (progress.phase === "parsing") {
      percent = 50;
    } else if (progress.phase === "matching") {
      percent = 80;
    } else if (total > 0) {
      percent = Math.min(rawPercent, 99);
    } else {
      percent = inPostPagePhase ? 99 : 50;
    }
  } else {
    percent = inPostPagePhase || zeroPageFinished
      ? 99
      : progress.status === "starting"
        ? 0
        : Math.min(rawPercent, 99);
  }

  // 与旧表格列一致：展示「等待收尾 / 收尾发布中 / 部分失败」等阶段文案
  const statusText = resolveCollectProgressStatusText(progress);

  let progressText = "未开始";
  if (isWebdav) {
    if (isDone) {
      progressText = `共 ${total} 个文件`;
    } else if (progress.phase === "listing") {
      progressText = progress.found
        ? `正在扫描目录，已发现 ${progress.found} 个文件`
        : "正在扫描目录...";
    } else if (progress.phase === "parsing") {
      progressText = `已发现 ${progress.found ?? total} 个文件，解析中...`;
    } else if (progress.phase === "matching") {
      progressText = `已解析 ${progress.parsed ?? 0} 个文件，匹配主站中...`;
    } else if (total > 0) {
      progressText = `${done}/${total} 个文件`;
    } else if (progress.status === "starting") {
      progressText = "排队中";
    } else {
      progressText = "即将开始扫描";
    }
  } else {
    progressText = !progress
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
  }

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

  const countLine = isWebdav
    ? (isDone
        ? `入库 ${progress?.success ?? 0} · 未匹配 ${progress?.unmatched ?? 0} · 跳过 ${progress?.skipped ?? 0}${failed > 0 ? ` · 失败 ${failed}` : ""}`
        : `${progressText}${failed > 0 ? ` · 失败 ${failed}` : ""}`)
    : `${progressText}${failed > 0 ? ` · 失败 ${failed} 页` : ""}`;
  // 收尾阶段：状态优先；计数作补充，避免只剩 9/9 看不出在发布
  const metaLine = inPostPagePhase
    ? `${statusText}${total > 0 || zeroPageFinished ? ` · ${countLine}` : ""}`
    : countLine;

  // 无进度数据时不渲染任何占位
  if (!progress) {
    return null;
  }

  // 环形进度：仅存在采集任务时渲染，避免无任务卡片出现误导性的 0%
  if (variant === "ring") {
    return (
      <Tooltip
        title={
          progress.error
            ? `${statusText} · ${countLine} · ${progress.error}`
            : `${statusText} · ${countLine}`
        }
      >
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
