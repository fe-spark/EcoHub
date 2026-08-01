"use client";

import { useRouter } from "next/navigation";
import {
  VideoCameraOutlined,
  PlaySquareOutlined,
  SmileOutlined,
  RocketOutlined,
  FireOutlined,
} from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import { startNavigationLoading } from "@/components/public/TopLoadingBar";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import HomeHero, { type HeroBannerItem } from "./HomeHero";
import styles from "./index.module.less";

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

interface MovieBasicInfo {
  id: string;
  mid?: string;
  name: string;
  picture: string;
  year: string;
  cName: string;
  area: string;
  language?: string;
  classTag?: string;
  remarks: string;
  blurb?: string;
}

interface ContentSection {
  nav: NavItem;
  movies: MovieBasicInfo[];
  hot: MovieBasicInfo[];
}

export default function HomePageView({
  data,
}: {
  data: {
    banners: HeroBannerItem[];
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
      <HomeHero banners={data.banners || []} />

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
                  onClick={() => {
                    startNavigationLoading("分类加载中");
                    router.push(`/filmClassify?Pid=${section.nav.id}`);
                  }}
                  style={{ cursor: "pointer" }}
                >
                  {section.nav.name}
                </a>
              </div>
              <div className={styles.nav}>
                {section.nav.children?.slice(0, 6).map((child, childIndex) => (
                  <a
                    key={childIndex}
                    onClick={() => {
                      startNavigationLoading("分类加载中");
                      router.push(
                        `/filmClassifySearch?Pid=${child.pid}&Category=${child.id}`,
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
                    startNavigationLoading("分类加载中");
                    router.push(`/filmClassify?Pid=${section.nav.id}`);
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
                    onClick={() => router.push(resolvePlayEntryPath(movie.id))}
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
