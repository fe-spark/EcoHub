"use client";

import React from "react";
import { Button, Popconfirm, Space, Spin, Tooltip } from "antd";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  CompassOutlined,
  DeleteOutlined,
  LoadingOutlined,
  StarFilled,
} from "@ant-design/icons";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { BannerRecord } from "./types";
import styles from "./banner-quick.module.less";

interface BannerQuickCardProps {
  item: BannerRecord;
  index: number;
  total: number;
  isScrapingThis: boolean;
  isAnyBusy: boolean;
  onMove: (index: number, direction: "up" | "down") => void;
  onRemove: (index: number) => void;
  onScrape?: (mid: number) => void;
}

export default function BannerQuickCard({
  item,
  index,
  total,
  isScrapingThis,
  isAnyBusy,
  onMove,
  onRemove,
  onScrape,
}: BannerQuickCardProps) {
  const posterUrl = item.picture || item.poster || item.customPicture || FALLBACK_IMG;

  const metaParts = [
    item.cName,
    item.year ? String(item.year) : null,
  ].filter(Boolean);

  return (
    <div
      className={`${styles.previewDeckCard} ${
        isScrapingThis ? styles.previewDeckCardScraping : ""
      }`}
    >
      {/* 主体信息：左侧海报封面 + 右侧详情 */}
      <div className={styles.cardBody}>
        <div className={styles.posterBox}>
          <img
            src={posterUrl}
            alt={item.name}
            className={styles.posterImg}
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).src = FALLBACK_IMG;
            }}
          />
          {item.score && item.score > 0 ? (
            <span className={styles.scoreBadge}>
              <StarFilled style={{ color: "#faad14" }} />
              {item.score.toFixed(1)}
            </span>
          ) : null}
        </div>

        <div className={styles.cardMainInfo}>
          <div>
            <div className={styles.cardHeadRow}>
              <span className={styles.orderNum}>
                {String(index + 1).padStart(2, "0")}
              </span>
              <div className={styles.cardTitle} title={item.name}>
                {item.name}
              </div>
            </div>

            <div className={styles.metaLine}>
              {metaParts.length > 0 ? metaParts.join(" · ") : "未分类"}
            </div>

            <div className={styles.cardBlurb} title={item.blurb}>
              {item.blurb || "暂无简介"}
            </div>
          </div>
        </div>

        {isScrapingThis && (
          <div className={styles.posterLoadingMask}>
            <Spin
              indicator={
                <LoadingOutlined style={{ fontSize: 24, color: "#ffffff" }} spin />
              }
            />
            <span className={styles.posterLoadingText}>刮削中</span>
          </div>
        )}
      </div>

      {/* 底部操作条 */}
      <div className={styles.cardFooter}>
        {onScrape ? (
          <Button
            size="small"
            icon={<CompassOutlined />}
            style={{ color: "#722ed1", borderColor: "#722ed1" }}
            disabled={isAnyBusy}
            onClick={() => onScrape(item.mid)}
          >
            TMDB 刮削
          </Button>
        ) : (
          <div />
        )}

        <Space size={6}>
          <Tooltip title="上移">
            <Button
              size="small"
              type="text"
              icon={<ArrowUpOutlined style={{ fontSize: 11 }} />}
              disabled={isAnyBusy || index === 0}
              onClick={() => onMove(index, "up")}
            />
          </Tooltip>

          <Tooltip title="下移">
            <Button
              size="small"
              type="text"
              icon={<ArrowDownOutlined style={{ fontSize: 11 }} />}
              disabled={isAnyBusy || index === total - 1}
              onClick={() => onMove(index, "down")}
            />
          </Tooltip>

          <Popconfirm
            title="确认移除？"
            onConfirm={() => onRemove(index)}
            okText="确定"
            cancelText="取消"
          >
            <Tooltip title="移除">
              <Button
                type="text"
                danger
                size="small"
                icon={<DeleteOutlined style={{ fontSize: 12 }} />}
                disabled={isAnyBusy}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      </div>
    </div>
  );
}
