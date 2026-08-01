"use client";

import { useCallback, useEffect, useMemo, useRef, useState, useTransition } from "react";
import { LoadingOutlined, LeftOutlined, RightOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { Pagination } from "antd";
import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";

import { startNavigationLoading } from "@/components/public/TopLoadingBar";

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
  const currentQueryString = useMemo(
    () => new URLSearchParams(currentParams).toString(),
    [currentParams],
  );
  const currentUrl = `/filmClassifySearch?${currentQueryString}`;
  const isPending = isRoutePending || (navigatingUrl !== "" && navigatingUrl !== currentUrl);

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
    const nextUrl = `/filmClassifySearch?${nextParams.toString()}`;

    if (nextUrl === currentUrl) {
      return;
    }

    startNavigationLoading("筛选影片中...");
    setNavigatingUrl(nextUrl);
    startTransition(() => {
      router.push(nextUrl);
    });
  };

  const handlePageChange = (pageNo: number) => {
    if (isPending) {
      return;
    }

    const nextParams = new URLSearchParams(currentParams);
    nextParams.set("current", pageNo.toString());
    const nextUrl = `/filmClassifySearch?${nextParams.toString()}`;

    if (nextUrl === currentUrl) {
      return;
    }

    startNavigationLoading("加载页面中...");
    setNavigatingUrl(nextUrl);
    startTransition(() => {
      router.push(nextUrl);
    });
  };

  return (
    <div className={`${styles.container} ${isPending ? styles.isPending : ""}`}>
      <div className={styles.resultHeader}>
        <div className={styles.count}>
          <span>{title?.name || "全部"}</span>共 {safePage.total ?? 0} 部影片
        </div>
      </div>

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

      <div className={styles.content}>
        {isPending && (
          <div className={styles.contentLoadingMask} role="status" aria-live="polite">
            <LoadingOutlined />
            <span>正在筛选影片...</span>
          </div>
        )}
        <FilmList key={categoryKey} list={safeList} className={styles.classifyGrid} />
      </div>

      {safeList.length > 0 && (
        <div className={styles.paginationWrapper}>
          <Pagination
            current={parseInt(currentParams.current || "1", 10)}
            total={safePage.total ?? 0}
            pageSize={safePage.pageSize || 20}
            onChange={handlePageChange}
            disabled={isPending}
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
          <button type="button" className={`${styles.arrowBtn} ${styles.arrowLeft}`} onClick={scrollLeft} aria-label="向左滚动">
            <LeftOutlined />
          </button>
        )}
        <div className={styles.options} ref={ref}>
          {tags.map((tag: any, index: number) => (
            <span
              key={`${filterKey}-${tag.Value}-${tag.Name}-${index}`}
              className={`${styles.option} ${activeValue === normalizeTagValue(tag.Value) ? styles.active : ""}`}
              aria-disabled={isPending}
              onClick={() => onTagClick(filterKey, tag.Value)}
            >
              {tag.Name}
            </span>
          ))}
        </div>
        {canRight && (
          <button type="button" className={`${styles.arrowBtn} ${styles.arrowRight}`} onClick={scrollRight} aria-label="向右滚动">
            <RightOutlined />
          </button>
        )}
      </div>
    </div>
  );
}
