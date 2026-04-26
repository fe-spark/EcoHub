"use client";

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
  const { title, list, search, params, page } = data;
  const safeList = Array.isArray(list) ? list : [];

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
    const nextParams = new URLSearchParams(currentParams);
    const normalizedValue = normalizeTagValue(value);
    if (normalizedValue === "") {
      nextParams.delete(key);
    } else {
      nextParams.set(key, normalizedValue);
    }
    nextParams.set("current", "1");
    router.push(`/filmClassifySearch?${nextParams.toString()}`);
  };

  const handlePageChange = (pageNo: number) => {
    const nextParams = new URLSearchParams(currentParams);
    nextParams.set("current", pageNo.toString());
    router.push(`/filmClassifySearch?${nextParams.toString()}`);
  };

  return (
    <div className={styles.container}>
      <div className={styles.resultHeader}>
        <div className={styles.count}>
          <span>{title?.name || "全部"}</span>共 {page.total} 部影片
        </div>
      </div>

      <div className={styles.filterSection}>
        {search.sortList.map((key: string) => (
          <div key={key} className={styles.filterRow}>
            <div className={styles.label}>{search.titles[key]}</div>
            <div className={styles.options}>
              {getSafeTags(search.tags[key]).map((tag: any, index: number) => (
                <span
                  key={`${key}-${tag.Value}-${tag.Name}-${index}`}
                  className={`${styles.option} ${normalizeTagValue(params[key]) === normalizeTagValue(tag.Value) ? styles.active : ""}`}
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
        <FilmList list={safeList} className={styles.classifyGrid} />
      </div>

      {safeList.length > 0 && (
        <div className={styles.paginationWrapper}>
          <Pagination
            current={parseInt(currentParams.current || "1", 10)}
            total={page.total}
            pageSize={page.pageSize || 20}
            onChange={handlePageChange}
            showSizeChanger={false}
            hideOnSinglePage
          />
        </div>
      )}
    </div>
  );
}
