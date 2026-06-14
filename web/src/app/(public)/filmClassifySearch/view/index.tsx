"use client";

import { useMemo, useState, useTransition } from "react";
import { LoadingOutlined } from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { Pagination } from "antd";
import FilmList from "@/components/public/FilmList";
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
          <div key={key} className={styles.filterRow}>
            <div className={styles.label}>{safeSearch.titles[key]}</div>
            <div className={styles.options}>
              {getSafeTags(safeSearch.tags[key]).map((tag: any, index: number) => (
                <span
                  key={`${key}-${tag.Value}-${tag.Name}-${index}`}
                  className={`${styles.option} ${normalizeTagValue(safeParams[key]) === normalizeTagValue(tag.Value) ? styles.active : ""}`}
                  aria-disabled={isPending}
                  onClick={() => handleTagClick(key, tag.Value)}
                >
                  {tag.Name}
                </span>
              ))}
            </div>
          </div>
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
