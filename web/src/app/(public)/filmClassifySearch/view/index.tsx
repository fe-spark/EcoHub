"use client";

import { useEffect, useMemo, useRef, useState, useTransition } from "react";
import { LoadingOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { Pagination, Spin } from "antd";
import FilmList from "@/components/public/FilmList";
import { startNavigationLoading } from "@/components/public/TopLoadingBar";
import styles from "./index.module.less";

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
  const scrollerRefs = useRef<Record<string, HTMLDivElement | null>>({});
  const { title, list, search, params, page } = data;
  const safeList = Array.isArray(list) ? list : [];
  const safeParams = params ?? {};
  const safePage = page ?? { total: 0, pageSize: 20 };
  const sortList = Array.isArray(search?.sortList) ? (search.sortList as string[]) : [];
  const sortListKey = sortList.join("\0");
  const safeSearch = {
    titles: search?.titles ?? {},
    sortList,
    tags: search?.tags ?? {},
  };
  const categoryKey = [safeParams.Pid || currentParams.Pid || "", safeParams.Category || currentParams.Category || ""].join(":");
  const currentQueryString = useMemo(
    () => new URLSearchParams(currentParams).toString(),
    [currentParams],
  );
  const currentUrl = `/filmClassifySearch?${currentQueryString}`;
  const isPending = isRoutePending || (navigatingUrl !== "" && navigatingUrl !== currentUrl);

  // 路由落地后清本地 pending 标记（顶栏 loading 由 NavigationLoadingListener 关闭）
  useEffect(() => {
    if (navigatingUrl && navigatingUrl === currentUrl) {
      setNavigatingUrl("");
    }
  }, [currentUrl, navigatingUrl]);

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

  // 选中项滚动到该行最左侧附近，方便回看当前筛选
  useEffect(() => {
    if (isPending) {
      return;
    }

    const timers = safeSearch.sortList.map((key: string) => {
      return window.setTimeout(() => {
        const scroller = scrollerRefs.current[key];
        if (!scroller) {
          return;
        }
        const active = scroller.querySelector<HTMLElement>(`[data-filter-active="true"]`);
        if (!active) {
          scroller.scrollTo({ left: 0, behavior: "smooth" });
          return;
        }
        // 把选中标签滚到行首（留一点左边距）
        const left = Math.max(0, active.offsetLeft - 8);
        scroller.scrollTo({ left, behavior: "smooth" });
      }, 50);
    });

    return () => {
      timers.forEach((id) => window.clearTimeout(id));
    };
    // 仅 URL / pending 变化时滚动，避免 params 对象引用抖动
  }, [currentUrl, isPending, sortListKey]);

  const beginNavigation = (nextUrl: string, label: string) => {
    setNavigatingUrl(nextUrl);
    startNavigationLoading(label);
    window.scrollTo({ top: 0, behavior: "smooth" });
    startTransition(() => {
      router.push(nextUrl);
    });
  };

  const handleTagClick = (key: string, value: string, el?: HTMLElement | null) => {
    if (isPending) {
      return;
    }

    // 点击当下先把该项滚到行首，再跳转
    if (el) {
      const scroller = scrollerRefs.current[key];
      if (scroller) {
        const left = Math.max(0, el.offsetLeft - 8);
        scroller.scrollTo({ left, behavior: "smooth" });
      }
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

    beginNavigation(nextUrl, "筛选加载中");
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

    beginNavigation(nextUrl, "页面加载中");
  };

  return (
    <div className={`${styles.container} ${isPending ? styles.isPending : ""}`}>
      <div className={styles.resultHeader}>
        <div className={styles.count}>
          <span>{title?.name || "全部"}</span>
          {isPending ? "加载中" : <>共 {safePage.total ?? 0} 部影片</>}
        </div>
      </div>

      <div className={styles.filterSection} aria-busy={isPending}>
        {safeSearch.sortList.map((key: string) => (
          <div key={key} className={styles.filterRow}>
            <div className={styles.label}>{safeSearch.titles[key]}</div>
            <div
              className={styles.options}
              data-filter-scroller={key}
              ref={(node) => {
                scrollerRefs.current[key] = node;
              }}
            >
              {getSafeTags(safeSearch.tags[key]).map((tag: any, index: number) => {
                const isActive =
                  normalizeTagValue(safeParams[key]) === normalizeTagValue(tag.Value);
                return (
                  <span
                    key={`${key}-${tag.Value}-${tag.Name}-${index}`}
                    className={`${styles.option} ${isActive ? styles.active : ""}`}
                    data-filter-key={key}
                    data-filter-active={isActive ? "true" : "false"}
                    aria-disabled={isPending}
                    onClick={(event) =>
                      handleTagClick(key, tag.Value, event.currentTarget)
                    }
                  >
                    {tag.Name}
                  </span>
                );
              })}
            </div>
          </div>
        ))}
      </div>

      <div className={styles.content}>
        {isPending ? (
          <div className={styles.contentLoading} role="status" aria-live="polite">
            <Spin
              size="large"
              indicator={<LoadingOutlined style={{ fontSize: 36, color: "#fa8c16" }} spin />}
            />
            <p className={styles.contentLoadingTitle}>正在加载影片列表</p>
            <p className={styles.contentLoadingHint}>网络较慢时请稍候，完成后自动展示结果</p>
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
