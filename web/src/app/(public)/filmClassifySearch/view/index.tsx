"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useTransition } from "react";
import { LeftOutlined, RightOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { Pagination } from "antd";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import styles from "./index.module.less";
import {
  forceFinishNavigationLoading,
  startNavigationLoading,
} from "@/components/public/TopLoadingBar";

/**
 * 单行筛选行滚动箭头 Hook
 * 检测 .options 容器是否可向左/右滚动，提供滚动方法
 */
function useScrollArrows(dep: string) {
  const ref = useRef<HTMLDivElement>(null);
  const [canLeft, setCanLeft] = useState(false);
  const [canRight, setCanRight] = useState(false);

  const check = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    setCanLeft(el.scrollLeft > 2);
    setCanRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 2);
  }, []);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    check();
    el.addEventListener("scroll", check, { passive: true });
    const ro = new ResizeObserver(check);
    ro.observe(el);
    return () => {
      el.removeEventListener("scroll", check);
      ro.disconnect();
    };
  }, [check, dep]);

  const scrollBy = useCallback((dir: number) => {
    const el = ref.current;
    if (!el) return;
    el.scrollBy({ left: dir * el.clientWidth * 0.6, behavior: "smooth" });
  }, []);

  return { ref, canLeft, canRight, scrollLeft: () => scrollBy(-1), scrollRight: () => scrollBy(1) };
}

export default function FilmClassifySearchPageView({
  data,
  currentParams,
}: {
  data: any;
  currentParams: Record<string, string>;
}) {
  const router = useRouter();
  const [isRoutePending, startTransition] = useTransition();
  const [navigatingUrl, setNavigatingUrl] = useState("");
  const { title, list, search, params, page } = data;
  const safeList = Array.isArray(list) ? list : [];
  const safeSearch = {
    titles: search?.titles ?? {},
    sortList: Array.isArray(search?.sortList) ? search.sortList : [],
    tags: search?.tags ?? {},
  };
  const safeParams = params ?? {};
  const safePage = page ?? { total: 0, pageSize: 20 };
  const categoryKey = [safeParams.Pid || currentParams.Pid || "", safeParams.Category || currentParams.Category || ""].join(":");

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
    if (navigatingUrl && reachedTarget) {
      setNavigatingUrl("");
    }
  }, [isPending, navigatingUrl, reachedTarget]);

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

  const normalizeTagValue = (value: unknown) =>
    typeof value === "string" ? value.trim() : "";

  const getSafeTags = (tags: any[] | undefined) => {
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
  };

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
    if (normalizedValue === "") {
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

  return (
    <div className={`${styles.container} ${isPending ? styles.isPending : ""}`}>
      <div className={styles.resultHeader}>
        <div className={styles.count}>
          <span>{title?.name || "全部"}</span>共 {safePage.total ?? 0} 部影片
        </div>
      </div>

      {/* 筛选区始终保留，不进入全屏 loading */}
      <div className={styles.filterSection} aria-busy={isPending}>
        {safeSearch.sortList.map((key: string) => (
          <FilterRow
            key={key}
            filterKey={key}
            label={safeSearch.titles[key]}
            tags={getSafeTags(safeSearch.tags[key])}
            activeValue={normalizeTagValue(safeParams[key])}
            isPending={isPending}
            onTagClick={handleTagClick}
            normalizeTagValue={normalizeTagValue}
          />
        ))}
      </div>

      {/* 仅列表区域 loading */}
      <div className={styles.content}>
        {isPending ? (
          <div className={styles.listLoading} role="status" aria-live="polite">
            <AppLoading text="列表加载中" size="default" showHints={false} />
          </div>
        ) : (
          <FilmList key={categoryKey} list={safeList} className={styles.classifyGrid} />
        )}
      </div>

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

/** 单行筛选行：带左右箭头滚动控制 */
function FilterRow({
  filterKey,
  label,
  tags,
  activeValue,
  isPending,
  onTagClick,
  normalizeTagValue,
}: {
  filterKey: string;
  label: string;
  tags: any[];
  activeValue: string;
  isPending: boolean;
  onTagClick: (key: string, value: string) => void;
  normalizeTagValue: (v: unknown) => string;
}) {
  const { ref, canLeft, canRight, scrollLeft, scrollRight } = useScrollArrows(filterKey);

  return (
    <div className={styles.filterRow}>
      <div className={styles.label}>{label}</div>
      <div className={styles.optionsWrap}>
        {canLeft && (
          <button
            type="button"
            className={`${styles.arrowBtn} ${styles.arrowLeft}`}
            onClick={scrollLeft}
            aria-label="向左滚动"
            disabled={isPending}
          >
            <LeftOutlined />
          </button>
        )}
        <div className={styles.options} ref={ref}>
          {tags.map((tag: any, index: number) => (
            <span
              key={`${filterKey}-${tag.Value}-${tag.Name}-${index}`}
              className={`${styles.option} ${activeValue === normalizeTagValue(tag.Value) ? styles.active : ""}`}
              aria-disabled={isPending}
              onClick={() => {
                if (!isPending) {
                  onTagClick(filterKey, tag.Value);
                }
              }}
            >
              {tag.Name}
            </span>
          ))}
        </div>
        {canRight && (
          <button
            type="button"
            className={`${styles.arrowBtn} ${styles.arrowRight}`}
            onClick={scrollRight}
            aria-label="向右滚动"
            disabled={isPending}
          >
            <RightOutlined />
          </button>
        )}
      </div>
    </div>
  );
}
