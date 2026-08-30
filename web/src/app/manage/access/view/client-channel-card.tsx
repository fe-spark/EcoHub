"use client";

import React, { useState } from "react";
import { Empty } from "antd";
import styles from "./index.module.less";

export type ClientChannelItem = {
  key: string;
  label: string;
  count: number;
  pct: number;
  color: string;
  icon?: React.ReactNode;
  desc?: string;
};

interface ClientChannelCardProps {
  data: ClientChannelItem[];
  unit?: string;
}

export default function ClientChannelCard({
  data = [],
  unit = "次",
}: ClientChannelCardProps) {
  const [hoverKey, setHoverKey] = useState<string | null>(null);

  const total = data.reduce((sum, item) => sum + (item.count || 0), 0);

  if (total === 0 && data.every((d) => d.count === 0)) {
    return (
      <div className={styles.clientChannelEmpty}>
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无终端访问数据" />
      </div>
    );
  }

  return (
    <div className={styles.clientChannelWrap}>
      <div className={styles.clientChannelList}>
        {data.map((item) => {
          const isHovered = hoverKey === item.key;
          return (
            <div
              key={item.key}
              className={`${styles.clientChannelItem} ${isHovered ? styles.clientChannelActive : ""}`}
              onMouseEnter={() => setHoverKey(item.key)}
              onMouseLeave={() => setHoverKey(null)}
            >
              <div className={styles.clientChannelHeader}>
                <div className={styles.clientChannelTitleCol}>
                  <span
                    className={styles.clientChannelIconWrap}
                    style={{ color: item.color, background: `${item.color}15` }}
                  >
                    {item.icon}
                  </span>
                  <div className={styles.clientChannelMeta}>
                    <span className={styles.clientChannelName}>{item.label}</span>
                    {item.desc && (
                      <span className={styles.clientChannelDesc}>{item.desc}</span>
                    )}
                  </div>
                </div>

                <div className={styles.clientChannelStats}>
                  <span className={styles.clientChannelCount}>
                    {item.count.toLocaleString()} <span className={styles.unitText}>{unit}</span>
                  </span>
                  <span
                    className={styles.clientChannelBadge}
                    style={{
                      color: item.color,
                      borderColor: `${item.color}40`,
                      background: `${item.color}10`,
                    }}
                  >
                    {item.pct}%
                  </span>
                </div>
              </div>

              {/* 进度条 */}
              <div className={styles.clientProgressBarWrap}>
                <div
                  className={styles.clientProgressBarFill}
                  style={{
                    width: `${Math.max(item.count > 0 ? 2 : 0, item.pct)}%`,
                    background: item.color,
                  }}
                />
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
