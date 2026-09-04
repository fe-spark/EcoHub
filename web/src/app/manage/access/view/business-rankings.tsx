"use client";

import { useState } from "react";
import Link from "next/link";
import { Button, Card, Empty, Space, Tag } from "antd";
import { AppstoreOutlined, FireOutlined, VideoCameraOutlined } from "@ant-design/icons";
import type { TopItem } from "./types";
import AllRankingsModal from "./all-rankings-modal";
import styles from "./index.module.less";

interface BusinessRankingsProps {
  playTops: TopItem[];
  searchTops: TopItem[];
  classifyTops?: TopItem[];
  loading?: boolean;
  dayStr?: string;
  module?: "app" | "web" | "tvbox";
  platform?: string;
}

export default function BusinessRankings({
  playTops,
  searchTops,
  classifyTops = [],
  loading = false,
  dayStr,
  module = "app",
  platform,
}: BusinessRankingsProps) {
  const [modalOpen, setModalOpen] = useState(false);
  const [modalKind, setModalKind] = useState<"play" | "search" | "classify">("play");

  const maxPlayCount = playTops[0]?.count || 1;
  const maxSearchCount = searchTops[0]?.count || 1;
  const maxClassifyCount = classifyTops[0]?.count || 1;

  const handleOpenModal = (kind: "play" | "search" | "classify") => {
    setModalKind(kind);
    setModalOpen(true);
  };

  return (
    <div className={styles.rankingsGridRow}>
      {/* 热门点播 TOP 10 */}
      <Card
        className={styles.halfCard}
        title={
          <Space>
            <VideoCameraOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
            <span>热门点播 TOP 10</span>
          </Space>
        }
        extra={
          <Button
            type="link"
            size="small"
            onClick={() => handleOpenModal("play")}
          >
            查看全部
          </Button>
        }
        loading={loading}
      >
        {playTops.length === 0 ? (
          <Empty description="暂无点播数据" />
        ) : (
          <div className={styles.hotPlayList}>
            {playTops.map((item, idx) => {
              const rankClass =
                idx === 0
                  ? styles.rank1
                  : idx === 1
                  ? styles.rank2
                  : idx === 2
                  ? styles.rank3
                  : "";
              const rawFilmId = item.key.replace(/^id\s+/, "");
              const displayName = item.title || `影片 #${rawFilmId}`;
              const barWidthPct = Math.max(4, Math.round((item.count / maxPlayCount) * 100));

              return (
                <div className={styles.hotPlayItem} key={`${item.key}-${idx}`}>
                  <div className={`${styles.rankBadge} ${rankClass}`}>{idx + 1}</div>
                  {item.poster ? (
                    // eslint-disable-next-line @next/next/no-img-element
                    <img
                      src={item.poster}
                      alt={displayName}
                      className={styles.posterThumb}
                      loading="lazy"
                    />
                  ) : (
                    <div className={styles.posterPlaceholder}>
                      <VideoCameraOutlined />
                    </div>
                  )}
                  <div className={styles.filmInfo}>
                    <div className={styles.filmTitleRow}>
                      <Link
                        href={`/play?id=${rawFilmId}`}
                        target="_blank"
                        className={styles.filmTitle}
                      >
                        {displayName}
                      </Link>
                      {item.category ? (
                        <Tag color="blue" className={styles.filmCategoryTag}>
                          {item.category}
                        </Tag>
                      ) : null}
                      {item.year ? (
                        <span className={styles.hint}>({item.year})</span>
                      ) : null}
                    </div>
                    <div className={styles.filmCountBarWrap}>
                      <div className={styles.filmCountBar}>
                        <div
                          className={styles.filmCountBarFill}
                          style={{ width: `${barWidthPct}%` }}
                        />
                      </div>
                      <span className={styles.filmCount}>
                        {item.count.toLocaleString()} 次
                      </span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      {/* 热门搜索 TOP 10 */}
      <Card
        className={styles.halfCard}
        title={
          <Space>
            <FireOutlined style={{ color: "#fa541c" }} />
            <span>热门搜索 TOP 10</span>
          </Space>
        }
        extra={
          <Button
            type="link"
            size="small"
            onClick={() => handleOpenModal("search")}
          >
            查看全部
          </Button>
        }
        loading={loading}
      >
        {searchTops.length === 0 ? (
          <Empty description="暂无搜索记录" />
        ) : (
          <div className={styles.hotPlayList}>
            {searchTops.map((item, idx) => {
              const rankClass =
                idx === 0
                  ? styles.rank1
                  : idx === 1
                  ? styles.rank2
                  : idx === 2
                  ? styles.rank3
                  : "";
              const barWidthPct = Math.max(4, Math.round((item.count / maxSearchCount) * 100));

              return (
                <div className={styles.hotPlayItem} key={`${item.key}-${idx}`}>
                  <div className={`${styles.rankBadge} ${rankClass}`}>{idx + 1}</div>
                  <div className={styles.filmInfo}>
                    <div className={styles.filmTitleRow}>
                      <Link
                        href={`/search?search=${encodeURIComponent(item.key)}`}
                        target="_blank"
                        className={styles.filmTitle}
                      >
                        {item.key}
                      </Link>
                    </div>
                    <div className={styles.filmCountBarWrap}>
                      <div className={styles.filmCountBar}>
                        <div
                          className={styles.filmCountBarFill}
                          style={{
                            width: `${barWidthPct}%`,
                            background: "linear-gradient(90deg, #fa541c, #ff7a45)",
                          }}
                        />
                      </div>
                      <span className={styles.filmCount} style={{ color: "#fa541c" }}>
                        {item.count.toLocaleString()} 次
                      </span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      <Card
        className={styles.halfCard}
        title={
          <Space>
            <AppstoreOutlined style={{ color: "var(--ant-color-success, #52c41a)" }} />
            <span>高频分类 TOP 10</span>
          </Space>
        }
        extra={
          <Button type="link" size="small" onClick={() => handleOpenModal("classify")}>
            查看全部
          </Button>
        }
        loading={loading}
      >
        {classifyTops.length === 0 ? (
          <Empty description="暂无分类记录" />
        ) : (
          <div className={styles.hotPlayList}>
            {classifyTops.map((item, idx) => {
              const rankClass =
                idx === 0 ? styles.rank1 : idx === 1 ? styles.rank2 : idx === 2 ? styles.rank3 : "";
              const barWidthPct = Math.max(4, Math.round((item.count / maxClassifyCount) * 100));
              const rawId = item.key.replace(/^id\s+/, "").trim();
              const isNumericId = /^\d+$/.test(rawId);
              const displayName = item.title || item.category || (isNumericId ? `分类 #${rawId}` : item.key);
              const showIdTag =
                isNumericId &&
                displayName !== `#${rawId}` &&
                displayName !== `分类 #${rawId}` &&
                displayName !== rawId;
              return (
                <div className={styles.hotPlayItem} key={`${item.key}-${idx}`}>
                  <div className={`${styles.rankBadge} ${rankClass}`}>{idx + 1}</div>
                  <div className={styles.filmInfo}>
                    <div className={styles.filmTitleRow}>
                      <span className={styles.filmTitle}>{displayName}</span>
                      {showIdTag && (
                        <Tag color="purple" style={{ fontSize: 11, lineHeight: "18px", padding: "0 6px" }}>
                          #{rawId}
                        </Tag>
                      )}
                    </div>
                    <div className={styles.filmCountBarWrap}>
                      <div className={styles.filmCountBar}>
                        <div
                          className={styles.filmCountBarFill}
                          style={{
                            width: `${barWidthPct}%`,
                            background: "linear-gradient(90deg, #52c41a, #73d13d)",
                          }}
                        />
                      </div>
                      <span className={styles.filmCount} style={{ color: "var(--ant-color-success, #52c41a)" }}>
                        {item.count.toLocaleString()} 次
                      </span>
                    </div>
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      <AllRankingsModal
        open={modalOpen}
        onClose={() => setModalOpen(false)}
        kind={modalKind}
        dayStr={dayStr || new Date().toISOString().slice(0, 10)}
        module={module}
        platform={platform}
      />
    </div>
  );
}
