"use client";

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
import FilmList from "@/components/public/FilmList";
import styles from "./page.module.less";

interface BannerItem {
  id: string;
  mid: string;
  name: string;
  poster: string;
  picture: string;
  year: string;
  cName: string;
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

export default function HomePageClient({
  data,
}: {
  data: {
    banners: BannerItem[];
    content: ContentSection[];
  };
}) {
  const router = useRouter();

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
                <div
                  className={styles.heroBg}
                  style={{
                    backgroundImage: `url(${item.poster || item.picture})`,
                  }}
                />

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
                        onClick={() => router.push(`/filmDetail?link=${item.mid}`)}
                      >
                        立即播放
                      </Button>
                      <Button
                        ghost
                        size="large"
                        className={styles.detailBtn}
                        onClick={() => router.push(`/filmDetail?link=${item.mid}`)}
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
