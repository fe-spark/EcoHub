"use client";

import { CheckOutlined, CloseOutlined } from "@ant-design/icons";
import { Tooltip, theme } from "antd";
import type { RingTone, RingView } from "./collect-ring-view";
import styles from "./collect-ring.module.less";

const size = 40;
const strokeWidth = 3;

function ringColor(tone: RingTone, token: ReturnType<typeof theme.useToken>["token"]) {
  if (tone === "success") return token.colorSuccess;
  if (tone === "warning") return token.colorWarning;
  if (tone === "error") return token.colorError;
  if (tone === "processing") return token.colorInfo;
  return token.colorPrimary;
}

export default function CollectRing({ view }: { view: RingView }) {
  const { token } = theme.useToken();
  const color = ringColor(view.tone, token);
  const radius = (size - strokeWidth) / 2;
  const circumference = 2 * Math.PI * radius;
  const percent = Math.max(0, Math.min(100, view.percent));
  const dash = (percent / 100) * circumference;
  const center = (size / 2).toFixed(2);
  const isWaiting = view.solid;

  const title = view.failedPages ? (
    <div className={styles.tip}>
      <div>{view.tooltip}</div>
      <div className={styles.tipLabel}>失败页（已记录 {view.failedPages.count} 页）</div>
      <div className={styles.tipPages}>{view.failedPages.text}</div>
    </div>
  ) : (
    view.tooltip
  );

  return (
    // 鼠标可以移进提示框滚动查看；位置随视口自动调整。
    <Tooltip title={title} styles={{ container: { maxWidth: 360 } }} autoAdjustOverflow>
      <span className={`${styles.ring} ${isWaiting ? styles.waitingRing : ""}`} style={{ color }}>
        <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden>
          <circle
            className={styles.track}
            cx={center}
            cy={center}
            r={radius}
            strokeWidth={strokeWidth}
          />
          {isWaiting ? (
            <circle
              className={styles.waitingArc}
              cx={center}
              cy={center}
              r={radius}
              strokeWidth={strokeWidth}
              strokeDasharray={`${(circumference * 0.35).toFixed(2)} ${circumference.toFixed(2)}`}
            />
          ) : percent > 0 ? (
            <circle
              className={styles.arc}
              cx={center}
              cy={center}
              r={radius}
              strokeWidth={strokeWidth}
              strokeDasharray={`${dash} ${circumference}`}
            />
          ) : null}
        </svg>
        {isWaiting && view.center === "empty" ? (
          <span className={styles.waitingCenter} aria-hidden>
            <span className={styles.waitingDot} />
          </span>
        ) : null}
        {view.center === "percent" ? <span className={styles.label}>{percent}%</span> : null}
        {view.center === "success" ? <CheckOutlined className={styles.icon} /> : null}
        {view.center === "fail" ? <CloseOutlined className={styles.icon} /> : null}
      </span>
    </Tooltip>
  );
}
