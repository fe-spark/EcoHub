"use client";

import React, { useEffect, useRef, useState } from "react";
import { Button, Pagination } from "antd";
import {
  AppstoreOutlined,
  CaretRightOutlined,
  ClearOutlined,
  ClockCircleOutlined,
  CloseCircleFilled,
  CloseOutlined,
  FireOutlined,
  SearchOutlined,
  UnorderedListOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";

const HOT_KEYWORDS = [
  "凡人修仙传",
  "庆余年",
  "仙逆",
  "吞噬星空",
  "遮天",
  "斗破苍穹",
  "白夜破晓",
  "大奉打更人",
];

const SEARCH_HISTORY_KEY = "ecohub_search_history";
const SEARCH_VIEW_MODE_KEY = "ecohub_search_view_mode";
const FOCUS_SEARCH_EVENT = "ecohub:focus-search";

function focusVisibleSearchInput(el: HTMLInputElement | null) {
  if (!el) {
    return false;
  }
  const rect = el.getBoundingClientRect();
  if (rect.width <= 0 || rect.height <= 0) {
    return false;
  }
  el.focus();
  return document.activeElement === el;
}

function normalizeMetaValue(value?: string | number | null) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") {
    return "";
  }
  return text;
}

function getPrimaryPlotTag(classTag?: string) {
  return (
    normalizeMetaValue(classTag)
      .split(/[,，/|、\s]+/)
      .map((tag) => tag.trim())
      .find(Boolean) || ""
  );
}

function getInitialHistory(): string[] {
  if (typeof window === "undefined") return [];
  try {
    const stored = localStorage.getItem(SEARCH_HISTORY_KEY);
    if (stored) {
      const parsed = JSON.parse(stored);
      if (Array.isArray(parsed)) {
        return parsed.slice(0, 8);
      }
    }
  } catch {}
  return [];
}

function getInitialViewMode(): "grid" | "detail" {
  if (typeof window === "undefined") return "grid";
  try {
    const stored = localStorage.getItem(SEARCH_VIEW_MODE_KEY);
    if (stored === "grid" || stored === "detail") {
      return stored;
    }
  } catch {}
  return "grid";
}

export default function SearchPageView({
  data,
  keyword,
  current,
  hotKeywords = [],
}: {
  data: any;
  keyword: string;
  current: string;
  hotKeywords?: string[];
}) {
  const { navigate, isNavigating } = useContentNavigate();
  const { message } = useAppMessage();
  const inputRef = useRef<HTMLInputElement>(null);
  const [searchKeyword, setSearchKeyword] = useState(keyword);
  const [history, setHistory] = useState<string[]>([]);
  const [viewMode, setViewMode] = useState<"grid" | "detail">("grid");

  useEffect(() => {
    const onFocusSearch = () => {
      const el = inputRef.current;
      if (!focusVisibleSearchInput(el) || !el) {
        return;
      }
      if (el.value) {
        el.select();
      }
    };
    window.addEventListener(FOCUS_SEARCH_EVENT, onFocusSearch);
    return () => window.removeEventListener(FOCUS_SEARCH_EVENT, onFocusSearch);
  }, []);

  useEffect(() => {
    if (isNavigating || keyword.trim()) {
      return;
    }
    let frame = 0;
    let tries = 0;
    const tryFocus = () => {
      if (focusVisibleSearchInput(inputRef.current)) {
        return;
      }
      if (tries < 8) {
        tries += 1;
        frame = window.requestAnimationFrame(tryFocus);
      }
    };
    frame = window.requestAnimationFrame(tryFocus);
    return () => window.cancelAnimationFrame(frame);
  }, [isNavigating, keyword]);

  useEffect(() => {
    setHistory(getInitialHistory());
    setViewMode(getInitialViewMode());
    const handleStorage = () => {
      setHistory(getInitialHistory());
    };
    window.addEventListener("storage", handleStorage);
    window.addEventListener("ecohub:search-history", handleStorage);
    return () => {
      window.removeEventListener("storage", handleStorage);
      window.removeEventListener("ecohub:search-history", handleStorage);
    };
  }, []);

  useEffect(() => {
    setSearchKeyword(keyword);
    const trimmed = keyword.trim();
    if (!trimmed) return;
    const currentList = getInitialHistory();
    const nextHistory = [
      trimmed,
      ...currentList.filter((item) => item !== trimmed),
    ].slice(0, 8);
    setHistory(nextHistory);
    try {
      localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(nextHistory));
    } catch {}
  }, [keyword]);

  const saveHistory = (kw: string) => {
    const trimmed = kw.trim();
    if (!trimmed) return;
    const currentList = getInitialHistory();
    const nextHistory = [
      trimmed,
      ...currentList.filter((item) => item !== trimmed),
    ].slice(0, 8);
    setHistory(nextHistory);
    try {
      localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(nextHistory));
      window.dispatchEvent(new Event("ecohub:search-history"));
    } catch {}
  };

  const removeHistoryItem = (target: string) => {
    const currentList = getInitialHistory();
    const nextHistory = currentList.filter((item) => item !== target);
    setHistory(nextHistory);
    try {
      localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(nextHistory));
      window.dispatchEvent(new Event("ecohub:search-history"));
    } catch {}
  };

  const clearHistory = () => {
    setHistory([]);
    try {
      localStorage.removeItem(SEARCH_HISTORY_KEY);
      window.dispatchEvent(new Event("ecohub:search-history"));
    } catch {}
  };

  const executeSearch = (targetKeyword: string) => {
    const trimmed = targetKeyword.trim();
    if (!trimmed) {
      message.warning("请输入搜索关键词");
      return;
    }
    saveHistory(trimmed);
    navigate(
      `/search?search=${encodeURIComponent(trimmed)}&current=1`,
      "搜索加载中...",
    );
  };

  const handlePageChange = (page: number) => {
    navigate(
      `/search?search=${encodeURIComponent(keyword)}&current=${page}`,
      "页面加载中...",
    );
  };

  const handlePlay = (id: string) => {
    navigate(
      resolvePlayEntryPath(id, { sourceId: "0", episodeIndex: 0 }),
      "进入播放页...",
    );
  };

  const toggleViewMode = (mode: "grid" | "detail") => {
    setViewMode(mode);
    try {
      localStorage.setItem(SEARCH_VIEW_MODE_KEY, mode);
    } catch {}
  };

  const totalCount = data?.page?.total ?? data?.list?.length ?? 0;
  const hasResults = Array.isArray(data?.list) && data.list.length > 0;
  const displayHotList =
    Array.isArray(hotKeywords) && hotKeywords.length > 0
      ? hotKeywords
      : HOT_KEYWORDS;

  return (
    <div className={styles.container}>
      <div className={styles.searchBar}>
        <div className={styles.searchInputBox}>
          <SearchOutlined className={styles.inputIcon} />
          <input
            ref={inputRef}
            type="text"
            placeholder="搜索电影、剧集、动漫、综艺..."
            value={searchKeyword}
            onChange={(e) => setSearchKeyword(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && executeSearch(searchKeyword)}
            aria-label="搜索影视内容"
            autoComplete="off"
          />
          {searchKeyword && (
            <button
              type="button"
              className={styles.clearBtn}
              onClick={() => setSearchKeyword("")}
              aria-label="清空输入"
            >
              <CloseCircleFilled />
            </button>
          )}
        </div>
        <Button
          type="primary"
          className={styles.searchSubmitBtn}
          onClick={() => executeSearch(searchKeyword)}
        >
          搜索
        </Button>
      </div>

      {/* 搜索结果头部信息条 */}
      <header className={styles.resultHeader}>
        <div className={styles.resultSummary}>
          {keyword ? (
            <>
              <h1 className={styles.resultTitle}>
                &ldquo;<span className={styles.keywordHighlight}>{keyword}</span>&rdquo; 的搜索结果
              </h1>
              <span className={styles.totalBadge}>共 {totalCount} 部作品</span>
            </>
          ) : (
            <h1 className={styles.resultTitle}>影视搜索</h1>
          )}
        </div>

        {hasResults && (
          <div className={styles.viewModeSwitcher}>
            <button
              type="button"
              className={`${styles.modeBtn} ${viewMode === "grid" ? styles.active : ""}`}
              onClick={() => toggleViewMode("grid")}
              title="海报网格视图"
              aria-label="海报网格视图"
            >
              <AppstoreOutlined />
              <span>海报</span>
            </button>
            <button
              type="button"
              className={`${styles.modeBtn} ${viewMode === "detail" ? styles.active : ""}`}
              onClick={() => toggleViewMode("detail")}
              title="图文详情视图"
              aria-label="图文详情视图"
            >
              <UnorderedListOutlined />
              <span>详情</span>
            </button>
          </div>
        )}
      </header>

      {/* 快捷推荐与历史栏（常驻展示） */}
      <section className={styles.quickBar}>
        {history.length > 0 && (
          <div className={styles.quickRow}>
            <span className={styles.quickRowLabel}>
              <ClockCircleOutlined /> 搜索历史:
            </span>
            <div className={styles.chipRow}>
              {history.map((item) => (
                <span key={item} className={styles.historyChip}>
                  <button
                    type="button"
                    className={styles.chipText}
                    onClick={() => executeSearch(item)}
                  >
                    {item}
                  </button>
                  <button
                    type="button"
                    className={styles.chipDeleteBtn}
                    onClick={(e) => {
                      e.stopPropagation();
                      removeHistoryItem(item);
                    }}
                    title="删除记录"
                    aria-label={`删除 ${item} 搜索记录`}
                  >
                    <CloseOutlined />
                  </button>
                </span>
              ))}
              <button
                type="button"
                className={styles.clearHistoryLink}
                onClick={clearHistory}
                title="清空所有历史"
              >
                <ClearOutlined /> 清空
              </button>
            </div>
          </div>
        )}

        {displayHotList.length > 0 && (
          <div className={styles.quickRow}>
            <span className={styles.quickRowLabel}>
              <FireOutlined className={styles.fireIcon} /> 热门推荐:
            </span>
            <div className={styles.chipRow}>
              {displayHotList.map((item, idx) => (
                <button
                  type="button"
                  key={item}
                  className={`${styles.chip} ${styles.hotChip}`}
                  onClick={() => executeSearch(item)}
                >
                  <span className={styles.rankNum}>{idx + 1}</span>
                  {item}
                </button>
              ))}
            </div>
          </div>
        )}
      </section>

      {/* 搜索结果呈现 */}
      {hasResults ? (
        <section className={styles.searchRes} aria-label="搜索结果列表">
          {/* 模式 A：海报瀑布流网格 */}
          {viewMode === "grid" ? (
            <div className={styles.gridContainer}>
              <FilmList list={data.list} col={6} />
            </div>
          ) : (
            /* 模式 B：图文详情卡片 */
            <div className={styles.resultList}>
              {data.list.map((movie: any) => (
                <article key={movie.id} className={styles.searchItem}>
                  <div
                    className={styles.posterWrapper}
                    onClick={() => handlePlay(movie.id)}
                  >
                    {/* eslint-disable-next-line @next/next/no-img-element */}
                    <img
                      src={movie.picture || FALLBACK_IMG}
                      className={styles.poster}
                      alt={movie.name}
                      loading="lazy"
                    />
                    {movie.remarks && (
                      <span className={styles.posterRemark}>{movie.remarks}</span>
                    )}
                  </div>

                  <div className={styles.intro}>
                    <h3
                      className={styles.filmName}
                      onClick={() => handlePlay(movie.id)}
                    >
                      {movie.name}
                    </h3>

                    <div className={styles.tags}>
                      {movie.cName && (
                        <span className={`${styles.tag} ${styles.category}`}>
                          {movie.cName}
                        </span>
                      )}
                      {normalizeMetaValue(movie.year) && (
                        <span className={styles.tag}>
                          {normalizeMetaValue(movie.year)}
                        </span>
                      )}
                      {normalizeMetaValue(movie.area) && (
                        <span className={styles.tag}>
                          {normalizeMetaValue(movie.area)}
                        </span>
                      )}
                      {normalizeMetaValue(movie.language) && (
                        <span className={styles.tag}>
                          {normalizeMetaValue(movie.language)}
                        </span>
                      )}
                      {getPrimaryPlotTag(movie.classTag) && (
                        <span className={styles.tag}>
                          {getPrimaryPlotTag(movie.classTag)}
                        </span>
                      )}
                    </div>

                    <div className={styles.metaRow}>
                      <span className={styles.metaLabel}>导演</span>
                      <span className={styles.metaValue}>
                        {movie.director || "暂无导演信息"}
                      </span>
                    </div>

                    <div className={styles.metaRow}>
                      <span className={styles.metaLabel}>主演</span>
                      <span className={styles.metaValue}>
                        {movie.actor || "暂无主演信息"}
                      </span>
                    </div>

                    <p className={styles.blurb}>
                      {movie.blurb?.replace(/[\s　]+/g, " ").trim() ||
                        "暂无剧情简介，点击立即进入播放页体验高清流畅观影。"}
                    </p>

                    <div className={styles.actionRow}>
                      <Button
                        type="primary"
                        icon={<CaretRightOutlined />}
                        className={styles.playBtn}
                        onClick={() => handlePlay(movie.id)}
                      >
                        立即播放
                      </Button>
                    </div>
                  </div>
                </article>
              ))}
            </div>
          )}

          <div className={styles.pagination}>
            <Pagination
              current={parseInt(current || "1", 10)}
              total={data.page?.total ?? totalCount}
              pageSize={data.page?.pageSize || 20}
              onChange={handlePageChange}
              showSizeChanger={false}
              hideOnSinglePage
            />
          </div>
        </section>
      ) : (
        /* 无搜索结果 / 初始态统一探索面板 */
        <section className={styles.emptyContainer}>
          <div className={styles.emptyHeader}>
            <div className={styles.emptyIconCircle}>
              <VideoCameraOutlined />
            </div>
            <h2 className={styles.emptyTitle}>
              {keyword ? (
                <>未找到与 &ldquo;<span className={styles.keywordHighlight}>{keyword}</span>&rdquo; 相关的影视</>
              ) : (
                "探索全网热门影视"
              )}
            </h2>
            <p className={styles.emptyDesc}>
              建议缩短或更换搜索词，也可以直接尝试上方的热门搜索推荐
            </p>
          </div>
        </section>
      )}
    </div>
  );
}
