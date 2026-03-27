"use client";

import React, {
  useState,
  useCallback,
  useEffect,
  Suspense,
} from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button, Pagination, Empty } from "antd";
import { SearchOutlined, CaretRightOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/api";
import AppLoading from "@/components/public/Loading";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";

function SearchContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { message } = useAppMessage();
  const keywordParam = searchParams.get("search") || "";
  const currentParam = searchParams.get("current") || "1";

  const [keyword, setKeyword] = useState(keywordParam);
  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(false);
  const [oldSearch, setOldSearch] = useState(keywordParam);

  useEffect(() => {
    setKeyword(keywordParam);
  }, [keywordParam]);

  const fetchResults = useCallback(
    async (k: string, c: string) => {
      if (!k) return;
      setLoading(true);
      try {
        const resp = await ApiGet("/searchFilm", { keyword: k, current: c });
        if (resp.code === 0) {
          setData(resp.data);
          setOldSearch(k);
        } else {
          message.warning(resp.msg);
        }
      } finally {
        setLoading(false);
      }
    },
    [message],
  );

  useEffect(() => {
    void fetchResults(keywordParam, currentParam);
  }, [keywordParam, currentParam, fetchResults]);

  const handleSearch = () => {
    if (!keyword.trim()) {
      message.error("搜索信息不能为空");
      return;
    }
    router.push(`/search?search=${encodeURIComponent(keyword)}&current=1`);
  };

  const handlePageChange = (page: number) => {
    router.push(
      `/search?search=${encodeURIComponent(oldSearch)}&current=${page}`,
    );
  };

  const handlePlay = (id: string) => {
    router.push(`/play?id=${id}&source=0&episode=0`);
  };

  return (
    <div className={styles.container}>
      <div className={styles.searchGroup}>
        <input
          placeholder="输入关键字搜索 动漫, 剧集, 电影"
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleSearch()}
        />
        <Button icon={<SearchOutlined />} onClick={handleSearch} />
      </div>

      {loading ? (
        <AppLoading padding="60px 0" />
      ) : data && data.list?.length > 0 ? (
        <div className={styles.searchRes}>
          <div className={styles.resultHeader}>
            <h2>{oldSearch}</h2>
            <p>
              共找到 {data.page.total} 部与 &quot;{oldSearch}&quot;
              相关的影视作品
            </p>
          </div>

          <div className={styles.resultList}>
            {data.list.map((m: any) => (
              <div key={m.id} className={styles.searchItem}>
                <img
                  src={m.picture || FALLBACK_IMG}
                  className={styles.poster}
                  alt={m.name}
                  onClick={() => router.push(`/filmDetail?link=${m.id}`)}
                  style={{ cursor: "pointer" }}
                />
                <div className={styles.intro}>
                  <h3
                    onClick={() => router.push(`/filmDetail?link=${m.id}`)}
                    style={{ cursor: "pointer" }}
                  >
                    {m.name}
                  </h3>
                  <div className={styles.tags}>
                    {m.cName && (
                      <span className={`${styles.tag} ${styles.category}`}>
                        {m.cName}
                      </span>
                    )}
                    {m.year && <span className={styles.tag}>{m.year}</span>}
                    {m.area && <span className={styles.tag}>{m.area}</span>}
                  </div>
                  <div className={styles.meta}>
                    <span>导演:</span> {m.director || "未知"}
                  </div>
                  <div className={styles.meta}>
                    <span>主演:</span> {m.actor || "未知"}
                  </div>
                  <div className={styles.blurb}>
                    <span>剧情:</span>{" "}
                    {m.blurb?.replace(/　　/g, "") || "暂无简介"}
                  </div>
                  <div className={styles.action}>
                    <Button
                      type="primary"
                      shape="round"
                      icon={<CaretRightOutlined />}
                      onClick={() => handlePlay(m.id)}
                      style={{
                        background: "#fa8c16",
                        borderColor: "#fa8c16",
                        boxShadow: "none",
                      }}
                    >
                      立即播放
                    </Button>
                  </div>
                </div>
              </div>
            ))}
          </div>

          <div className={styles.pagination}>
            <Pagination
              current={parseInt(currentParam)}
              total={data.page.total}
              pageSize={data.page.pageSize || 20}
              onChange={handlePageChange}
              showSizeChanger={false}
              hideOnSinglePage
            />
          </div>
        </div>
      ) : oldSearch ? (
        <Empty description="未查询到对应影片" />
      ) : null}
    </div>
  );
}

export default function SearchPage() {
  return (
    <Suspense fallback={null}>
      <SearchContent />
    </Suspense>
  );
}
