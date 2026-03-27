"use client";

import React, { useState, useEffect } from "react";
import { useRouter } from "next/navigation";
import { Carousel, Button } from "antd";
import {
  VideoCameraOutlined,
  PlaySquareOutlined,
  SmileOutlined,
  RocketOutlined,
  FireOutlined,
  LeftOutlined,
  RightOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/api";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";

interface BannerItem {
  id: string;
  mid: string;
  name: string;
  poster: string;
  picture: string;
  year: string;
  cName: string;
}

interface NavItem {
  id: string;
  name: string;
  show: boolean;
  children: any[];
}

interface ContentSection {
  nav: NavItem;
  movies: any[];
  hot: any[];
}

// 自定义轮播箭头组件，避免 currentSlide/slideCount 透传到 DOM 报错

function NextArrow({ currentSlide: _cs, slideCount: _sc, ...props }: any) {
  return (
    <div {...props}>
      <RightOutlined />
    </div>
  );
}

function PrevArrow({ currentSlide: _cs, slideCount: _sc, ...props }: any) {
  return (
    <div {...props}>
      <LeftOutlined />
    </div>
  );
}

export default function HomePage() {
  const [data, setData] = useState<{
    banners: BannerItem[];
    content: ContentSection[];
  }>({
    banners: [],
    content: [],
  });
  const [loading, setLoading] = useState(true);
  const router = useRouter();
  const { message } = useAppMessage();

  useEffect(() => {
    ApiGet("/index")
      .then((resp) => {
        if (resp.code === 0) {
          setData(resp.data);
        } else {
          message.error(resp.msg);
        }
      })
      .finally(() => setLoading(false));
  }, [message]);

  const getSectionIcon = (name: string) => {
    if (name.includes("电影"))
      return <VideoCameraOutlined className={styles.icon} />;
    if (name.includes("剧"))
      return <PlaySquareOutlined className={styles.icon} />;
    if (name.includes("动漫")) return <SmileOutlined className={styles.icon} />;
    return <RocketOutlined className={styles.icon} />;
  };

  if (loading) {
    return <AppLoading text="" padding="88px 0" />;
  }

  return (
    <div className={styles.container}>
      {/* Hero Banner - Immersive Refactored */}
      {data.banners.length > 0 && (
        <section className={styles.heroSection}>
          <Carousel
            autoplay
            autoplaySpeed={5000}
            effect="fade"
            dots={{ className: styles.customDots }}
            arrows
            nextArrow={<NextArrow className={styles.customArrow} />}
            prevArrow={<PrevArrow className={styles.customArrow} />}
          >
            {data.banners.map((item, idx) => (
              <div
                key={idx}
                className={styles.heroSlide}
                onClick={() => router.push(`/filmDetail?link=${item.mid}`)}
              >
                {/* Background Layer */}
                <div
                  className={styles.heroBg}
                  style={{
                    backgroundImage: `url(${item.poster || item.picture})`,
                  }}
                />

                {/* Content Layer */}
                <div className={styles.heroOverlay}>
                  <div className={styles.heroInfo}>
                    <div className={styles.heroBadge}>
                      {item.cName || "精彩推荐"}
                    </div>
                    <h1 className={styles.heroTitle}>{item.name}</h1>
                    <div className={styles.heroMeta}>
                      <span>{item.year}</span>
                      <span className={styles.divider}>|</span>
                      <span>HD 高清</span>
                    </div>
                    <div className={styles.heroActions}>
                      <Button
                        type="primary"
                        size="large"
                        icon={<PlaySquareOutlined />}
                        className={styles.playBtn}
                        onClick={() =>
                          router.push(`/filmDetail?link=${item.mid}`)
                        }
                      >
                        立即播放
                      </Button>
                      <Button
                        ghost
                        size="large"
                        className={styles.detailBtn}
                        onClick={() =>
                          router.push(`/filmDetail?link=${item.mid}`)
                        }
                      >
                        查看详情
                      </Button>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </Carousel>
        </section>
      )}

      {/* Content Sections */}
      {data.content.map((section, idx) => {
        if (!section.nav.show) return null;

        return (
          <section key={idx} className={styles.section}>
            <div className={styles.sectionHeader}>
              <div className={styles.left}>
                {getSectionIcon(section.nav.name)}
                <a
                  onClick={() =>
                    router.push(`/filmClassify?Pid=${section.nav.id}`)
                  }
                  style={{ cursor: "pointer" }}
                >
                  {section.nav.name}
                </a>
              </div>
              <div className={styles.nav}>
                {section.nav.children?.slice(0, 6).map((c, cIdx) => (
                  <a
                    key={cIdx}
                    onClick={() =>
                      router.push(
                        `/filmClassifySearch?Pid=${c.pid}&Category=${c.id}`,
                      )
                    }
                    style={{ cursor: "pointer" }}
                  >
                    {c.name}
                  </a>
                ))}
                <a
                  className={styles.more}
                  onClick={() =>
                    router.push(`/filmClassify?Pid=${section.nav.id}`)
                  }
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

              {/* Sidebar Hot List */}
              <div className={styles.sideList}>
                <div className={styles.sideTitle}>
                  <FireOutlined style={{ color: "#ff4d4f" }} />
                  热播{section.nav.name}
                </div>
                {section.hot.slice(0, 12).map((m, mIdx) => (
                  <div
                    key={mIdx}
                    className={styles.hotItem}
                    onClick={() => router.push(`/filmDetail?link=${m.mid}`)}
                  >
                    <span className={styles.rank}>{mIdx + 1}.</span>
                    <span className={styles.name}>{m.name}</span>
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
