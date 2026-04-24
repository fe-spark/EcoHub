"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "antd";
import {
  VideoCameraOutlined,
  PlaySquareOutlined,
  SmileOutlined,
  RocketOutlined,
  FireOutlined,
} from "@ant-design/icons";
import { Autoplay, Pagination, A11y } from "swiper/modules";
import { Swiper, SwiperSlide } from "swiper/react";
import type { Swiper as SwiperInstance } from "swiper/types";
import FilmList from "@/components/public/FilmList";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import "swiper/css";
import "swiper/css/pagination";
import styles from "./index.module.less";

interface BannerItem {
  id: string;
  mid: string;
  name: string;
  poster?: string;
  picture: string;
  pictureSlide?: string;
  year: string;
  cName: string;
}

function buildHeroMetaItems(item: BannerItem): string[] {
  const metaItems: string[] = [];

  if (item.year && item.year !== "0") {
    metaItems.push(item.year);
  }
  if (item.cName) {
    metaItems.push(item.cName);
  }

  return metaItems;
}

function getBannerBackdropImage(item: BannerItem): string {
  return item.pictureSlide || item.picture || item.poster || "";
}

function getBannerPosterImage(item: BannerItem): string {
  return item.poster || item.picture || item.pictureSlide || "";
}

function getCircularOffset(total: number, activeIndex: number, targetIndex: number): number {
  let offset = targetIndex - activeIndex;
  if (total <= 1) {
    return offset;
  }

  const wrappedForward = offset + total;
  const wrappedBackward = offset - total;

  if (Math.abs(wrappedForward) < Math.abs(offset)) {
    offset = wrappedForward;
  }
  if (Math.abs(wrappedBackward) < Math.abs(offset)) {
    offset = wrappedBackward;
  }

  return offset;
}

interface NavChildItem {
  id: string;
  pid: string;
  name: string;
}

interface NavItem {
  id: string;
  name: string;
  show: boolean;
  children: NavChildItem[];
}

interface ContentSection {
  nav: NavItem;
  movies: any[];
  hot: any[];
}

export default function HomePageView({
  data,
}: {
  data: {
    banners: BannerItem[];
    content: ContentSection[];
  };
}) {
  const router = useRouter();
  const featuredCovers = data.banners;
  const [activeIndex, setActiveIndex] = useState(0);
  const [heroSwiper, setHeroSwiper] = useState<SwiperInstance | null>(null);
  const safeActiveIndex =
    featuredCovers.length === 0 ? 0 : Math.min(activeIndex, featuredCovers.length - 1);

  const activeCover = featuredCovers[safeActiveIndex] || featuredCovers[0];
  const activeMetaItems = activeCover ? buildHeroMetaItems(activeCover) : [];

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

  return (
    <div className={styles.container}>
      {featuredCovers.length > 0 && activeCover && (
        <section className={styles.heroSection}>
          <div className={styles.heroBackground}>
            <div
              className={styles.heroBackdropImage}
              style={{ backgroundImage: `url(${getBannerBackdropImage(activeCover)})` }}
            />
            <div className={styles.heroBackdropMask} />
          </div>

          <div className={styles.heroLayout}>
            <div className={styles.heroContent}>
              <div className={styles.heroCopyBlock}>
                <div className={styles.heroEyebrow}>Featured Spotlight</div>

                <div className={styles.heroBadgeRow}>
                  <div className={styles.heroBadge}>{activeCover.cName || "精彩推荐"}</div>
                  {featuredCovers.length > 1 && (
                    <div className={styles.heroCounter}>
                      <span>{String(safeActiveIndex + 1).padStart(2, "0")}</span>
                      <span className={styles.heroCounterDivider}>/</span>
                      <span>{String(featuredCovers.length).padStart(2, "0")}</span>
                    </div>
                  )}
                </div>

                <h1 className={styles.heroTitle}>{activeCover.name}</h1>

                <div className={styles.heroMeta}>
                  {activeMetaItems.map((meta) => (
                    <span key={meta} className={styles.heroMetaItem}>
                      {meta}
                    </span>
                  ))}
                </div>

                <p className={styles.heroDescription}>
                  当前主推影片与同组推荐内容集中展示。
                </p>

                <div className={styles.heroActions}>
                  <Button
                    type="primary"
                    size="large"
                    icon={<PlaySquareOutlined />}
                    className={styles.playBtn}
                    onClick={() =>
                      router.push(
                        resolvePlayEntryPath(activeCover.mid, {
                          sourceId: "0",
                          episodeIndex: 0,
                        }),
                      )
                    }
                  >
                    立即播放
                  </Button>
                </div>
              </div>
            </div>

            <div className={styles.heroCarouselColumn}>
              <div className={styles.heroStage}>
                <Swiper
                  modules={[Autoplay, Pagination, A11y]}
                  className={styles.heroSwiper}
                  slidesPerView={1}
                  loop={featuredCovers.length > 1}
                  speed={650}
                  onSwiper={setHeroSwiper}
                  autoplay={
                    featuredCovers.length > 1
                      ? {
                          delay: 3600,
                          disableOnInteraction: false,
                          pauseOnMouseEnter: true,
                      }
                    : false
                  }
                  pagination={{
                    clickable: true,
                    bulletClass: styles.heroPaginationBullet,
                    bulletActiveClass: styles.heroPaginationBulletActive,
                  }}
                  onSlideChange={(swiper) => {
                    setActiveIndex(swiper.realIndex);
                  }}
                >
                  {featuredCovers.map((_, stageIndex) => {
                    return (
                      <SwiperSlide key={`${featuredCovers[stageIndex].id}-${stageIndex}`} className={styles.heroSwiperSlide}>
                        <div className={styles.heroRingScene}>
                          <div
                            className={styles.heroRingTrack}
                            style={{
                              ["--hero-active-angle" as string]: `${stageIndex * (360 / featuredCovers.length)}deg`,
                            }}
                          >
                            {featuredCovers.map((item, ringIndex) => {
                              const offset = getCircularOffset(featuredCovers.length, stageIndex, ringIndex);
                              const distance = Math.abs(offset);
                              const angle = (360 / featuredCovers.length) * ringIndex;
                              const scale = distance === 0 ? 1 : distance === 1 ? 0.92 : distance === 2 ? 0.82 : 0.72;
                              const opacity = distance === 0 ? 1 : distance === 1 ? 0.76 : distance === 2 ? 0.46 : 0.22;
                              const zIndex = 10 - distance;
                              const posterImage = getBannerPosterImage(item);
                              const isCurrent = distance === 0;

                              return (
                                <button
                                  key={`${item.id}-${ringIndex}`}
                                  type="button"
                                  className={`${styles.heroRingPoster} ${isCurrent ? styles.heroRingPosterActive : ""}`}
                                  style={{
                                    ["--hero-wall-angle" as string]: `${angle}deg`,
                                    ["--hero-wall-scale" as string]: String(scale),
                                    opacity,
                                    zIndex,
                                  }}
                                  onClick={() => heroSwiper?.slideToLoop(ringIndex)}
                                  aria-label={isCurrent ? `${item.name}，当前展示` : `切换到${item.name}`}
                                >
                                  <span className={styles.heroRingPosterFrame}>
                                    <span
                                      className={styles.heroPosterImage}
                                      style={{ backgroundImage: `url(${posterImage})` }}
                                    />
                                  </span>
                                </button>
                              );
                            })}
                          </div>
                        </div>
                      </SwiperSlide>
                    );
                  })}
                </Swiper>
              </div>
            </div>
          </div>
        </section>
      )}

      {data.content.map((section, idx) => {
        if (!section.nav.show) {
          return null;
        }

        return (
          <section key={idx} className={styles.section}>
            <div className={styles.sectionHeader}>
              <div className={styles.left}>
                {getSectionIcon(section.nav.name)}
                <a
                  onClick={() => router.push(`/filmClassify?Pid=${section.nav.id}`)}
                  style={{ cursor: "pointer" }}
                >
                  {section.nav.name}
                </a>
              </div>
              <div className={styles.nav}>
                {section.nav.children?.slice(0, 6).map((child, childIndex) => (
                  <a
                    key={childIndex}
                    onClick={() =>
                      router.push(
                        `/filmClassifySearch?Pid=${child.pid}&Category=${child.id}`,
                      )
                    }
                    style={{ cursor: "pointer" }}
                  >
                    {child.name}
                  </a>
                ))}
                <a
                  className={styles.more}
                  onClick={() => router.push(`/filmClassify?Pid=${section.nav.id}`)}
                  style={{ cursor: "pointer" }}
                >
                  更多 &gt;
                </a>
              </div>
            </div>

            <div className={styles.sectionBody}>
              <div className={styles.filmGrid}>
                <FilmList
                  list={section.movies.slice(0, 12)}
                  className={styles.homeList}
                  col={6}
                />
              </div>

              <div className={styles.sideList}>
                <div className={styles.sideTitle}>
                  <FireOutlined style={{ color: "#ff4d4f" }} />
                  热播{section.nav.name}
                </div>
                {section.hot.slice(0, 12).map((movie, movieIndex) => (
                  <div
                    key={movieIndex}
                    className={styles.hotItem}
                    onClick={() => router.push(resolvePlayEntryPath(movie.mid))}
                  >
                    <span className={styles.rank}>{movieIndex + 1}.</span>
                    <span className={styles.name}>{movie.name}</span>
                  </div>
                ))}
              </div>
            </div>
          </section>
        );
      })}
    </div>
  );
}
