"use client";

import { useState } from "react";
import Link from "next/link";
import { Button, Card, Col, Empty, Row, Segmented, Skeleton, Space, Tag, Typography } from "antd";
import {
  AppstoreOutlined,
  CompassOutlined,
  FireOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import AllRankingsModal from "./AllRankingsModal";
import SourceCallsCard from "./SourceCallsCard";
import { isSafeInternalHref } from "../constants";
import type { AnalyticsModule, AppPlatform, SourceCallRow, TopItem } from "../types";
import styles from "../index.module.less";

const CARD_BODY_FLEX_STYLE = { flex: 1, display: "flex", flexDirection: "column" as const };

interface TopRankingsSectionProps {
  sources: SourceCallRow[];
  playTops: TopItem[];
  searchTops: TopItem[];
  classifyTops: TopItem[];
  pageTops: TopItem[];
  dayStr: string;
  module: AnalyticsModule;
  platform: AppPlatform;
  loading?: boolean;
}

type RankKind = "play" | "search" | "classify" | "page";

export default function TopRankingsSection({
  sources,
  playTops,
  searchTops,
  classifyTops,
  pageTops,
  dayStr,
  module,
  platform,
  loading,
}: TopRankingsSectionProps) {
  const [modalOpen, setModalOpen] = useState(false);
  const [modalKind, setModalKind] = useState<RankKind>("play");
  const [secondaryKind, setSecondaryKind] = useState<"classify" | "page">("classify");

  const openModal = (kind: RankKind) => {
    setModalKind(kind);
    setModalOpen(true);
  };

  // TVBox 无独立页面排行，自动固定展示高频分类
  const activeSecondaryKind: "classify" | "page" =
    module === "tvbox" ? "classify" : secondaryKind;

  const renderRankBody = (kind: RankKind, items: TopItem[], fillColor: string) => {
    const maxCount = items[0]?.count || 1;
    if (loading && items.length === 0) {
      return <Skeleton active paragraph={{ rows: 5 }} />;
    }
    if (items.length === 0) {
      return (
        <div className={styles.emptyCenter} style={{ minHeight: 260 }}>
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无排行数据" />
        </div>
      );
    }

    return (
      <div className={styles.rankList}>
        {items.map((item, idx) => {
          const pct = Math.max(4, Math.round((item.count / maxCount) * 100));
          const rawId = item.key.replace(/^id\s+/, "").trim();
          const isNumericId = /^\d+$/.test(rawId);

          let labelNode: React.ReactNode = item.key;
          if (kind === "play") {
            const name = item.title || `影片 #${rawId}`;
            labelNode = (
              <Space size={6} style={{ minWidth: 0 }}>
                <Link href={`/play?id=${rawId}`} target="_blank" style={{ fontWeight: 500 }}>
                  {name}
                </Link>
                {item.category && (
                  <Tag color="blue" style={{ margin: 0 }}>
                    {item.category}
                  </Tag>
                )}
              </Space>
            );
          } else if (kind === "search") {
            labelNode = (
              <Link
                href={`/search?search=${encodeURIComponent(item.key)}`}
                target="_blank"
                style={{ fontWeight: 500 }}
              >
                {item.key}
              </Link>
            );
          } else if (kind === "classify") {
            const name =
              item.title || item.category || (isNumericId ? `分类 #${rawId}` : item.key);
            labelNode =
              item.href && isSafeInternalHref(item.href) ? (
                <Link href={item.href} target="_blank" style={{ fontWeight: 500 }}>
                  {name}
                </Link>
              ) : (
                <Typography.Text strong>{name}</Typography.Text>
              );
          } else if (isSafeInternalHref(item.key)) {
            labelNode = (
              <Link href={item.key} target="_blank" style={{ fontWeight: 500 }}>
                {item.key}
              </Link>
            );
          }

          return (
            <div key={`${kind}-${item.key}-${idx}`} className={styles.rankItem}>
              <div
                className={styles.rankFill}
                style={{ width: `${pct}%`, background: fillColor }}
              />
              <div className={styles.rankLeft}>
                <Tag
                  color={
                    idx === 0 ? "red" : idx === 1 ? "orange" : idx === 2 ? "gold" : "default"
                  }
                  style={{ margin: 0, minWidth: 24, textAlign: "center" }}
                >
                  {idx + 1}
                </Tag>
                <div className={styles.rankLabel}>{labelNode}</div>
              </div>
              <Typography.Text type="secondary" className={styles.rankCount}>
                {item.count.toLocaleString()} 次
              </Typography.Text>
            </div>
          );
        })}
      </div>
    );
  };

  return (
    <>
      {/* 严格 2×2 宽列栅格（lg=12），保证片名、分类标签和完整路径不拥挤截断 */}
      <Row gutter={[16, 16]}>
        <Col xs={24} lg={12}>
          <Card
            size="small"
            className={styles.fullHeightCard}
            styles={{ body: CARD_BODY_FLEX_STYLE }}
            title={
              <Space size={6}>
                <VideoCameraOutlined style={{ color: "#fa8c16" }} />
                <span>热门点播 TOP 10</span>
              </Space>
            }
            extra={
              <Button type="link" size="small" onClick={() => openModal("play")}>
                查看完整榜
              </Button>
            }
          >
            {renderRankBody("play", playTops, "#fa8c16")}
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <SourceCallsCard sources={sources} loading={loading} />
        </Col>

        <Col xs={24} lg={12}>
          <Card
            size="small"
            className={styles.fullHeightCard}
            styles={{ body: CARD_BODY_FLEX_STYLE }}
            title={
              <Space size={6}>
                <FireOutlined style={{ color: "#fa541c" }} />
                <span>热门搜索 TOP 10</span>
              </Space>
            }
            extra={
              <Button type="link" size="small" onClick={() => openModal("search")}>
                查看完整榜
              </Button>
            }
          >
            {renderRankBody("search", searchTops, "#fa541c")}
          </Card>
        </Col>

        <Col xs={24} lg={12}>
          <Card
            size="small"
            className={styles.fullHeightCard}
            styles={{ body: CARD_BODY_FLEX_STYLE }}
            title={
              <Space size={8} wrap>
                {activeSecondaryKind === "classify" ? (
                  <AppstoreOutlined style={{ color: "#52c41a" }} />
                ) : (
                  <CompassOutlined style={{ color: "#1677ff" }} />
                )}
                <span>
                  {activeSecondaryKind === "classify" ? "高频分类 TOP 10" : "热门页面 TOP 10"}
                </span>
                {module !== "tvbox" && (
                  <Segmented
                    size="small"
                    value={activeSecondaryKind}
                    options={[
                      { label: "高频分类", value: "classify" },
                      { label: "热门页面", value: "page" },
                    ]}
                    onChange={(val) => setSecondaryKind(val as "classify" | "page")}
                  />
                )}
              </Space>
            }
            extra={
              <Button
                type="link"
                size="small"
                onClick={() => openModal(activeSecondaryKind)}
              >
                查看完整榜
              </Button>
            }
          >
            {activeSecondaryKind === "classify"
              ? renderRankBody("classify", classifyTops, "#52c41a")
              : renderRankBody("page", pageTops, "#1677ff")}
          </Card>
        </Col>
      </Row>

      <AllRankingsModal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        kind={modalKind}
        dayStr={dayStr}
        module={module}
        platform={platform}
      />
    </>
  );
}
