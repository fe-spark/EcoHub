"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "antd";
import {
  VideoCameraOutlined,
  PlaySquareOutlined,
  SmileOutlined,
  RocketOutlined,
  FireOutlined,
} from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";

interface BannerItem {
  id: string;
  mid: string;
  name: string;
  poster: string;
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

function getBannerLandscapeImage(item: BannerItem): string {
  return item.pictureSlide || item.poster || item.picture;
}

function getBannerPortraitImage(item: BannerItem): string {
  return item.picture || item.poster || item.pictureSlide || "";
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
  const banners = data.banners;
  const [activeIndex, setActiveIndex] = useState(0);
  const safeActiveIndex =
    banners.length === 0 ? 0 : Math.min(activeIndex, banners.length - 1);

  const activeBanner = banners[safeActiveIndex] || banners[0];
  const activeMetaItems = activeBanner ? buildHeroMetaItems(activeBanner) : [];

  useEffect(() => {
    if (banners.length <= 1) {
      return;
    }

    const timer = window.setTimeout(() => {
      setActiveIndex((currentIndex) => (currentIndex + 1) % banners.length);
    }, 3600);

    return () => window.clearTimeout(timer);
  }, [activeIndex, banners.length]);

  const handleHeroCardClick = (index: number) => {
    if (index === safeActiveIndex) {
      return;
    }

    setActiveIndex(index);
  };

  const getHeroAccordionItemClassName = (index: number) => {
    if (index === safeActiveIndex) {
      return styles.heroAccordionItemActive;
    }

    if (banners.length <= 1) {
      return styles.heroAccordionItemFar;
    }

    const distance = Math.abs(index - safeActiveIndex);

    return distance === 1 ? styles.heroAccordionItemNear : styles.heroAccordionItemFar;
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

  return (
    <div className={styles.container}>
      {banners.length > 0 && activeBanner && (
        <section className={styles.heroSection}>
          <div className={styles.heroBackground}>
            <div
              className={styles.heroBackdropImage}
              style={{ backgroundImage: `url(${getBannerLandscapeImage(activeBanner)})` }}
            />
            <div className={styles.heroBackdropMask} />
          </div>

          <div className={styles.heroLayout}>
            <div className={styles.heroContent}>
              <div className={styles.heroPanel}>
                <div className={styles.heroEyebrow}>Editor&apos;s Pick</div>

                <div className={styles.heroBadgeRow}>
                  <div className={styles.heroBadge}>{activeBanner.cName || "精彩推荐"}</div>
                  {banners.length > 1 && (
                    <div className={styles.heroCounter}>
                      <span>{String(safeActiveIndex + 1).padStart(2, "0")}</span>
                      <span className={styles.heroCounterDivider}>/</span>
                      <span>{String(banners.length).padStart(2, "0")}</span>
                    </div>
                  )}
                </div>
                
                <h1 className={styles.heroTitle}>{activeBanner.name}</h1>

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
                    onClick={() => router.push(`/play?id=${activeBanner.mid}&source=0&episode=0`)}
                  >
                    立即播放
                  </Button>
                  <Button
                    ghost
                    size="large"
                    className={styles.detailBtn}
                    onClick={() => router.push(`/filmDetail?link=${activeBanner.mid}`)}
                  >
                    查看详情
                  </Button>
                </div>
              </div>
            </div>

            <div className={styles.heroCarouselColumn}>
              <div className={styles.heroAccordion}>
                {banners.map((item, index) => {
                  const isActive = index === safeActiveIndex;

                  return (
                    <button
                      key={item.id}
                      type="button"
                      className={`${styles.heroAccordionItem} ${getHeroAccordionItemClassName(index)}`}
                      onClick={() => handleHeroCardClick(index)}
                      aria-label={`切换到 ${item.name}`}
                      >
                        <span
                          className={styles.heroCardImage}
                          style={{ backgroundImage: `url(${getBannerPortraitImage(item)})` }}
                        />
                      <span className={styles.heroCardMask} />
                      <span className={styles.heroAccordionSpine} />
                      <span className={styles.heroCardInfo}>
                        <span className={styles.heroCardTag}>{item.cName || "推荐"}</span>
                        <span className={styles.heroCardTitle}>{item.name}</span>
                      </span>
                    </button>
                  );
                })}
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
                    onClick={() => router.push(`/filmDetail?link=${movie.mid}`)}
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
