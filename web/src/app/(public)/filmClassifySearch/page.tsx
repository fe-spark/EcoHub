"use client";

import React, { useState, useEffect, useCallback, Suspense } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Pagination } from "antd";
import { ApiGet } from "@/lib/api";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";

function ClassifySearchContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { message } = useAppMessage();

  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  const fetchResults = useCallback(async () => {
    setLoading(true);
    const params: any = {};
    searchParams.forEach((value, key) => {
      params[key] = value;
    });
    try {
      const resp = await ApiGet("/filmClassifySearch", params);
      if (resp.code === 0) {
        setData(resp.data);
      } else {
        message.error("影片搜索结果异常, 请稍后刷新重试");
      }
    } finally {
      setLoading(false);
    }
  }, [searchParams, message]);

  useEffect(() => {
    void fetchResults();
  }, [fetchResults]);

  const handleTagClick = (key: string, value: string) => {
    const currentParams = new URLSearchParams(searchParams.toString());
    currentParams.set(key, value);
    currentParams.set("current", "1"); // Reset to page 1 on filter change
    router.push(`/filmClassifySearch?${currentParams.toString()}`);
  };

  const handlePageChange = (page: number) => {
    const currentParams = new URLSearchParams(searchParams.toString());
    currentParams.set("current", page.toString());
    router.push(`/filmClassifySearch?${currentParams.toString()}`);
  };

  if (loading && !data) {
    return <AppLoading text="" padding="88px 0" />;
  }

  if (!data) return null;

  const { title, list, search, params, page } = data;
  const safeList = Array.isArray(list) ? list : [];

  return (
    <div className={styles.container}>
      {/* Search Header / Stats */}
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
              {search.tags[key].map((tag: any) => (
                <span
                  key={tag.Value}
                  className={`${styles.option} ${params[key] === tag.Value ? styles.active : ""}`}
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
        <FilmList
          list={safeList}
          loading={loading}
          className={styles.classifyGrid}
        />
      </div>

      {safeList.length > 0 && (
        <div className={styles.paginationWrapper}>
          <Pagination
            current={parseInt(searchParams.get("current") || "1")}
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

export default function FilmClassifySearchPage() {
  return (
    <Suspense fallback={null}>
      <ClassifySearchContent />
    </Suspense>
  );
}
