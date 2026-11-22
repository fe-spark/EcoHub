"use client";

import React from "react";
import { Button } from "antd";
import {
  VideoCameraOutlined,
  ThunderboltOutlined,
  UserOutlined,
  CloudSyncOutlined,
  PlayCircleOutlined,
} from "@ant-design/icons";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import styles from "./index.module.less";

const GUIDE_CHIPS = [
  { icon: <UserOutlined className={styles.chipIcon} />, label: "1. 登录后台 (admin)" },
  { icon: <CloudSyncOutlined className={styles.chipIcon} />, label: "2. 绑定采集节点" },
  { icon: <PlayCircleOutlined className={styles.chipIcon} />, label: "3. 一键同步片库" },
];

export interface EmptyCollectGuideProps {
  badgeText?: string;
  title?: string;
  description?: string;
  compact?: boolean;
}

export default function EmptyCollectGuide({
  badgeText = "EcoHub · 片库准备就绪",
  title = "探索影视宇宙 · 从开启采集开始",
  description = "当前数据库中暂无上映影片与推荐内容。登录管理后台开启数据采集，即可实时同步海量影视片源。",
  compact = false,
}: EmptyCollectGuideProps) {
  const { navigate } = useContentNavigate();

  return (
    <section
      className={`${styles.container} ${compact ? styles.compact : ""}`}
      aria-label="片库待初始化"
    >
      <div className={styles.ambientGlow} aria-hidden />

      <div className={styles.content}>
        <div className={styles.badge}>
          <span className={styles.badgeDot} />
          <span>{badgeText}</span>
        </div>

        <div className={styles.iconRing}>
          <VideoCameraOutlined className={styles.heroIcon} />
        </div>

        <h2 className={styles.title}>{title}</h2>
        <p className={styles.description}>{description}</p>

        <div className={styles.stepChips}>
          {GUIDE_CHIPS.map((chip, idx) => (
            <div key={idx} className={styles.chip}>
              {chip.icon}
              <span>{chip.label}</span>
            </div>
          ))}
        </div>

        <div className={styles.actions}>
          <Button
            type="primary"
            icon={<ThunderboltOutlined />}
            size="large"
            className={styles.primaryBtn}
            onClick={() => {
              navigate("/manage/collect", "正在前往采集中心...");
            }}
          >
            前往采集中心
          </Button>
        </div>
      </div>
    </section>
  );
}
