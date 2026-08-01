"use client";

import { useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "antd";
import { PlayCircleFilled } from "@ant-design/icons";
import { Autoplay, EffectCards } from "swiper/modules";
import { Swiper, SwiperSlide } from "swiper/react";
import type { Swiper as SwiperType } from "swiper";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import "swiper/css";
import "swiper/css/effect-cards";
import styles from "./HomeHero.module.less";

export interface HeroBannerItem {
  id: string;
  mid: string;
  name: string;
  poster?: string;
  picture: string;
  pictureSlide?: string;
  year: string;
  cName: string;
  area?: string;
  remarks?: string;
  blurb?: string;
  score?: string | number;
}

function getBackdrop(item: HeroBannerItem) {
  return item.pictureSlide || item.picture || item.poster || "";
}

function getPoster(item: HeroBannerItem) {
  return item.poster || item.picture || item.pictureSlide || "";
}

export default function HomeHero({ banners }: { banners: HeroBannerItem[] }) {
  const router = useRouter();
  const swiperRef = useRef<SwiperType | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  const covers = useMemo(() => banners.filter(Boolean), [banners]);
  const multi = covers.length > 1;
  const safeIndex = covers.length
    ? Math.min(Math.max(activeIndex, 0), covers.length - 1)
    : 0;
  const active = covers[safeIndex];

  if (!covers.length || !active) {
    return null;
  }

  const goPlay = (mid: string) => {
    router.push(
      resolvePlayEntryPath(mid, {
        sourceId: "0",
        episodeIndex: 0,
      }),
    );
  };

  const slideTo = (index: number) => {
    swiperRef.current?.slideTo(index);
  };

  const onCardClick = (index: number, mid: string) => {
    const swiper = swiperRef.current;
    if (!swiper || swiper.activeIndex === index) {
      goPlay(mid);
      return;
    }
    swiper.slideTo(index);
  };

  return (
    <section className={styles.hero} aria-label="首页推荐">
      <div className={styles.backdrop} aria-hidden>
        <div
          key={active.id}
          className={styles.backdropImage}
          style={{ backgroundImage: `url(${getBackdrop(active)})` }}
        />
        <div className={styles.backdropShade} />
      </div>

      <div className={styles.shell}>
        <div className={styles.layout}>
          <div className={styles.copy}>
            <p className={styles.eyebrow}>本周精选 · Featured</p>

            <div className={styles.copyHead}>
              {active.cName ? (
                <span className={styles.chip}>{active.cName}</span>
              ) : null}
              {active.score ? (
                <span className={styles.badgeRating}>★ {active.score}</span>
              ) : (
                <span className={styles.badgeRating}>★ 9.6</span>
              )}
              <span className={styles.badgeQuality}>4K 超清</span>
              {multi ? (
                <span className={styles.counter}>
                  {String(safeIndex + 1).padStart(2, "0")}
                  <i>/</i>
                  {String(covers.length).padStart(2, "0")}
                </span>
              ) : null}
            </div>

            <h2 className={styles.title}>{active.name}</h2>

            <div className={styles.tags}>
              {active.year && active.year !== "0" ? (
                <span className={styles.tag}>{active.year}</span>
              ) : null}
              {active.cName ? (
                <span className={styles.tag}>{active.cName}</span>
              ) : null}
              {active.area ? (
                <span className={styles.tag}>{active.area}</span>
              ) : null}
              {active.remarks ? (
                <span className={styles.tagHighlight}>{active.remarks}</span>
              ) : null}
            </div>

            {active.blurb ? (
              <p className={styles.blurb}>{active.blurb}</p>
            ) : (
              <p className={styles.blurb}>
                震撼视觉体验，呈现高帧率全高清画面与纯正环绕声效，带您体验沉浸式观影之旅。
              </p>
            )}

            <Button
              type="primary"
              size="large"
              icon={<PlayCircleFilled />}
              className={styles.playBtn}
              onClick={() => goPlay(active.mid)}
            >
              立即播放
            </Button>

            {multi ? (
              <div className={styles.thumbs} aria-label="推荐列表">
                {covers.map((item, index) => (
                  <button
                    key={`${item.id}-thumb`}
                    type="button"
                    className={`${styles.thumb} ${index === safeIndex ? styles.thumbActive : ""}`}
                    style={{ backgroundImage: `url(${getPoster(item)})` }}
                    onClick={() => slideTo(index)}
                    aria-label={`切换到 ${item.name}`}
                    aria-current={index === safeIndex}
                  />
                ))}
              </div>
            ) : null}
          </div>

          <div className={styles.deck}>
            <Swiper
              modules={[Autoplay, EffectCards]}
              className={styles.deckSwiper}
              effect="cards"
              cardsEffect={{
                perSlideOffset: 12,
                perSlideRotate: 2,
                rotate: true,
                slideShadows: true,
              }}
              grabCursor={multi}
              allowTouchMove={multi}
              speed={500}
              rewind={multi}
              autoplay={
                multi
                  ? {
                      delay: 4800,
                      disableOnInteraction: false,
                      pauseOnMouseEnter: true,
                    }
                  : false
              }
              onSwiper={(swiper) => {
                swiperRef.current = swiper;
                setActiveIndex(swiper.activeIndex);
              }}
              onSlideChange={(swiper) => setActiveIndex(swiper.activeIndex)}
            >
              {covers.map((item, index) => (
                <SwiperSlide key={`${item.id}-${index}`} className={styles.deckSlide}>
                  <button
                    type="button"
                    className={styles.card}
                    onClick={() => onCardClick(index, item.mid)}
                    aria-label={
                      index === safeIndex ? `播放 ${item.name}` : `切换到 ${item.name}`
                    }
                  >
                    <span
                      className={styles.cardImg}
                      style={{ backgroundImage: `url(${getPoster(item)})` }}
                    />
                  </button>
                </SwiperSlide>
              ))}
            </Swiper>
          </div>
        </div>
      </div>

      {multi ? (
        <div className={styles.bannerDots} role="tablist" aria-label="轮播进度">
          {covers.map((item, index) => (
            <button
              key={`${item.id}-dot`}
              type="button"
              role="tab"
              aria-selected={index === safeIndex}
              aria-label={`第 ${index + 1} 张`}
              className={`${styles.bullet} ${index === safeIndex ? styles.bulletActive : ""}`}
              onClick={() => slideTo(index)}
            />
          ))}
        </div>
      ) : null}
    </section>
  );
}
