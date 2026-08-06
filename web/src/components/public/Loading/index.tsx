"use client";

import React, { useEffect, useMemo, useState } from "react";
import styles from "./index.module.less";

interface AppLoadingProps {
  text?: string;
  padding?: string;
  size?: "small" | "default" | "large";
  /** 是否展示轮换趣味副文案（默认 large 开启） */
  showHints?: boolean;
}

const FUN_HINTS = [
  "灯光、摄影、开拍",
  "胶片传送中，请稍候",
  "正在为镜头调焦",
  "好戏即将开场",
  "放映厅已就位",
  "内容整理中，马上回来",
  "导演说再等一镜",
];

/** 去掉尾部省略号，避免与动画点叠加成「......」 */
function normalizeLabel(text: string) {
  return text.replace(/[\s.。…·]+$/u, "").trim() || "加载中";
}

export default function AppLoading({
  text,
  padding,
  size = "large",
  showHints,
}: AppLoadingProps) {
  const loadingText = normalizeLabel(text ?? "加载中");
  const enableHints = showHints ?? size === "large";
  const [hintIndex, setHintIndex] = useState(0);

  useEffect(() => {
    if (!enableHints) {
      return;
    }
    setHintIndex(Math.floor(Math.random() * FUN_HINTS.length));
    const timer = window.setInterval(() => {
      setHintIndex((i) => (i + 1) % FUN_HINTS.length);
    }, 3200);
    return () => window.clearInterval(timer);
  }, [enableHints, loadingText]);

  const sizeClass = useMemo(() => {
    if (size === "small") return styles.small;
    if (size === "default") return styles.default;
    return styles.large;
  }, [size]);

  const containerStyle: React.CSSProperties = padding
    ? {
        padding,
        textAlign: "center",
        display: "flex",
        flexDirection: "column",
        alignItems: "center",
        justifyContent: "center",
      }
    : {
        position: "absolute",
        left: 0,
        top: 0,
        width: "100%",
        height: "100%",
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        flexDirection: "column",
      };

  return (
    <div style={containerStyle}>
      <div className={`${styles.wrap} ${sizeClass}`} role="presentation">
        <div className={styles.stage} aria-hidden="true">
          <span className={styles.glow} />
          <span className={styles.ringOuter} />
          <span className={styles.trackOuter}>
            <span className={`${styles.bead} ${styles.beadLg}`} />
            <span className={`${styles.bead} ${styles.beadOpposite}`} />
          </span>
          <span className={styles.ringInner} />
          <span className={styles.trackInner}>
            <span className={`${styles.bead} ${styles.beadSm}`} />
          </span>
          <span className={styles.core}>
            <svg className={styles.playIcon} viewBox="0 0 24 24" aria-hidden="true">
              <path
                fill="currentColor"
                d="M8.5 6.8c0-.9 1-1.45 1.75-.97l9.1 5.2c.7.4.7 1.44 0 1.84l-9.1 5.2c-.75.43-1.75-.11-1.75-.97V6.8z"
              />
            </svg>
          </span>
        </div>

        <div className={styles.bars} aria-hidden="true">
          <span className={styles.bar} />
          <span className={styles.bar} />
          <span className={styles.bar} />
          <span className={styles.bar} />
          <span className={styles.bar} />
        </div>

        <div className={styles.copy}>
          {loadingText ? (
            <p className={styles.label}>
              {loadingText}
              <span className={styles.dotTrail} aria-hidden="true">
                <span>.</span>
                <span>.</span>
                <span>.</span>
              </span>
            </p>
          ) : null}

          {enableHints ? (
            <p key={hintIndex} className={styles.hint}>
              {FUN_HINTS[hintIndex]}
            </p>
          ) : null}
        </div>
      </div>
    </div>
  );
}
