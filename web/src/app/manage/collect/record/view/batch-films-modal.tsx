"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import Link from "next/link";
import { Alert, Modal, Input, Button, Spin, Space } from "antd";
import {
  VideoCameraOutlined,
  SearchOutlined,
  ExportOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  InboxOutlined,
  CloseCircleOutlined,
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

  const fetchFilms = useCallback(
    async (cursor: FilmCursor, pKeyword: string) => {
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
        setErrorText(resp.msg || "获取影片清单失败");
      } catch (err) {
        setList([]);
        setTotal(0);
        setErrorText(err instanceof Error && err.message ? err.message : "获取影片清单失败");
      } finally {
        setLoading(false);
        setHasLoaded(true);
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
      setList([]);
      setTotal(0);
      setHasMore(false);
      setNextCursor(firstFilmCursor);
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

  const sourceDesc = sourceName || sourceId ? `来源站点：${sourceName || sourceId}` : "涵盖全部站点";
  const filmManageUrl = sourceId ? `/manage/film?sourceId=${encodeURIComponent(sourceId)}` : "/manage/film";

  const isInitialSearching = !hasLoaded && loading;
  const subtitleText = isInitialSearching
    ? `${sourceDesc} · 正在读取更新清单...`
    : `${sourceDesc} · 共 ${total.toLocaleString("zh-CN")} 部更新影片`;

  const isBatchZeroUpdate = total === 0 && hasLoaded && !appliedKeyword.current;

  return (
    <Modal
      open={open}
      onCancel={onClose}
      footer={null}
      width={740}
      destroyOnHidden
      title={
        <div className={styles.modalHeader}>
          <div className={styles.headerIconWrap}>
            <VideoCameraOutlined />
          </div>
          <div className={styles.headerTextGroup}>
            <div className={styles.headerTitleRow}>
              <span className={styles.headerTitle}>更新影片清单</span>
            </div>
            <div className={styles.headerSubtitle}>
              {subtitleText}
            </div>
          </div>
        </div>
      }
    >
      <div className={styles.modalBody}>
        {errorText && list.length > 0 ? (
          <Alert type="error" showIcon message={errorText} className={styles.errorAlert} />
        ) : null}

        <div className={styles.toolbar}>
          <Space size={8} className={styles.filterGroup}>
            <Input
              placeholder={isBatchZeroUpdate ? "当前批次无更新影片" : "输入片名开头"}
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
            <Button
              icon={<ReloadOutlined />}
              onClick={handleResetSearch}
              disabled={loading || (!keyword && !appliedKeyword.current)}
            >
              重置
            </Button>
          </Space>
          <Button
            icon={<ExportOutlined />}
            href={filmManageUrl}
            target="_blank"
            className={styles.jumpBtn}
          >
            前往影片库
          </Button>
        </div>

        <div className={styles.contentContainer}>
          <Spin spinning={loading} tip="正在加载更新影片...">
            {errorText && list.length === 0 && !loading ? (
              <div className={styles.emptyContainer}>
                <div className={`${styles.emptyIconWrap} ${styles.emptyIconError}`}>
                  <CloseCircleOutlined />
                </div>
                <div className={styles.emptyTitle}>获取更新影片失败</div>
                <div className={styles.emptyDesc}>{errorText}</div>
                <div className={styles.emptyActions}>
                  <Button
                    type="primary"
                    icon={<ReloadOutlined />}
                    onClick={() => void fetchFilms(cursorStack[cursorStack.length - 1] ?? firstFilmCursor, appliedKeyword.current)}
                  >
                    重新加载
                  </Button>
                </div>
              </div>
            ) : list.length === 0 && !loading ? (
              <div className={styles.emptyContainer}>
                <div className={styles.emptyIconWrap}>
                  {appliedKeyword.current ? <SearchOutlined /> : <InboxOutlined />}
                </div>
                <div className={styles.emptyTitle}>
                  {appliedKeyword.current ? "未找到匹配影片" : "暂无入库影片"}
                </div>
                <div className={styles.emptyDesc}>
                  {appliedKeyword.current ? (
                    <>
                      未搜索到片名以「<strong>{appliedKeyword.current}</strong>」开头的影片
                    </>
                  ) : sourceName && sourceName !== "全部站点" ? (
                    `「${sourceName}」暂无入库影片`
                  ) : (
                    "暂无入库影片"
                  )}
                </div>
                <div className={styles.emptyActions}>
                  {appliedKeyword.current ? (
                    <>
                      <Button
                        type="primary"
                        ghost
                        icon={<ReloadOutlined />}
                        onClick={handleResetSearch}
                      >
                        清除搜索条件
                      </Button>
                      <Button
                        icon={<ExportOutlined />}
                        href={filmManageUrl}
                        target="_blank"
                      >
                        前往影片库检索
                      </Button>
                    </>
                  ) : (
                    <>
                      <Button
                        type="primary"
                        ghost
                        icon={<ExportOutlined />}
                        href={filmManageUrl}
                        target="_blank"
                      >
                        前往影片库查看全部
                      </Button>
                      <Button
                        icon={<ReloadOutlined />}
                        onClick={() => {
                          setCursorStack([firstFilmCursor]);
                          void fetchFilms(firstFilmCursor, "");
                        }}
                      >
                        刷新清单
                      </Button>
                    </>
                  )}
                </div>
              </div>
            ) : list.length === 0 && loading ? (
              <div className={styles.loadingPlaceholder} />
            ) : (
              <div className={styles.listWrapper}>
                {list.map((film, index) => {
                  const playUrl = resolvePlayEntryPath(film.mid, { sourceId: film.sourceId || sourceId });
                  const badgeText = film.mid ? `#${film.mid}` : `#${index + 1}`;

                  return (
                    <div key={`${film.sourceId ?? sourceId}-${film.mid}`} className={styles.listItem}>
                      <span className={styles.midBadge}>{badgeText}</span>
                      <div className={styles.filmInfo}>
                        <Link href={playUrl} target="_blank" className={styles.filmName} title={film.name}>
                          {film.name}
                        </Link>
                      </div>
                      <Button
                        type="primary"
                        ghost
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
        </div>

        {cursorStack.length > 1 || hasMore ? (
          <div className={styles.paginationWrapper}>
            <div className={styles.paginationTotal}>
              共 <span className={styles.totalHighlight}>{total.toLocaleString("zh-CN")}</span> 部影片
            </div>
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
