"use client";

import React, { useEffect, useMemo, useRef, useState, useSyncExternalStore } from "react";
import { Button } from "antd";
import {
  AppstoreOutlined,
  ClearOutlined,
  ClockCircleOutlined,
  CloseCircleFilled,
  CloseOutlined,
  FireOutlined,
  SearchOutlined,
  UnorderedListOutlined,
} from "@ant-design/icons";
import { useAppMessage } from "@/lib/useAppMessage";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import SourceTabs from "./SourceTabs";
import SearchResultPanel from "./SearchResultPanel";
import useSearchSources from "./useSearchSources";
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

const EMPTY_HISTORY_LIST: string[] = [];

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

function subscribeHistory(callback: () => void) {
  if (typeof window === "undefined") return () => {};
  window.addEventListener("storage", callback);
  window.addEventListener("ecohub:search-history", callback);
  return () => {
    window.removeEventListener("storage", callback);
    window.removeEventListener("ecohub:search-history", callback);
  };
}

function getHistorySnapshot(): string {
  if (typeof window === "undefined") return "[]";
  return localStorage.getItem(SEARCH_HISTORY_KEY) || "[]";
}

function subscribeViewMode(callback: () => void) {
  if (typeof window === "undefined") return () => {};
  window.addEventListener("storage", callback);
  window.addEventListener("ecohub:search-view-mode", callback);
  return () => {
    window.removeEventListener("storage", callback);
    window.removeEventListener("ecohub:search-view-mode", callback);
  };
}

function getViewModeSnapshot(): "grid" | "detail" {
  if (typeof window === "undefined") return "grid";
  const stored = localStorage.getItem(SEARCH_VIEW_MODE_KEY);
  return stored === "detail" ? "detail" : "grid";
}

const SORT_OPTIONS = [
  { key: "", label: "综合相关度" },
  { key: "hits", label: "最多播放" },
  { key: "latest", label: "最近更新" },
  { key: "score", label: "最高评分" },
  { key: "year", label: "上映年份" },
];

function buildSearchPath(keyword: string, current: string, sort: string, source: string) {
  const params = new URLSearchParams({
    search: keyword,
    current,
  });
  if (!source && sort) {
    params.set("sort", sort);
  }
  if (source) {
    params.set("source", source);
  }
  return `/search?${params.toString()}`;
}

export default function SearchPageView({
  data,
  keyword,
  current,
  sort = "",
  source = "",
  hotKeywords = [],
}: {
  data: any;
  keyword: string;
  current: string;
  sort?: string;
  source?: string;
  hotKeywords?: string[];
}) {
  const { navigate, isNavigating } = useContentNavigate();
  const { message } = useAppMessage();
  const inputRef = useRef<HTMLInputElement>(null);
  const [searchKeyword, setSearchKeyword] = useState(keyword);
  const [prevParamsKey, setPrevParamsKey] = useState(`${keyword}:${current}:${sort}:${source}`);

  if (prevParamsKey !== `${keyword}:${current}:${sort}:${source}`) {
    setPrevParamsKey(`${keyword}:${current}:${sort}:${source}`);
    setSearchKeyword(keyword);
  }

  const rawHistory = useSyncExternalStore(subscribeHistory, getHistorySnapshot, () => "[]");
  const history = useMemo(() => {
    try {
      const parsed = JSON.parse(rawHistory);
      return Array.isArray(parsed) ? parsed.slice(0, 8) : EMPTY_HISTORY_LIST;
    } catch {
      return EMPTY_HISTORY_LIST;
    }
  }, [rawHistory]);

  const viewMode = useSyncExternalStore<"grid" | "detail">(subscribeViewMode, getViewModeSnapshot, () => "grid");
  const {
    sources,
    activeId,
    list,
    page,
    sourceError,
    listLoading,
    changeSource,
    changePage,
  } = useSearchSources({ keyword, sort, source, current, data });

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
    const trimmed = keyword.trim();
    if (!trimmed) return;
    const currentList = getInitialHistory();
    const nextHistory = [
      trimmed,
      ...currentList.filter((item) => item !== trimmed),
    ].slice(0, 8);
    try {
      localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(nextHistory));
      window.dispatchEvent(new Event("ecohub:search-history"));
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
    try {
      localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(nextHistory));
      window.dispatchEvent(new Event("ecohub:search-history"));
    } catch {}
  };

  const removeHistoryItem = (target: string) => {
    const currentList = getInitialHistory();
    const nextHistory = currentList.filter((item) => item !== target);
    try {
      localStorage.setItem(SEARCH_HISTORY_KEY, JSON.stringify(nextHistory));
      window.dispatchEvent(new Event("ecohub:search-history"));
    } catch {}
  };

  const clearHistory = () => {
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
    setSearchKeyword(trimmed);
    saveHistory(trimmed);
    navigate(buildSearchPath(trimmed, "1", sort, ""), "搜索加载中...");
  };

  const handleSortChange = (newSort: string) => {
    if (newSort === sort && !source) return;
    setSearchKeyword(keyword);
    navigate(buildSearchPath(keyword, "1", newSort, ""), "排序切换中...");
  };

  const handlePageChange = (nextPage: number) => {
    setSearchKeyword(keyword);
    void changePage(nextPage);
  };

  const handleSourceChange = (nextSource: string) => {
    setSearchKeyword(keyword);
    changeSource(nextSource);
  };

  const handlePlay = (movie: { id?: string | number; sourceId?: string; sourceMid?: string | number }) => {
    const localId = Number(movie?.id) > 0 ? String(movie.id) : "";
    navigate(
      resolvePlayEntryPath(localId, {
        sourceId: movie?.sourceId,
        sourceMid: movie?.sourceMid,
        episodeIndex: 0,
      }),
      "进入播放页...",
    );
  };

  const toggleViewMode = (mode: "grid" | "detail") => {
    try {
      localStorage.setItem(SEARCH_VIEW_MODE_KEY, mode);
      window.dispatchEvent(new Event("ecohub:search-view-mode"));
    } catch {}
  };

  const totalCount = page?.total ?? list.length ?? 0;
  const hasResults = Array.isArray(list) && list.length > 0;
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
            placeholder="搜索电影、剧集、动漫、综艺、主演、导演..."
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

      <div className={styles.sourceBlock}>
        <header className={styles.resultHeader}>
          <div className={styles.resultSummary}>
            {keyword ? (
              <>
                <h1 className={styles.resultTitle}>
                  &ldquo;<span className={styles.keywordHighlight}>{keyword}</span>&rdquo; 的搜索结果
                </h1>
                <span className={styles.totalBadge}>
                  {listLoading ? "正在搜索..." : `共 ${totalCount} 部作品`}
                </span>
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

        {keyword && (
          <SourceTabs
            sources={sources}
            activeId={activeId}
            onChange={handleSourceChange}
          />
        )}
      </div>

      {/* YouTube 风格排序筛选栏 */}
      {keyword && !activeId && (
        <div className={styles.sortBar} aria-label="排序方式">
          {SORT_OPTIONS.map((opt) => {
            const isActive = (opt.key === "" && (!sort || sort === "relevance")) || opt.key === sort;
            return (
              <button
                type="button"
                key={opt.key}
                className={`${styles.sortChip} ${isActive ? styles.active : ""}`}
                aria-pressed={isActive}
                onClick={() => handleSortChange(opt.key)}
              >
                {opt.label}
              </button>
            );
          })}
        </div>
      )}

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

      <SearchResultPanel
        keyword={keyword}
        current={current}
        list={list}
        page={page}
        totalCount={totalCount}
        listLoading={listLoading}
        hasResults={hasResults}
        sourceError={sourceError}
        viewMode={viewMode}
        onPlay={handlePlay}
        onPageChange={handlePageChange}
      />
    </div>
  );
}
