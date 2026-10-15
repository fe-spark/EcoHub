"use client";

import React from "react";
import { Button, Image, Popconfirm, Space, Spin, Tag } from "antd";
import {
  ArrowDownOutlined,
  ArrowUpOutlined,
  DeleteOutlined,
  LoadingOutlined,
  StarFilled,
  ThunderboltOutlined,
} from "@ant-design/icons";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { BannerRecord } from "./types";
import styles from "./index.module.less";

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

function resolveCardScrapeTag(item: BannerRecord, isScraping: boolean) {
  if (isScraping) {
    return {
      text: "刮削中",
      color: "purple",
      icon: <LoadingOutlined spin style={{ fontSize: 11 }} />,
    };
  }
  const state =
    item.scrapeState ||
    (item.pendingScrape ? "pending" : item.isScraped ? "scraped" : "none");
  switch (state) {
    case "pending":
    case "scraped":
      return { text: "已刮削", color: "purple" };
    case "miss":
      return { text: "未匹配", color: "warning" };
    default:
      return { text: "未刮削", color: "default" };
  }
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
  const scrapeTag = resolveCardScrapeTag(item, isScrapingThis);
  const hasRealSlide = Boolean(
    item.pictureSlide &&
      item.pictureSlide.trim() !== "" &&
      item.pictureSlide !== item.picture &&
      item.pictureSlide !== item.poster,
  );

  return (
    <div
      className={`${styles.previewDeckCard} ${
        isScrapingThis ? styles.previewDeckCardScraping : ""
      }`}
    >
      <div className={styles.previewDeckCardIndex}>
        #{String(index + 1).padStart(2, "0")}
      </div>
      <div className={styles.previewDeckCardPoster}>
        <Image
          src={posterUrl}
          alt={item.name}
          fallback={FALLBACK_IMG}
          preview={isScrapingThis ? false : { mask: "预览" }}
          wrapperStyle={{ width: "100%", height: "100%" }}
          style={{ width: "100%", height: "100%", objectFit: "cover" }}
        />
        {isScrapingThis && (
          <div className={styles.posterLoadingMask}>
            <Spin
              indicator={
                <LoadingOutlined style={{ fontSize: 20, color: "#ffffff" }} spin />
              }
            />
            <span className={styles.posterLoadingText}>刮削中</span>
          </div>
        )}
      </div>
      <div className={styles.previewDeckCardInfo}>
        <div>
          <div
            style={{
              display: "flex",
              justifyContent: "space-between",
              alignItems: "flex-start",
              gap: 6,
            }}
          >
            <span className={styles.previewDeckCardTitle} title={item.name}>
              {item.name}
            </span>
            <Space size={2} align="center">
              {onScrape && (
                <Button
                  type="text"
                  size="small"
                  icon={
                    <ThunderboltOutlined
                      style={{
                        fontSize: 11,
                        color: isAnyBusy ? undefined : "#722ed1",
                      }}
                    />
                  }
                  disabled={isAnyBusy}
                  onClick={() => onScrape(item.mid)}
                  title="刮削"
                  style={{ width: 22, height: 22, padding: 0 }}
                />
              )}
              <Button
                type="text"
                size="small"
                icon={<ArrowUpOutlined style={{ fontSize: 10 }} />}
                disabled={isAnyBusy || index === 0}
                onClick={() => onMove(index, "up")}
                title="上移"
                style={{ width: 22, height: 22, padding: 0 }}
              />
              <Button
                type="text"
                size="small"
                icon={<ArrowDownOutlined style={{ fontSize: 10 }} />}
                disabled={isAnyBusy || index === total - 1}
                onClick={() => onMove(index, "down")}
                title="下移"
                style={{ width: 22, height: 22, padding: 0 }}
              />
              <Popconfirm
                title="确认移除？"
                onConfirm={() => onRemove(index)}
                okText="确定"
                cancelText="取消"
              >
                <Button
                  type="text"
                  danger
                  size="small"
                  icon={<DeleteOutlined style={{ fontSize: 11 }} />}
                  title="移除"
                  disabled={isAnyBusy}
                  style={{ width: 22, height: 22, padding: 0 }}
                />
              </Popconfirm>
            </Space>
          </div>
          <div className={styles.previewDeckCardTags}>
            {item.cName && (
              <Tag color="orange" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                {item.cName}
              </Tag>
            )}
            {item.year ? (
              <Tag color="blue" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                {item.year}
              </Tag>
            ) : null}
            {item.score && item.score > 0 ? (
              <Tag
                color="gold"
                icon={<StarFilled />}
                style={{ margin: 0, borderRadius: 4, fontSize: 11 }}
              >
                {item.score.toFixed(1)}
              </Tag>
            ) : null}
            <Tag
              color={scrapeTag.color}
              icon={scrapeTag.icon}
              style={{
                margin: 0,
                borderRadius: 4,
                fontSize: 11,
                cursor: onScrape && !isAnyBusy ? "pointer" : "default",
              }}
              onClick={onScrape && !isAnyBusy ? () => onScrape(item.mid) : undefined}
              title={onScrape && !isAnyBusy ? "刮削" : undefined}
            >
              {scrapeTag.text}
            </Tag>
          </div>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "flex-end", marginTop: 4 }}>
          <div className={styles.previewDeckCardBlurb} title={item.blurb}>
            {item.blurb || "暂无简介"}
          </div>
          {hasRealSlide && item.pictureSlide && (
            <div className={styles.previewDeckCardSlideThumb} title="横图">
              <Image
                src={item.pictureSlide}
                alt="横图"
                fallback={FALLBACK_IMG}
                preview={{ mask: "预览" }}
                wrapperStyle={{ width: "100%", height: "100%" }}
                style={{ width: "100%", height: "100%", objectFit: "cover" }}
              />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
