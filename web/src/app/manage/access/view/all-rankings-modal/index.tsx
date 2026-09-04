"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import { Button, Empty, Input, Modal, Pagination, Space, Spin, Tag } from "antd";
import {
  AppstoreOutlined,
  FireOutlined,
  ReloadOutlined,
  SearchOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import type { TopItem } from "../types";
import styles from "./index.module.less";

interface AllRankingsModalProps {
  open: boolean;
  onClose: () => void;
  kind: "play" | "search" | "classify";
  dayStr: string;
  module?: "app" | "web" | "tvbox";
  platform?: string;
}

const DEFAULT_PAGE_SIZE = 20;

export default function AllRankingsModal({
  open,
  onClose,
  kind,
  dayStr,
  module = "app",
  platform,
}: AllRankingsModalProps) {
  const [items, setItems] = useState<TopItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [keyword, setKeyword] = useState("");
  const [currentPage, setCurrentPage] = useState(1);
  const [pageSize, setPageSize] = useState(DEFAULT_PAGE_SIZE);
  const listRef = useRef<HTMLDivElement>(null);
  // 竞态防护：同一次打开期间只保留最新一批请求的响应，防止慢响应覆盖切换 kind/日期后的榜单
  const reqSeqRef = useRef(0);
  const inFlightRef = useRef(false);

  const fetchAllData = useCallback(async () => {
    if (!open || inFlightRef.current) return;
    inFlightRef.current = true;
    const seq = ++reqSeqRef.current;
    setLoading(true);
    try {
      const pParam = platform && platform !== "all" ? `&platform=${encodeURIComponent(platform)}` : "";
      const mParam = module ? `&module=${encodeURIComponent(module)}` : "";
      const res = await ApiGet<{ items: TopItem[] }>(
        `/manage/access/tops?day=${dayStr}${mParam}&kind=${kind}${pParam}&limit=2000`
      );
      if (seq !== reqSeqRef.current) {
        return;
      }
      if (res.code === 0 && res.data?.items) {
        setItems(res.data.items);
      } else {
        setItems([]);
      }
    } catch {
      if (seq === reqSeqRef.current) {
        setItems([]);
      }
    } finally {
      if (seq === reqSeqRef.current) {
        inFlightRef.current = false;
        setLoading(false);
      }
    }
  }, [open, dayStr, module, kind, platform]);

  useEffect(() => {
    if (open) {
      setCurrentPage(1);
      setKeyword("");
      void fetchAllData();
    }
  }, [open, fetchAllData]);

  // 根据关键字过滤
  const filteredItems = useMemo(() => {
    if (!keyword.trim()) return items;
    const q = keyword.trim().toLowerCase();
    return items.filter((it) => {
      const keyMatch = it.key.toLowerCase().includes(q);
      const titleMatch = it.title ? it.title.toLowerCase().includes(q) : false;
      const catMatch = it.category ? it.category.toLowerCase().includes(q) : false;
      return keyMatch || titleMatch || catMatch;
    });
  }, [items, keyword]);

  // 统计数据
  const totalCount = useMemo(
    () => items.reduce((acc, cur) => acc + (cur.count || 0), 0),
    [items]
  );
  const maxCount = useMemo(() => items[0]?.count || 1, [items]);

  // 当前分页数据
  const pagedItems = useMemo(() => {
    const start = (currentPage - 1) * pageSize;
    return filteredItems.slice(start, start + pageSize);
  }, [filteredItems, currentPage, pageSize]);

  const isPlay = kind === "play";
  const isClassify = kind === "classify";
  const titleText = isPlay
    ? "当日热门点播全部记录"
    : isClassify
      ? "当日高频分类全部记录"
      : "当日热门搜索全部记录";

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={760}
      destroyOnClose
      title={
        <div className={styles.headerTitle}>
          {isPlay ? (
            <VideoCameraOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
          ) : isClassify ? (
            <AppstoreOutlined style={{ color: "var(--ant-color-success, #52c41a)" }} />
          ) : (
            <FireOutlined style={{ color: "#fa541c" }} />
          )}
          <span>{titleText}</span>
          <Tag color="default">{dayStr}</Tag>
        </div>
      }
    >
      <div className={styles.filterRow}>
        <Input
          placeholder={
            isPlay
              ? "搜索片名、分类或 ID..."
              : isClassify
                ? "搜索分类名称或 ID..."
                : "搜索关键词..."
          }
          prefix={<SearchOutlined style={{ color: "var(--ant-color-text-tertiary)" }} />}
          allowClear
          value={keyword}
          onChange={(e) => {
            setKeyword(e.target.value);
            setCurrentPage(1);
          }}
          className={styles.searchBox}
        />
        <div className={styles.toolbarActions}>
          <Space size="middle">
            <span className={styles.statsText}>
              记录总数:
              <strong className={styles.statHighlight}>{items.length}</strong> 条
            </span>
            <span className={styles.statsText}>
              总频次:
              <strong className={styles.statHighlight}>{totalCount.toLocaleString()}</strong> 次
            </span>
          </Space>
          <Button
            icon={<ReloadOutlined spin={loading} />}
            onClick={() => void fetchAllData()}
            disabled={loading}
          >
            刷新
          </Button>
        </div>
      </div>

      <Spin spinning={loading}>
        {filteredItems.length === 0 ? (
          <Empty description={keyword ? "未匹配到相关记录" : "当日暂无记录"} />
        ) : (
          <>
            <div className={styles.listWrapper} ref={listRef}>
              {pagedItems.map((item) => {
                // 计算在整体 items 中的原绝对排名
                const absoluteIndex = items.findIndex((orig) => orig.key === item.key);
                const rankNumber = absoluteIndex >= 0 ? absoluteIndex + 1 : 1;
                const rankClass =
                  rankNumber === 1
                    ? styles.rank1
                    : rankNumber === 2
                    ? styles.rank2
                    : rankNumber === 3
                    ? styles.rank3
                    : "";
                const barWidthPct = Math.max(3, Math.round((item.count / maxCount) * 100));

                if (isPlay) {
                  const rawFilmId = item.key.replace(/^id\s+/, "");
                  const displayName = item.title || `影片 #${rawFilmId}`;

                  return (
                    <div className={styles.listItem} key={item.key}>
                      <div className={`${styles.rankBadge} ${rankClass}`}>{rankNumber}</div>
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
                }

                if (isClassify) {
                  const rawId = item.key.replace(/^id\s+/, "").trim();
                  const isNumericId = /^\d+$/.test(rawId);
                  const displayName = item.title || item.category || (isNumericId ? `分类 #${rawId}` : item.key);
                  const classifyUrl = isNumericId ? `/filmClassify?Pid=${encodeURIComponent(rawId)}` : undefined;
                  const showIdTag =
                    isNumericId &&
                    displayName !== `#${rawId}` &&
                    displayName !== `分类 #${rawId}` &&
                    displayName !== rawId;

                  return (
                    <div className={styles.listItem} key={item.key}>
                      <div className={`${styles.rankBadge} ${rankClass}`}>{rankNumber}</div>
                      <div className={styles.filmInfo}>
                        <div className={styles.filmTitleRow}>
                          {classifyUrl ? (
                            <Link
                              href={classifyUrl}
                              target="_blank"
                              className={styles.filmTitle}
                            >
                              {displayName}
                            </Link>
                          ) : (
                            <span className={styles.filmTitle}>{displayName}</span>
                          )}
                          {showIdTag && (
                            <Tag color="purple" className={styles.classifyIdTag}>
                              #{rawId}
                            </Tag>
                          )}
                        </div>
                        <div className={styles.filmCountBarWrap}>
                          <div className={styles.filmCountBar}>
                            <div
                              className={`${styles.filmCountBarFill} ${styles.classifyBarFill}`}
                              style={{ width: `${barWidthPct}%` }}
                            />
                          </div>
                          <span className={`${styles.filmCount} ${styles.classifyCount}`}>
                            {item.count.toLocaleString()} 次
                          </span>
                        </div>
                      </div>
                    </div>
                  );
                }

                // 热门搜索条目
                return (
                  <div className={styles.listItem} key={item.key}>
                    <div className={`${styles.rankBadge} ${rankClass}`}>{rankNumber}</div>
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
                            className={`${styles.filmCountBarFill} ${styles.searchBarFill}`}
                            style={{ width: `${barWidthPct}%` }}
                          />
                        </div>
                        <span className={`${styles.filmCount} ${styles.searchCount}`}>
                          {item.count.toLocaleString()} 次
                        </span>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>

            {filteredItems.length > 0 && (
              <div className={styles.paginationWrapper}>
                <Pagination
                  current={currentPage}
                  pageSize={pageSize}
                  total={filteredItems.length}
                  pageSizeOptions={[10, 20, 50, 100]}
                  showSizeChanger
                  showQuickJumper={filteredItems.length > pageSize * 3}
                  showTotal={(total, range) => `第 ${range[0]}-${range[1]} 条 / 共 ${total} 条`}
                  onChange={(page, newPageSize) => {
                    setCurrentPage(page);
                    if (newPageSize !== pageSize) {
                      setPageSize(newPageSize);
                    }
                    listRef.current?.scrollTo({ top: 0, behavior: "smooth" });
                  }}
                  size="small"
                />
              </div>
            )}
          </>
        )}
      </Spin>
    </Modal>
  );
}
