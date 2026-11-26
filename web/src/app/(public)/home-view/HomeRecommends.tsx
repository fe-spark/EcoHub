"use client";

import { useState, useEffect } from "react";
import { FireOutlined, VideoCameraOutlined } from "@ant-design/icons";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import styles from "./HotCharts.module.less";

export interface RecommendPublicItem {
  id: number | string;
  name: string;
  picture?: string;
  year?: string;
  area?: string;
  cName?: string;
  remarks?: string;
  doubanScore?: string;
  rank: number;
}

export interface RecommendSectionNavChild {
  id: string;
  pid: string;
  name: string;
}

export interface RecommendSectionNav {
  id: string;
  name: string;
  show: boolean;
  children?: RecommendSectionNavChild[];
}

export interface RecommendSection {
  id: string;
  kind?: "recommend" | "category";
  title: string;
  autoSync?: boolean;
  syncChartKey?: string;
  categoryId?: number;
  nav?: RecommendSectionNav;
  items: RecommendPublicItem[];
  hot?: Array<{
    id: string | number;
    name: string;
    picture?: string;
    year?: string;
    cName?: string;
    area?: string;
    remarks?: string;
  }>;
}

function RecommendPoster({ src, alt }: { src?: string; alt: string }) {
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    setFailed(false);
  }, [src]);

  if (!src || failed) {
    return (
      <div className={styles.posterFallback}>
        <VideoCameraOutlined style={{ fontSize: 22 }} />
        <span>{alt}</span>
      </div>
    );
  }

  return (
    /* 封面来自采集源动态地址，保持原生 img */
    /* eslint-disable-next-line @next/next/no-img-element */
    <img
      src={src}
      alt={alt}
      loading="lazy"
      className={styles.posterImg}
      onError={() => setFailed(true)}
    />
  );
}

export function RecommendSectionRow({ section }: { section: RecommendSection }) {
  const { navigate } = useContentNavigate();

  if (!section.items || section.items.length === 0) {
    return null;
  }

  return (
    <section
      key={section.id}
      className={styles.section}
      aria-label={section.title}
    >
      <div className={styles.header}>
        <div className={styles.titleGroup}>
          <FireOutlined className={styles.icon} />
          <h2 className={styles.title}>{section.title}</h2>
          {section.syncChartKey ? (
            <span className={styles.subTag}>热门榜单</span>
          ) : (
            <span className={styles.subTag}>精选推荐</span>
          )}
        </div>
      </div>

      <div className={styles.grid}>
        {section.items.slice(0, 12).map((item) => {
          const metaParts = [item.year, item.area, item.cName]
            .map((v) => String(v || "").trim())
            .filter(Boolean);
          return (
            <div
              key={`${section.id}-${item.id}`}
              className={styles.card}
              onClick={() =>
                navigate(
                  resolvePlayEntryPath(String(item.id)),
                  "进入播放页...",
                )
              }
            >
              <div className={styles.posterWrap}>
                <RecommendPoster src={item.picture} alt={item.name} />
                <span
                  className={`${styles.rankBadge} ${
                    item.rank <= 3 ? styles.rankTop : ""
                  }`}
                >
                  {item.rank}
                </span>
                {item.doubanScore ? (
                  <span className={styles.scoreBadge}>
                    ★ {item.doubanScore}
                  </span>
                ) : null}
                {item.remarks ? (
                  <div className={styles.remarksBar}>{item.remarks}</div>
                ) : null}
              </div>
              <div className={styles.cardTitle} title={item.name}>
                {item.name}
              </div>
              <div className={styles.cardMeta}>
                {metaParts.slice(0, 2).join(" · ") || "精选资源"}
              </div>
            </div>
          );
        })}
      </div>
    </section>
  );
}
