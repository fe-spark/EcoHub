"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Button } from "antd";
import { PlayCircleFilled } from "@ant-design/icons";
import { Autoplay, EffectCards } from "swiper/modules";
import { Swiper, SwiperSlide } from "swiper/react";
import type { Swiper as SwiperType } from "swiper";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import { resolvePlayEntryPath } from "@/lib/playNavigation";
import "swiper/css";
import "swiper/css/effect-cards";
import styles from "./HomeHero.module.less";

const AUTOPLAY_DELAY = 4800;
const SLIDE_SPEED = 500;

export interface HeroBannerItem {
  id: string;
  mid: string;
  name: string;
  poster?: string;
  picture: string;
  pictureSlide?: string;
  year?: string | number;
  cName?: string;
  area?: string;
  classTag?: string;
  actor?: string;
  director?: string;
  remark?: string;
  remarks?: string;
  blurb?: string;
  score?: string | number;
  hits?: number;
}

function parseClassTags(classTag?: any, limit = 1): string[] {
  return String(classTag || "").split(/[,/，\s]+/).filter(Boolean).slice(0, limit);
}

function resolveQualityBadge(remarks?: any): string {
  const text = String(remarks || "").toUpperCase();
  if (text.includes("4K")) return "4K 超清";
  if (text.includes("1080")) return "1080P";
  if (text.includes("HD")) return "HD 高清";
  return "4K 超清";
}

function resolveHeroSummary(item: HeroBannerItem): string {
  if (item.blurb) return String(item.blurb).trim();
  if (item.director || item.actor) {
    return [item.director && `导演：${item.director}`, item.actor && `主演：${item.actor}`].filter(Boolean).join(" · ");
  }
  return `热播精选《${item.name || "影视"}》，高清流畅播放，点击立即畅享精彩正片。`;
}

function getBackdrop(item: HeroBannerItem) {
  return item.pictureSlide || item.picture || item.poster || "";
}

function getPoster(item: HeroBannerItem) {
  return item.poster || item.picture || item.pictureSlide || "";
}

export default function HomeHero({ banners }: { banners: HeroBannerItem[] }) {
  const { navigate } = useContentNavigate();
  const swiperRef = useRef<SwiperType | null>(null);
  const resumeTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [activeIndex, setActiveIndex] = useState(0);

  const covers = useMemo(() => banners.filter(Boolean), [banners]);
  const multi = covers.length > 1;
  const safeIndex = covers.length
    ? Math.min(Math.max(activeIndex, 0), covers.length - 1)
    : 0;
  const active = covers[safeIndex];

  const autoplayConfig = useMemo(
    () =>
      multi
        ? {
            delay: AUTOPLAY_DELAY,
            disableOnInteraction: false,
            pauseOnMouseEnter: true,
            // EffectCards 下 wrapper 不一定触发 transitionend，避免永久 paused
            waitForTransition: false,
          }
        : false,
    [multi],
  );

  const clearResumeTimer = useCallback(() => {
    if (resumeTimerRef.current != null) {
      clearTimeout(resumeTimerRef.current);
      resumeTimerRef.current = null;
    }
  }, []);

  /** 中断自动轮播定时器（手动切换 / 拖拽时调用） */
  const stopAutoplay = useCallback(() => {
    clearResumeTimer();
    const swiper = swiperRef.current;
    if (swiper?.autoplay?.running) {
      swiper.autoplay.stop();
    }
  }, [clearResumeTimer]);

  /** 切换动画结束后重新计时启动自动轮播 */
  const scheduleAutoplayRestart = useCallback(() => {
    clearResumeTimer();
    if (!multi) {
      return;
    }
    const swiper = swiperRef.current;
    const delay = typeof swiper?.params?.speed === "number" ? swiper.params.speed : SLIDE_SPEED;
    resumeTimerRef.current = setTimeout(() => {
      resumeTimerRef.current = null;
      const s = swiperRef.current;
      if (!s || s.destroyed || !multi) {
        return;
      }
      // 仅在已被 stop 时重启；hover pause 保持 running，不强制 resume
      if (!s.autoplay.running) {
        s.autoplay.start();
      }
    }, delay);
  }, [clearResumeTimer, multi]);

  /** 手动切到指定页：先停表，再切换，动画后重新自动轮播 */
  const slideToManual = useCallback(
    (index: number) => {
      const swiper = swiperRef.current;
      if (!swiper || swiper.destroyed || swiper.activeIndex === index) {
        return;
      }
      stopAutoplay();
      swiper.slideTo(index);
      scheduleAutoplayRestart();
    },
    [scheduleAutoplayRestart, stopAutoplay],
  );

  useEffect(() => () => clearResumeTimer(), [clearResumeTimer]);

  if (!covers.length || !active) {
    return null;
  }

  const goPlay = (mid: string) => {
    navigate(
      resolvePlayEntryPath(mid, {
        sourceId: "0",
        episodeIndex: 0,
      }),
      "进入播放页...",
    );
  };

  const onCardClick = (index: number, mid: string) => {
    const swiper = swiperRef.current;
    if (!swiper || swiper.activeIndex === index) {
      goPlay(mid);
      return;
    }
    slideToManual(index);
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
              <span className={styles.badgeFeatured}>✨ 精选推荐</span>
              {active.score && Number(active.score) > 0 ? (
                <span className={styles.badgeRating}>★ {Number(active.score).toFixed(1)}</span>
              ) : null}
              <span className={styles.badgeQuality}>
                {resolveQualityBadge(active.remarks || active.remark)}
              </span>
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
              {active.cName ? (
                <span className={styles.tagCategory}>{active.cName}</span>
              ) : null}
              {active.score && Number(active.score) > 0 ? (
                <span className={styles.tagRating}>★ {Number(active.score).toFixed(1)}</span>
              ) : null}
              {active.year && String(active.year) !== "0" ? (
                <span className={styles.tag}>{String(active.year)}</span>
              ) : null}
              {active.area ? (
                <span className={styles.tag}>{String(active.area)}</span>
              ) : null}
              {parseClassTags(active.classTag, 1).map((tag) => (
                <span key={tag} className={styles.tag}>{tag}</span>
              ))}
              {active.remarks || active.remark ? (
                <span className={styles.tagHighlight}>{String(active.remarks || active.remark)}</span>
              ) : null}
            </div>

            <p className={styles.blurb}>{resolveHeroSummary(active)}</p>

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
                    onClick={() => slideToManual(index)}
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
                perSlideOffset: 16,
                perSlideRotate: 3,
                rotate: true,
                slideShadows: true,
              }}
              grabCursor={multi}
              allowTouchMove={multi}
              speed={SLIDE_SPEED}
              rewind={multi}
              autoplay={autoplayConfig}
              onSwiper={(swiper) => {
                swiperRef.current = swiper;
                setActiveIndex(swiper.activeIndex);
              }}
              onSlideChange={(swiper) => setActiveIndex(swiper.activeIndex)}
              onTouchStart={() => {
                if (!multi) {
                  return;
                }
                // 按下即取消待重启，避免拖拽中途被重新计时
                clearResumeTimer();
              }}
              onSliderFirstMove={() => {
                // 真正开始拖拽时中断自动轮播
                if (multi) {
                  stopAutoplay();
                }
              }}
              onTouchMove={() => {
                if (!multi) {
                  return;
                }
                // 拖动全程保持中断：停表 + 清掉可能排队的重启
                clearResumeTimer();
                stopAutoplay();
              }}
              onTouchEnd={() => {
                if (multi) {
                  scheduleAutoplayRestart();
                }
              }}
              onTransitionEnd={(swiper) => {
                // 兜底：手动切换后若 autoplay 仍停着则重启
                if (multi && !swiper.destroyed && !swiper.autoplay?.running) {
                  scheduleAutoplayRestart();
                }
              }}
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
                    <span className={styles.cardFeaturedBadge}>✨ 精选</span>
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
              onClick={() => slideToManual(index)}
            />
          ))}
        </div>
      ) : null}
    </section>
  );
}
