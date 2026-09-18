"use client";

import { Button, Pagination } from "antd";
import { CaretRightOutlined, VideoCameraOutlined } from "@ant-design/icons";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import FilmList from "@/components/public/FilmList";
import HighlightMatchedText from "@/components/public/HighlightMatchedText";
import AppLoading from "@/components/public/Loading";
import styles from "./index.module.less";

function normalizeMetaValue(value?: string | number | null) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") {
    return "";
  }
  return text;
}

function getPrimaryPlotTag(classTag?: string) {
  return (
    normalizeMetaValue(classTag)
      .split(/[,，/|、\s]+/)
      .map((tag) => tag.trim())
      .find(Boolean) || ""
  );
}

export default function SearchResultPanel({
  keyword,
  current,
  list,
  page,
  totalCount,
  listLoading,
  hasResults,
  viewMode,
  onPlay,
  onPageChange,
}: {
  keyword: string;
  current: string;
  list: any[];
  page: any;
  totalCount: number;
  listLoading: boolean;
  hasResults: boolean;
  viewMode: "grid" | "detail";
  onPlay: (movie: { id?: string | number; sourceId?: string; sourceMid?: string | number }) => void;
  onPageChange: (page: number) => void;
}) {
  if (listLoading) {
    return (
      <div className={styles.listLoading} role="status" aria-live="polite">
        <AppLoading text="正在搜索该采集源" padding="64px 0" size="default" showHints={false} />
      </div>
    );
  }

  if (!hasResults) {
    return (
      <section className={styles.emptyContainer}>
        <div className={styles.emptyHeader}>
          <div className={styles.emptyIconCircle}>
            <VideoCameraOutlined />
          </div>
          <h2 className={styles.emptyTitle}>
            {keyword ? (
              <>未找到与 &ldquo;<span className={styles.keywordHighlight}>{keyword}</span>&rdquo; 相关的影视</>
            ) : (
              "探索全网热门影视"
            )}
          </h2>
          <p className={styles.emptyDesc}>
            建议缩短或更换搜索词，也可以直接尝试上方的热门搜索推荐
          </p>
        </div>
      </section>
    );
  }

  return (
    <section className={styles.searchRes} aria-label="搜索结果列表">
      {viewMode === "grid" ? (
        <div className={styles.gridContainer}>
          <FilmList list={list} col={6} highlightQuery={keyword} />
        </div>
      ) : (
        <div className={styles.resultList}>
          {list.map((movie: any) => (
            <article key={`${movie.sourceId || ""}:${movie.id || 0}:${movie.sourceMid || 0}`} className={styles.searchItem}>
              <div
                className={styles.posterWrapper}
                onClick={() => onPlay(movie)}
              >
                {/* eslint-disable-next-line @next/next/no-img-element */}
                <img
                  src={movie.picture || FALLBACK_IMG}
                  className={styles.poster}
                  alt={movie.name}
                  referrerPolicy="no-referrer"
                  loading="lazy"
                />
                {movie.remarks && (
                  <span className={styles.posterRemark}>{movie.remarks}</span>
                )}
              </div>

              <div className={styles.intro}>
                <h3
                  className={styles.filmName}
                  onClick={() => onPlay(movie)}
                >
                  <HighlightMatchedText
                    text={movie.name}
                    query={keyword}
                    className={styles.highlightMatched}
                  />
                </h3>

                <div className={styles.tags}>
                  {movie.cName && (
                    <span className={`${styles.tag} ${styles.category}`}>
                      {movie.cName}
                    </span>
                  )}
                  {normalizeMetaValue(movie.year) && (
                    <span className={styles.tag}>
                      {normalizeMetaValue(movie.year)}
                    </span>
                  )}
                  {normalizeMetaValue(movie.area) && (
                    <span className={styles.tag}>
                      {normalizeMetaValue(movie.area)}
                    </span>
                  )}
                  {normalizeMetaValue(movie.language) && (
                    <span className={styles.tag}>
                      {normalizeMetaValue(movie.language)}
                    </span>
                  )}
                  {getPrimaryPlotTag(movie.classTag) && (
                    <span className={styles.tag}>
                      {getPrimaryPlotTag(movie.classTag)}
                    </span>
                  )}
                </div>

                <div className={styles.metaRow}>
                  <span className={styles.metaLabel}>导演</span>
                  <span className={styles.metaValue}>
                    {movie.director ? (
                      <HighlightMatchedText
                        text={movie.director}
                        query={keyword}
                        className={styles.highlightMatched}
                      />
                    ) : (
                      "暂无导演信息"
                    )}
                  </span>
                </div>

                <div className={styles.metaRow}>
                  <span className={styles.metaLabel}>主演</span>
                  <span className={styles.metaValue}>
                    {movie.actor ? (
                      <HighlightMatchedText
                        text={movie.actor}
                        query={keyword}
                        className={styles.highlightMatched}
                      />
                    ) : (
                      "暂无主演信息"
                    )}
                  </span>
                </div>

                <p className={styles.blurb}>
                  {movie.blurb?.replace(/[\s　]+/g, " ").trim() ||
                    "暂无剧情简介，点击立即进入播放页体验高清流畅观影。"}
                </p>

                <div className={styles.actionRow}>
                  <Button
                    type="primary"
                    icon={<CaretRightOutlined />}
                    className={styles.playBtn}
                    onClick={() => onPlay(movie)}
                  >
                    立即播放
                  </Button>
                </div>
              </div>
            </article>
          ))}
        </div>
      )}

      <div className={styles.pagination}>
        <Pagination
          current={Number(page?.current) || parseInt(current || "1", 10)}
          total={page?.total ?? totalCount}
          pageSize={page?.pageSize || 12}
          onChange={onPageChange}
          showSizeChanger={false}
          hideOnSinglePage
        />
      </div>
    </section>
  );
}
