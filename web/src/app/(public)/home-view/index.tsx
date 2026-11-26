"use client";

import {
  VideoCameraOutlined,
  PlaySquareOutlined,
  SmileOutlined,
  RocketOutlined,
  FireOutlined,
} from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import HomeHero, { type HeroBannerItem } from "./HomeHero";
import { RecommendSectionRow, type RecommendSection } from "./HomeRecommends";
import DailyUpdates from "./DailyUpdates";
import EmptyCollectGuide from "@/components/public/EmptyCollectGuide";
import styles from "./index.module.less";

function asHeroBanner(item: HeroBannerItem & { remark?: string }): HeroBannerItem {
  const id = String(item.mid || item.id || "").trim();
  return {
    ...item,
    id: item.id || id,
    mid: item.mid || id,
    remarks: item.remarks || item.remark || "",
  };
}

export default function HomePageView({
  data,
}: {
  data: {
    banners: HeroBannerItem[];
    recommends?: RecommendSection[];
  };
}) {
  const { navigate } = useContentNavigate();
  const heroDeck = (data.banners || []).map(asHeroBanner);
  const sections = Array.isArray(data.recommends) ? data.recommends : [];

  // 与 Hero 一致：走布局级 navigate，展示整页内容 loading
  const goPlay = (id: string) => {
    navigate(resolvePlayEntryPath(id), "进入播放页...");
  };

  const getSectionIcon = (name: string) => {
    if (name.includes("电影")) {
      return <VideoCameraOutlined className={styles.icon} />;
    }
    if (name.includes("剧")) {
      return <PlaySquareOutlined className={styles.icon} />;
    }
    if (name.includes("动漫")) {
      return <SmileOutlined className={styles.icon} />;
    }
    return <RocketOutlined className={styles.icon} />;
  };

  const hasSections = sections.some(
    (sec) =>
      (Array.isArray(sec.items) && sec.items.length > 0) ||
      (Array.isArray(sec.hot) && sec.hot.length > 0),
  );
  const isEmpty = heroDeck.length === 0 && !hasSections;

  if (isEmpty) {
    return <EmptyCollectGuide />;
  }

  return (
    <div className={styles.container}>
      <HomeHero banners={heroDeck} />

      <DailyUpdates />

      {sections.map((section) => {
        if (section.kind !== "category") {
          return <RecommendSectionRow key={section.id} section={section} />;
        }
        const nav = section.nav;
        if (!nav || !nav.show) {
          return null;
        }
        const movies = section.items || [];
        const hot = section.hot || [];
        const displayTitle = section.title || nav.name;

        return (
          <section key={section.id} className={styles.section}>
            <div className={styles.sectionHeader}>
              <div className={styles.left}>
                {getSectionIcon(nav.name || displayTitle)}
                <a
                  onClick={() => {
                    navigate(`/category?pid=${nav.id}`, "分类加载中");
                  }}
                  style={{ cursor: "pointer" }}
                >
                  {displayTitle}
                </a>
              </div>
              <div className={styles.nav}>
                {nav.children?.slice(0, 6).map((child) => (
                  <a
                    key={child.id}
                    onClick={() => {
                      navigate(
                        `/category/filter?pid=${child.pid}&category=${child.id}`,
                        "分类加载中",
                      );
                    }}
                    style={{ cursor: "pointer" }}
                  >
                    {child.name}
                  </a>
                ))}
                <a
                  className={styles.more}
                  onClick={() => {
                    navigate(`/category?pid=${nav.id}`, "分类加载中");
                  }}
                  style={{ cursor: "pointer" }}
                >
                  更多 &gt;
                </a>
              </div>
            </div>

            <div className={styles.sectionBody}>
              <div className={styles.filmGrid}>
                <FilmList
                  list={movies.slice(0, 12) as any}
                  className={styles.homeList}
                  col={6}
                />
              </div>

              <div className={styles.sideList}>
                <div className={styles.sideTitle}>
                  <FireOutlined style={{ color: "#ff4d4f" }} />
                  热播{nav.name || displayTitle}
                </div>
                {hot.length > 0 ? (
                  hot.slice(0, 12).map((movie, movieIndex) => (
                    <div
                      key={`${section.id}-hot-${movie.id}`}
                      className={styles.hotItem}
                      onClick={() => goPlay(String(movie.id))}
                    >
                      <span className={styles.rank}>{movieIndex + 1}.</span>
                      <span className={styles.name}>{movie.name}</span>
                    </div>
                  ))
                ) : (
                  <div
                    style={{
                      color: "var(--public-text-3, rgba(255, 255, 255, 0.45))",
                      fontSize: 13,
                      padding: "24px 0",
                      textAlign: "center",
                    }}
                  >
                    暂无热播影片
                  </div>
                )}
              </div>
            </div>
          </section>
        );
      })}
    </div>
  );
}
