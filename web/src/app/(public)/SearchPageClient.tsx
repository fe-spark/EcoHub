"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button, Pagination, Empty } from "antd";
import { SearchOutlined, CaretRightOutlined } from "@ant-design/icons";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import styles from "./page.module.less";

export default function SearchPageClient({
  data,
  keyword,
  current,
}: {
  data: any;
  keyword: string;
  current: string;
}) {
  const router = useRouter();
  const { message } = useAppMessage();
  const [searchKeyword, setSearchKeyword] = useState(keyword);

  const handleSearch = () => {
    if (!searchKeyword.trim()) {
      message.error("搜索信息不能为空");
      return;
    }

    router.push(`/search?search=${encodeURIComponent(searchKeyword)}&current=1`);
  };

  const handlePageChange = (page: number) => {
    router.push(`/search?search=${encodeURIComponent(keyword)}&current=${page}`);
  };

  const handlePlay = (id: string) => {
    router.push(`/play?id=${id}&source=0&episode=0`);
  };

  return (
    <div className={styles.container}>
      <div className={styles.searchGroup}>
        <input
          placeholder="输入关键字搜索 动漫, 剧集, 电影"
          value={searchKeyword}
          onChange={(event) => setSearchKeyword(event.target.value)}
          onKeyDown={(event) => event.key === "Enter" && handleSearch()}
        />
        <Button icon={<SearchOutlined />} onClick={handleSearch} />
      </div>

      {data?.list?.length > 0 ? (
        <div className={styles.searchRes}>
          <div className={styles.resultHeader}>
            <h2>{keyword}</h2>
            <p>
              共找到 {data.page.total} 部与 &quot;{keyword}&quot; 相关的影视作品
            </p>
          </div>

          <div className={styles.resultList}>
            {data.list.map((movie: any) => (
              <div key={movie.id} className={styles.searchItem}>
                {/* 搜索结果封面为动态远程地址，这里保持原生 img，避免额外远程图片配置 */}
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={movie.picture || FALLBACK_IMG}
                  className={styles.poster}
                  alt={movie.name}
                  onClick={() => router.push(`/filmDetail?link=${movie.id}`)}
                  style={{ cursor: "pointer" }}
                />
                <div className={styles.intro}>
                  <h3
                    onClick={() => router.push(`/filmDetail?link=${movie.id}`)}
                    style={{ cursor: "pointer" }}
                  >
                    {movie.name}
                  </h3>
                  <div className={styles.tags}>
                    {movie.cName && (
                      <span className={`${styles.tag} ${styles.category}`}>
                        {movie.cName}
                      </span>
                    )}
                    {movie.year && <span className={styles.tag}>{movie.year}</span>}
                    {movie.area && <span className={styles.tag}>{movie.area}</span>}
                  </div>
                  <div className={styles.meta}>
                    <span>导演:</span> {movie.director || "未知"}
                  </div>
                  <div className={styles.meta}>
                    <span>主演:</span> {movie.actor || "未知"}
                  </div>
                  <div className={styles.blurb}>
                    <span>剧情:</span>{" "}
                    {movie.blurb?.replace(/　　/g, "") || "暂无简介"}
                  </div>
                  <div className={styles.action}>
                    <Button
                      type="primary"
                      shape="round"
                      icon={<CaretRightOutlined />}
                      onClick={() => handlePlay(movie.id)}
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
              current={parseInt(current, 10)}
              total={data.page.total}
              pageSize={data.page.pageSize || 20}
              onChange={handlePageChange}
              showSizeChanger={false}
              hideOnSinglePage
            />
          </div>
        </div>
      ) : keyword ? (
        <Empty description="未查询到对应影片" />
      ) : null}
    </div>
  );
}
