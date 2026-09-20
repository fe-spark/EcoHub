"use client";

import React from "react";
import { Tooltip } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import styles from "./PlayHeaderCard.module.less";

interface PlayHeaderCardProps {
  name: string;
  remarks?: string;
  filmScore: string;
  localUpdateTime?: string;
  updateReason?: string;
  cName?: string;
  year?: string | number;
  area?: string;
  actorText: string;
}

export default function PlayHeaderCard({
  name,
  remarks,
  filmScore,
  localUpdateTime,
  updateReason,
  cName,
  year,
  area,
  actorText,
}: PlayHeaderCardProps) {
  return (
    <div className={styles.topInfoCard}>
      <div className={styles.titleRow}>
        <div className={styles.titleMain}>
          <h1 className={styles.filmTitle}>{name}</h1>
          {remarks ? <span className={styles.statusTag}>{remarks}</span> : null}
        </div>
        <div className={styles.scoreBadge} aria-label={`综合评分 ${filmScore} 分`}>
          <span className={styles.scoreCaption}>综合评分</span>
          <span className={styles.scoreNum}>
            {filmScore}
            <span className={styles.scoreUnit}>分</span>
          </span>
        </div>
      </div>

      <div className={styles.metaChips}>
        {localUpdateTime && (
          <span className={styles.metaChip}>
            {`${localUpdateTime} 更新`}
            {updateReason && (
              <Tooltip title={updateReason} placement="top">
                <InfoCircleOutlined className={styles.infoIcon} />
              </Tooltip>
            )}
          </span>
        )}
        {cName && <span className={styles.metaChip}>{cName}</span>}
        {year && <span className={styles.metaChip}>{year}</span>}
        {area && <span className={styles.metaChip}>{area}</span>}
      </div>

      <div className={styles.actorRow}>
        <span className={styles.actorLabel}>主演</span>
        <span className={styles.actorValue}>{actorText}</span>
      </div>
    </div>
  );
}
