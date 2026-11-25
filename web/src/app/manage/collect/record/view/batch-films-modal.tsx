"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import Link from "next/link";
import { Alert, Modal, Input, Button, Spin, Space, Empty, Tag } from "antd";
import {
  VideoCameraOutlined,
  SearchOutlined,
  ExportOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import styles from "./batch-films-modal.module.less";

export interface BatchFilmsModalProps {
  open: boolean;
  batchId: string;
  sourceId: string;
  sourceName: string;
  onClose: () => void;
}

interface FilmItem {
  id: number;
  mid: number;
  name: string;
  sourceId?: string;
}

interface FilmCursor {
  id: number;
  name: string;
  mid: number;
}

const firstFilmCursor: FilmCursor = { id: 0, name: "", mid: 0 };

export function BatchFilmsModal({
  open,
  batchId,
  sourceId,
  sourceName,
  onClose,
}: BatchFilmsModalProps) {
  const [keyword, setKeyword] = useState("");
  const [pageSize] = useState(20);
  const [list, setList] = useState<FilmItem[]>([]);
  const [total, setTotal] = useState(0);
  const [hasMore, setHasMore] = useState(false);
  const [nextCursor, setNextCursor] = useState<FilmCursor>(firstFilmCursor);
  const [cursorStack, setCursorStack] = useState<FilmCursor[]>([firstFilmCursor]);
  const [errorText, setErrorText] = useState("");
  const [loading, setLoading] = useState(false);
  const [hasLoaded, setHasLoaded] = useState(false);
  const appliedKeyword = useRef("");
  const requestSeq = useRef(0);

  const fetchFilms = useCallback(
    async (cursor: FilmCursor, pKeyword: string) => {
      const seq = ++requestSeq.current;
      if (!batchId) {
        setHasLoaded(true);
        return;
      }
      setLoading(true);
      setErrorText("");
      try {
        const resp = await ApiGet<{
          list: FilmItem[];
          total: number;
          nextAfterId: number;
          nextAfterName?: string;
          nextAfterMid?: number;
          hasMore: boolean;
        }>("/manage/collect/history/films", {
          batchId,
          sourceId,
          afterId: cursor.id,
          afterName: cursor.name,
          afterMid: cursor.mid,
          pageSize,
          keyword: pKeyword.trim(),
        });
        if (seq !== requestSeq.current) return;
        if (resp.code === 0 && resp.data) {
          const rows = resp.data.list || [];
          setList(rows);
          setTotal(resp.data.total || 0);
          setHasMore(Boolean(resp.data.hasMore));
          setNextCursor(
            resp.data.hasMore
              ? {
                  id: resp.data.nextAfterId || 0,
                  name: resp.data.nextAfterName || "",
                  mid: resp.data.nextAfterMid || 0,
                }
              : firstFilmCursor,
          );
          return;
        }
        setList([]);
        setTotal(0);
        setErrorText(resp.msg || "加载失败");
      } catch (err) {
        if (seq !== requestSeq.current) return;
        setList([]);
        setTotal(0);
        setErrorText(err instanceof Error && err.message ? err.message : "加载失败");
      } finally {
        if (seq === requestSeq.current) {
          setLoading(false);
          setHasLoaded(true);
        }
      }
    },
    [batchId, sourceId, pageSize],
  );

  useEffect(() => {
    if (open) {
      appliedKeyword.current = "";
      setKeyword("");
      setCursorStack([firstFilmCursor]);
      setErrorText("");
      setHasLoaded(false);
      void fetchFilms(firstFilmCursor, "");
    } else {
      requestSeq.current++;
      setList([]);
      setTotal(0);
      setHasMore(false);
      setNextCursor(firstFilmCursor);
      setLoading(false);
      setErrorText("");
      setHasLoaded(false);
    }
  }, [open, fetchFilms]);

  const handleExecuteSearch = () => {
    const next = keyword.trim();
    appliedKeyword.current = next;
    setCursorStack([firstFilmCursor]);
    void fetchFilms(firstFilmCursor, next);
  };

  const handleResetSearch = () => {
    appliedKeyword.current = "";
    setKeyword("");
    setCursorStack([firstFilmCursor]);
    void fetchFilms(firstFilmCursor, "");
  };

  const handleNext = () => {
    if (!hasMore || nextCursor.id <= 0) return;
    const cursor = nextCursor;
    setCursorStack((stack) => [...stack, cursor]);
    void fetchFilms(cursor, appliedKeyword.current);
  };

  const handlePrev = () => {
    setCursorStack((stack) => {
      if (stack.length <= 1) return stack;
      const next = stack.slice(0, -1);
      void fetchFilms(next[next.length - 1], appliedKeyword.current);
      return next;
    });
  };

  const sourceLabel = sourceName || sourceId || "全部站点";
  const filmManageUrl = sourceId ? `/manage/film?sourceId=${encodeURIComponent(sourceId)}` : "/manage/film";
  const isInitialSearching = !hasLoaded && loading;
  const isBatchZeroUpdate = total === 0 && hasLoaded && !errorText && !appliedKeyword.current;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={680}
      destroyOnHidden
      title={
        <div className={styles.modalHeader}>
          <span className={styles.headerTitle}>
            <VideoCameraOutlined className={styles.headerIcon} />
            <span>更新影片</span>
          </span>
          <Tag variant="filled">{sourceLabel}</Tag>
          {!isInitialSearching ? (
            <span className={styles.headerCount}>共 {total.toLocaleString("zh-CN")} 部</span>
          ) : null}
        </div>
      }
    >
      <div className={styles.modalBody}>
        {errorText && list.length > 0 ? (
          <Alert type="error" showIcon title={errorText} />
        ) : null}

        <div className={styles.toolbar}>
          <Space size={8} className={styles.filterGroup}>
            <Input
              placeholder="输入片名开头"
              allowClear
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onPressEnter={handleExecuteSearch}
              className={styles.searchInput}
              disabled={loading || isBatchZeroUpdate}
            />
            <Button
              type="primary"
              icon={<SearchOutlined />}
              onClick={handleExecuteSearch}
              loading={loading}
              disabled={isBatchZeroUpdate}
            >
              搜索
            </Button>
            {(keyword || appliedKeyword.current) ? (
              <Button icon={<ReloadOutlined />} onClick={handleResetSearch} disabled={loading}>
                重置
              </Button>
            ) : null}
          </Space>
          <Button icon={<ExportOutlined />} href={filmManageUrl} target="_blank">
            影片库
          </Button>
        </div>

        <Spin spinning={loading}>
          {errorText && list.length === 0 && !loading ? (
            <div className={styles.emptyBox}>
              <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description={errorText}>
                <Button
                  type="primary"
                  size="small"
                  icon={<ReloadOutlined />}
                  onClick={() =>
                    void fetchFilms(cursorStack[cursorStack.length - 1] ?? firstFilmCursor, appliedKeyword.current)
                  }
                >
                  重试
                </Button>
              </Empty>
            </div>
          ) : list.length === 0 && !loading ? (
            <div className={styles.emptyBox}>
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description={appliedKeyword.current ? "无匹配影片" : "暂无入库影片"}
              >
                {appliedKeyword.current ? (
                  <Button size="small" icon={<ReloadOutlined />} onClick={handleResetSearch}>
                    清除搜索
                  </Button>
                ) : null}
              </Empty>
            </div>
          ) : list.length === 0 && loading ? (
            <div className={styles.loadingPlaceholder} />
          ) : (
            <div className={styles.listContainer}>
              {list.map((film, index) => {
                const playUrl = resolvePlayEntryPath(film.mid, { sourceId: film.sourceId || sourceId });
                const badgeText = film.mid ? `#${film.mid}` : `#${index + 1}`;
                return (
                  <div key={`${film.sourceId ?? sourceId}-${film.mid}`} className={styles.listRow}>
                    <span className={styles.midBadge}>{badgeText}</span>
                    <Link href={playUrl} target="_blank" className={styles.filmName} title={film.name}>
                      {film.name}
                    </Link>
                    <Button
                      type="link"
                      size="small"
                      icon={<PlayCircleOutlined />}
                      href={playUrl}
                      target="_blank"
                      className={styles.playBtn}
                    >
                      播放
                    </Button>
                  </div>
                );
              })}
            </div>
          )}
        </Spin>

        {cursorStack.length > 1 || hasMore ? (
          <div className={styles.paginationWrapper}>
            <span className={styles.paginationTotal}>
              第 {cursorStack.length} 页 · 共 {total.toLocaleString("zh-CN")} 部
            </span>
            <Space>
              <Button size="small" disabled={cursorStack.length <= 1 || loading} onClick={handlePrev}>
                上一页
              </Button>
              <Button size="small" disabled={!hasMore || loading} onClick={handleNext}>
                下一页
              </Button>
            </Space>
          </div>
        ) : null}
      </div>
    </Modal>
  );
}
