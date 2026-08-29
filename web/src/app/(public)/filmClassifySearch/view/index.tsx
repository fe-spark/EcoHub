"use client";

import React, { useCallback, useEffect, useMemo, useRef, useState, useTransition } from "react";
import {
  AppstoreOutlined,
  CompassOutlined,
  FilterOutlined,
} from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { Pagination } from "antd";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import {
  forceFinishNavigationLoading,
  startNavigationLoading,
} from "@/components/public/TopLoadingBar";
import DesktopFilterPanel, { ActiveChipItem } from "./components/DesktopFilterPanel";
import MobileFilterDrawer from "./components/MobileFilterDrawer";
import { isDefaultSortValue, normalizeTagValue } from "./filter-params";
import styles from "./index.module.less";

function getSafeTags(tags: any[] | undefined) {
  if (!Array.isArray(tags)) {
    return [];
  }
  return tags.filter((tag, index) => {
    const value = normalizeTagValue(tag?.Value);
    if (value !== "") {
      return true;
    }
    return index === 0 && tag?.Name === "全部";
  });
}

export default function FilmClassifySearchPageView({
  data,
  currentParams,
}: {
  data: any;
  currentParams: Record<string, string>;
}) {
  const router = useRouter();
  const { navigate } = useContentNavigate();
  const [isRoutePending, startTransition] = useTransition();
  const [navigatingUrl, setNavigatingUrl] = useState("");
  const { title, list, search, params, page } = data || {};
  const safeList = Array.isArray(list) ? list : [];
  const safeSearch = {
    titles: search?.titles ?? {},
    sortList: Array.isArray(search?.sortList) ? search.sortList : [],
    tags: search?.tags ?? {},
  };
  const safeParams = params ?? {};
  const safePage = page ?? { total: 0, pageSize: 48 };
  const pid = safeParams.Pid || currentParams.Pid || "0";
  const categoryName = title?.name || "分类";
  const categoryKey = [pid, safeParams.Category || currentParams.Category || ""].join(":");

  /** 语义化 query 比较：忽略键序与空值，避免 page 过滤假值后全等失败卡 loading */
  const normalizeQueryKey = useCallback((input: string | Record<string, string>) => {
    let entries: [string, string][];
    if (typeof input === "string") {
      const q = input.includes("?") ? input.slice(input.indexOf("?") + 1) : input;
      entries = [...new URLSearchParams(q).entries()];
    } else {
      entries = Object.entries(input);
    }
    const filtered = entries
      .filter(([k, v]) => !k.startsWith("_") && v !== "")
      .sort(([a], [b]) => a.localeCompare(b));
    return new URLSearchParams(filtered).toString();
  }, []);

  const currentQueryKey = useMemo(
    () => normalizeQueryKey(currentParams),
    [currentParams, normalizeQueryKey],
  );
  const currentUrl = `/filmClassifySearch?${currentQueryKey}`;
  const reachedTarget =
    navigatingUrl === "" ||
    normalizeQueryKey(navigatingUrl) === currentQueryKey;
  const isPending = isRoutePending || (navigatingUrl !== "" && !reachedTarget);
  const loadingBarStartedRef = useRef(false);

  useEffect(() => {
    if (isPending) {
      loadingBarStartedRef.current = true;
      return;
    }
    if (loadingBarStartedRef.current) {
      loadingBarStartedRef.current = false;
      forceFinishNavigationLoading();
    }
  }, [isPending]);

  if (!isPending && navigatingUrl && reachedTarget) {
    setNavigatingUrl("");
  }

  useEffect(() => {
    if (!navigatingUrl) {
      return;
    }
    const timer = window.setTimeout(() => {
      setNavigatingUrl("");
      forceFinishNavigationLoading();
    }, 15_000);
    return () => window.clearTimeout(timer);
  }, [navigatingUrl]);

  const tagsMap = useMemo(() => {
    const map: Record<string, any[]> = {};
    for (const key of safeSearch.sortList) {
      map[key] = getSafeTags(safeSearch.tags[key]);
    }
    return map;
  }, [safeSearch.sortList, safeSearch.tags]);

  const pushFilterUrl = (nextUrl: string, barLabel: string) => {
    if (nextUrl === currentUrl || isPending) {
      return;
    }
    startNavigationLoading(barLabel);
    setNavigatingUrl(nextUrl);
    startTransition(() => {
      router.push(nextUrl);
    });
  };

  const handleTagClick = (key: string, value: string) => {
    if (isPending) {
      return;
    }

    const nextParams = new URLSearchParams(currentParams);
    const normalizedValue = normalizeTagValue(value);
    if (normalizedValue === "" || isDefaultSortValue(key, normalizedValue)) {
      nextParams.delete(key);
    } else {
      nextParams.set(key, normalizedValue);
    }
    nextParams.set("current", "1");
    pushFilterUrl(`/filmClassifySearch?${nextParams.toString()}`, "筛选影片中...");
  };

  const handlePageChange = (pageNo: number) => {
    if (isPending) {
      return;
    }

    const nextParams = new URLSearchParams(currentParams);
    nextParams.set("current", pageNo.toString());
    pushFilterUrl(`/filmClassifySearch?${nextParams.toString()}`, "加载列表中...");
  };

  // 统计已激活的筛选条件列表
  const activeChips = useMemo<ActiveChipItem[]>(() => {
    const items: ActiveChipItem[] = [];
    for (const key of safeSearch.sortList) {
      const rawVal = normalizeTagValue(currentParams[key]);
      if (!rawVal || rawVal === "") continue;
      if (isDefaultSortValue(key, rawVal)) continue;

      const tags = tagsMap[key] || [];
      const matchTag = tags.find((t) => normalizeTagValue(t.Value) === rawVal);
      const name = matchTag?.Name || rawVal;
      const label = safeSearch.titles[key] || key;

      items.push({ key, label, name, value: rawVal });
    }
    return items;
  }, [safeSearch.sortList, safeSearch.titles, tagsMap, currentParams]);

  const handleRemoveChip = (key: string) => {
    handleTagClick(key, "");
  };

  const handleResetFilters = () => {
    if (isPending || activeChips.length === 0) {
      return;
    }
    const nextParams = new URLSearchParams();
    if (currentParams.Pid) {
      nextParams.set("Pid", currentParams.Pid);
    }
    nextParams.set("current", "1");
    pushFilterUrl(`/filmClassifySearch?${nextParams.toString()}`, "重置筛选中...");
  };

  const handleApplyFilters = (nextObj: Record<string, string>) => {
    const nextParams = new URLSearchParams();
    if (currentParams.Pid) {
      nextParams.set("Pid", currentParams.Pid);
    }
    for (const [k, v] of Object.entries(nextObj)) {
      if (v && v !== "" && k !== "Pid" && k !== "current") {
        if (isDefaultSortValue(k, v)) {
          continue;
        }
        nextParams.set(k, v);
      }
    }
    nextParams.set("current", "1");
    pushFilterUrl(`/filmClassifySearch?${nextParams.toString()}`, "筛选影片中...");
  };

  return (
    <div className={`${styles.container} ${isPending ? styles.isPending : ""}`}>
      {/* 头部专区氛围卡片 */}
      <header className={styles.heroHeader}>
        <div className={styles.heroGlow} aria-hidden />
        <div className={styles.heroContent}>
          <div className={styles.heroMeta}>
            <span className={styles.eyebrow}>
              <FilterOutlined className={styles.eyebrowIcon} />
              <span>片库检索 · 共 {safePage.total ?? 0} 部</span>
            </span>
            <h1 className={styles.heroTitle}>{categoryName}片库</h1>
          </div>

          <div className={styles.headerActions}>
            <div className={styles.tabSwitcher}>
              <button
                type="button"
                className={styles.tabBtn}
                onClick={() =>
                  navigate(`/filmClassify?Pid=${pid}`, "分类专区加载中...")
                }
              >
                <AppstoreOutlined />
                <span>精选推荐</span>
              </button>
              <button
                type="button"
                className={`${styles.tabBtn} ${styles.active}`}
                onClick={() =>
                  navigate(`/filmClassifySearch?Pid=${pid}`, "片库加载中...")
                }
              >
                <CompassOutlined />
                <span>全量片库</span>
              </button>
            </div>
          </div>
        </div>
      </header>

      {/* PC 桌面端全量平铺筛选面板 (移动端隐藏) */}
      {safeSearch.sortList.length > 0 && (
        <DesktopFilterPanel
          sortList={safeSearch.sortList}
          titles={safeSearch.titles}
          tagsMap={tagsMap}
          activeParams={currentParams}
          activeChips={activeChips}
          total={safePage.total}
          isPending={isPending}
          onTagClick={handleTagClick}
          onRemoveChip={handleRemoveChip}
          onReset={handleResetFilters}
          normalizeTagValue={normalizeTagValue}
        />
      )}

      {/* 移动端常驻已选条件状态条 + 底部抽屉筛选 (PC 端隐藏) */}
      {safeSearch.sortList.length > 0 && (
        <MobileFilterDrawer
          sortList={safeSearch.sortList}
          titles={safeSearch.titles}
          tagsMap={tagsMap}
          activeParams={currentParams}
          activeChips={activeChips}
          total={safePage.total}
          isPending={isPending}
          onApplyFilters={handleApplyFilters}
          onQuickSelect={handleTagClick}
          onRemoveChip={handleRemoveChip}
          onReset={handleResetFilters}
          normalizeTagValue={normalizeTagValue}
        />
      )}

      {/* 列表与局部加载 */}
      <div className={styles.content}>
        {isPending ? (
          <div className={styles.listLoading} role="status" aria-live="polite">
            <AppLoading text="列表加载中..." size="default" showHints={false} />
          </div>
        ) : (
          <FilmList key={categoryKey} list={safeList} col={6} />
        )}
      </div>

      {/* 分页控制 */}
      {!isPending && safeList.length > 0 && (
        <div className={styles.paginationWrapper}>
          <Pagination
            current={parseInt(currentParams.current || "1", 10)}
            total={safePage.total ?? 0}
            pageSize={safePage.pageSize || 20}
            onChange={handlePageChange}
            showSizeChanger={false}
            hideOnSinglePage
          />
        </div>
      )}
    </div>
  );
}

