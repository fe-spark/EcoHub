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
import { Autoplay, EffectCards, Pagination } from "swiper/modules";
import { Swiper, SwiperSlide } from "swiper/react";
import FilmList from "@/components/public/FilmList";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import "swiper/css";
import "swiper/css/effect-cards";
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

  const navigateToPlay = (mid: string) => {
    router.push(
      resolvePlayEntryPath(mid, {
        sourceId: "0",
        episodeIndex: 0,
      }),
    );
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

          <div className={styles.heroShell}>
            <div className={styles.heroInfo}>
              <div className={styles.heroEyebrow}>Cinema Orbit</div>

              <div className={styles.heroHeadlineRow}>
                <span className={styles.heroBadge}>{activeCover.cName || "本周主推"}</span>
                {featuredCovers.length > 1 && (
                  <span className={styles.heroCounter}>
                    {String(safeActiveIndex + 1).padStart(2, "0")}
                    <span className={styles.heroCounterDivider}>/</span>
                    {String(featuredCovers.length).padStart(2, "0")}
                  </span>
                )}
              </div>

              <h1 className={styles.heroTitle}>{activeCover.name}</h1>

              <p className={styles.heroDescription}>
                以当前主推影片为圆心，周围环绕同组竖屏海报，直接点击任意卡片即可切换焦点。
              </p>

              <div className={styles.heroMeta}>
                {activeMetaItems.map((meta) => (
                  <span key={meta} className={styles.heroMetaItem}>
                    {meta}
                  </span>
                ))}
              </div>

              <div className={styles.heroActions}>
                <Button
                  type="primary"
                  size="large"
                  icon={<PlaySquareOutlined />}
                  className={styles.playBtn}
                  onClick={() => navigateToPlay(activeCover.mid)}
                >
                  立即播放
                </Button>
              </div>
            </div>

            <div className={styles.heroVisual}>
              <div className={styles.heroScene}>
                <div className={styles.heroGlow} />
                <Swiper
                  modules={[Autoplay, EffectCards, Pagination]}
                  className={styles.heroSwiper}
                  grabCursor={featuredCovers.length > 1}
                  allowTouchMove={featuredCovers.length > 1}
                  autoplay={
                    featuredCovers.length > 1
                      ? {
                          delay: 5000,
                          disableOnInteraction: false,
                        }
                      : false
                  }
                  effect="cards"
                  cardsEffect={{
                    perSlideOffset: 12,
                    perSlideRotate: 2,
                    rotate: true,
                    slideShadows: true,
                  }}
                  speed={560}
                  resistanceRatio={0.72}
                  watchSlidesProgress
                  onSlideChange={(swiper) => setActiveIndex(swiper.realIndex)}
                  pagination={{
                    clickable: true,
                    bulletClass: styles.heroPaginationBullet,
                    bulletActiveClass: styles.heroPaginationBulletActive,
                  }}
                >
                  {featuredCovers.map((item, index) => {
                    const posterImage = getBannerPosterImage(item);

                    return (
                      <SwiperSlide key={`${item.id}-${index}`} className={styles.heroSlide}>
                        <div
                          role="button"
                          tabIndex={0}
                          className={styles.heroPoster}
                          onClick={() => navigateToPlay(item.mid)}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              navigateToPlay(item.mid);
                            }
                          }}
                          aria-label={`进入${item.name}播放页`}
                        >
                          <span className={styles.heroPosterFrame}>
                            <span
                              className={styles.heroPosterImage}
                              style={{ backgroundImage: `url(${posterImage})` }}
                            />
                            <span className={styles.heroPosterOverlay} />
                            <span className={styles.heroPosterCopy}>
                              <span className={styles.heroFocusTag}>{item.cName || "推荐"}</span>
                              <strong className={styles.heroFocusTitle}>{item.name}</strong>
                            </span>
                          </span>
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
