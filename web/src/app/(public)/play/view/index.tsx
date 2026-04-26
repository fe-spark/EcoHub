"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import { useRouter } from "next/navigation";
import { StepForwardOutlined, PlayCircleOutlined } from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import VideoPlayer from "@/components/public/VideoPlayer";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";
import { buildPlayPath } from "@/lib/playNavigation";
import styles from "./index.module.less";

function parseInitialTimeParam(value?: string): number {
  if (!value) return 0;
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function makeEpisodeKey(sourceId: string, episodeIndex: number) {
  return `${sourceId}:${episodeIndex}`;
}

function buildPlayLink(
  filmId: string | number,
  sourceId: string,
  episodeIndex: number,
  currentTime = 0,
) {
  return buildPlayPath(String(filmId), sourceId, episodeIndex, currentTime);
}

function buildInitialPlaybackState(data: any, initialTime?: string) {
  const playingSourceId = data?.currentPlayFrom || "";
  const episodeIndex = data?.currentEpisode ?? 0;

  return {
    playingSourceId,
    viewingSourceId: playingSourceId,
    current: data?.current ? { index: episodeIndex, ...data.current } : null,
    playInitialTime: parseInitialTimeParam(initialTime),
  };
}

interface PlayPageViewProps {
  data: any;
  filmId: string;
  initialTime?: string;
  emptyMessage?: string;
}

export default function PlayPageView({
  data,
  filmId,
  initialTime,
  emptyMessage,
}: PlayPageViewProps) {
  const router = useRouter();
  const { message } = useAppMessage();
  const initialPlaybackState = buildInitialPlaybackState(data, initialTime);

  const [playingSourceId, setPlayingSourceId] = useState(
    initialPlaybackState.playingSourceId,
  );
  const [viewingSourceId, setViewingSourceId] = useState(
    initialPlaybackState.viewingSourceId,
  );
  const [current, setCurrent] = useState<any>(initialPlaybackState.current);
  const [playInitialTime, setPlayInitialTime] = useState(
    initialPlaybackState.playInitialTime,
  );
  const [autoplay, setAutoplay] = useState(true);
  const [playerError, setPlayerError] = useState(false);
  const [isSourceMenuOpen, setIsSourceMenuOpen] = useState(false);

  const activeEpRef = useRef<HTMLDivElement>(null);
  const activeTabRef = useRef<HTMLDivElement>(null);
  const sourceTabsRef = useRef<HTMLDivElement>(null);
  const episodeListRef = useRef<HTMLDivElement>(null);
  const sourceMenuRef = useRef<HTMLDivElement>(null);

  const currentFilm = data?.detail;
  const relatedFilms = data?.relate;

  const applyPlaybackSelection = useCallback(
    (nextSourceId: string, episodeIndex: number, currentPlay: any, nextInitialTime = 0) => {
      setCurrent(currentPlay ? { index: episodeIndex, ...currentPlay } : null);
      setPlayingSourceId(nextSourceId);
      setViewingSourceId(nextSourceId);
      setPlayInitialTime(nextInitialTime);
      setPlayerError(false);
    },
    [],
  );

  const viewingSource = currentFilm?.list?.find((item: any) => item.id === viewingSourceId);
  const playingSource = currentFilm?.list?.find((item: any) => item.id === playingSourceId);
  const currentEpisodeKey =
    current && playingSourceId ? makeEpisodeKey(playingSourceId, current.index) : "";
  const visibleActiveEpisodeKey =
    viewingSourceId === playingSourceId ? currentEpisodeKey : "";
  const hasNext =
    playingSource && current && current.index < (playingSource?.linkList?.length ?? 0) - 1;

  useEffect(() => {
    const handlePointerDown = (event: MouseEvent | TouchEvent) => {
      if (!sourceMenuRef.current?.contains(event.target as Node)) {
        setIsSourceMenuOpen(false);
      }
    };

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setIsSourceMenuOpen(false);
      }
    };

    document.addEventListener("mousedown", handlePointerDown);
    document.addEventListener("touchstart", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);

    return () => {
      document.removeEventListener("mousedown", handlePointerDown);
      document.removeEventListener("touchstart", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, []);

  useEffect(() => {
    const scrollToTarget = (
      container: HTMLElement | null,
      target: HTMLElement | null,
      isHorizontal = false,
    ) => {
      if (!container || !target) return;
      container.scrollTo({
        [isHorizontal ? "left" : "top"]: isHorizontal
          ? target.offsetLeft - container.offsetWidth / 2 + target.offsetWidth / 2
          : target.offsetTop - container.offsetHeight / 2 + target.offsetHeight / 2,
        behavior: "smooth",
      });
    };

    if (visibleActiveEpisodeKey) {
      scrollToTarget(episodeListRef.current, activeEpRef.current);
    } else if (episodeListRef.current) {
      episodeListRef.current.scrollTo({ top: 0, behavior: "smooth" });
    }

    if (viewingSourceId) {
      scrollToTarget(sourceTabsRef.current, activeTabRef.current, true);
    }
  }, [visibleActiveEpisodeKey, viewingSourceId]);

  const handlePlayNext = () => {
    if (!hasNext) {
      message.info("已经是最后一集了");
      return;
    }

    const nextEpisodeIndex = current.index + 1;
    const nextEpisode = playingSource?.linkList?.[nextEpisodeIndex];
    if (nextEpisode) {
      applyPlaybackSelection(playingSourceId, nextEpisodeIndex, nextEpisode);
    }

    router.replace(buildPlayPath(String(filmId), playingSourceId, nextEpisodeIndex), {
      scroll: false,
    });
  };

  const persistHistory = useCallback(
    (currentTime?: number, duration?: number) => {
      if (!currentFilm || !current || !playingSourceId) return;

      const historyMap = readHistoryMap();
      const historyKey = String(currentFilm.id);
      const previousRecord = historyMap[historyKey];
      const nextCurrentTime =
        typeof currentTime === "number" && Number.isFinite(currentTime)
          ? currentTime
          : previousRecord?.currentTime || 0;
      const nextDuration =
        typeof duration === "number" && Number.isFinite(duration)
          ? duration
          : previousRecord?.duration || 0;

      historyMap[historyKey] = {
        ...(previousRecord ?? {}),
        id: currentFilm.id,
        name: currentFilm.name,
        picture: currentFilm.picture,
        sourceId: playingSourceId,
        episodeIndex: current.index,
        sourceName: playingSource?.name || "默认源",
        episode: current.episode || "正在观看",
        timeStamp: Date.now(),
        link: buildPlayLink(currentFilm.id, playingSourceId, current.index, nextCurrentTime),
        currentTime: nextCurrentTime,
        duration: nextDuration,
        devices: window.innerWidth <= 768,
      };

      writeHistoryMap(historyMap);
    },
    [currentFilm, current, playingSourceId, playingSource],
  );

  useEffect(() => {
    persistHistory();
  }, [persistHistory]);

  const handleTimeUpdate = useCallback(
    (currentTime: number, duration: number) => {
      persistHistory(currentTime, duration);
    },
    [persistHistory],
  );

  if (!data || !currentFilm) {
    return (
      <div className={styles.emptyPage}>
        <div className={styles.emptyCard}>
          <div className={styles.emptyEyebrow}>Play Unavailable</div>
          <h1 className={styles.emptyTitle}>当前内容无法播放</h1>
          <p className={styles.emptyDescription}>
            {emptyMessage || "未获取到影片播放数据，请返回上一页后重新尝试。"}
          </p>
          <div className={styles.emptyActions}>
            <button
              type="button"
              className={styles.emptyPrimaryAction}
              onClick={() => router.back()}
            >
              返回上一页
            </button>
            <button
              type="button"
              className={styles.emptySecondaryAction}
              onClick={() => router.push("/")}
            >
              回到首页
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <div className={styles.bgWrapper}>
        {/* 播放页背景图来自动态视频资源，当前不走 next/image 优化链路 */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img src={currentFilm.picture} className={styles.bgPoster} alt="background" />
        <div className={styles.mask} />
      </div>

      <div className={styles.mainContent}>
        <div className={styles.leftColumn}>
            <div className={styles.topInfoCard}>
              <div className={styles.leftSection}>
                <h1 className={styles.filmTitle}>{currentFilm.name}</h1>
              <div className={styles.meta}>
                <span className={styles.active}>{currentFilm.descriptor.remarks}</span>
                <span>|</span>
                <span>{currentFilm.descriptor.cName}</span>
                <span>|</span>
                <span>{currentFilm.descriptor.year}</span>
                <span>|</span>
                <span>{currentFilm.descriptor.area}</span>
              </div>
            </div>
            <div className={styles.rightSection}>
              <div className={styles.extraInfo}>
                <div className={styles.scoreLabel}>综合评分</div>
                <div className={styles.scoreValue}>
                  {currentFilm.descriptor.score || "9.0"}
                  <span>分</span>
                </div>
              </div>
            </div>
          </div>

          <div className={`${styles.playerWrapper} ${playerError ? styles.isPlayerError : ""}`}>
            {current?.link && (
              <VideoPlayer
                key={current.link}
                src={current.link}
                initialTime={playInitialTime}
                autoplay={autoplay}
                onEnded={() => autoplay && handlePlayNext()}
                onTimeUpdate={handleTimeUpdate}
                onError={() => {
                  setPlayerError(true);
                  message.error("该视频源加载失败，请尝试切换播放源。");
                }}
              />
            )}
          </div>
        </div>

        <div className={styles.sidebarWrapper}>
          <div className={styles.sidebar}>
            <div className={styles.sideHeader}>
              <div className={styles.title}>正在播放</div>
              <div className={styles.subtitle}>
                {currentFilm.name} - {current?.episode}
              </div>
            </div>

            <div className={styles.sourcePicker} ref={sourceMenuRef}>
              <button
                type="button"
                className={`${styles.sourcePickerTrigger} ${isSourceMenuOpen ? styles.open : ""}`}
                aria-expanded={isSourceMenuOpen}
                onClick={() => setIsSourceMenuOpen((prev) => !prev)}
              >
                <div className={styles.sourcePickerMeta}>
                  <span className={styles.sourcePickerLabel}>播放源</span>
                  <span className={styles.sourcePickerValue}>
                    {viewingSource?.name || "选择播放源"}
                  </span>
                </div>
                <span className={styles.sourcePickerArrow} aria-hidden="true" />
              </button>

              {isSourceMenuOpen && (
                <div className={styles.sourcePickerMenu}>
                  {currentFilm?.list?.map((item: any) => {
                    const isViewing = viewingSourceId === item.id;
                    const isPlaying = playingSourceId === item.id;
                    const episodeCount = item.linkList?.length ?? 0;

                    return (
                      <button
                        key={item.id}
                        type="button"
                        className={`${styles.sourcePickerOption} ${isViewing ? styles.active : ""}`}
                        onClick={() => {
                          if (viewingSourceId === item.id) {
                            setIsSourceMenuOpen(false);
                            return;
                          }

                          setViewingSourceId(item.id);
                          setIsSourceMenuOpen(false);
                        }}
                      >
                        <span className={styles.sourcePickerOptionMain}>{item.name}</span>
                        <span className={styles.sourcePickerOptionMeta}>
                          {isPlaying ? "当前播放" : `${episodeCount} 集`}
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
            </div>

            <div className={styles.sourceTabs} ref={sourceTabsRef}>
              {currentFilm?.list?.map((item: any) => {
                const isActive = viewingSourceId === item.id;
                return (
                  <div
                    key={item.id}
                    ref={isActive ? activeTabRef : null}
                    className={`${styles.tab} ${isActive ? styles.active : ""}`}
                    onClick={() => {
                      if (viewingSourceId === item.id) return;
                      setViewingSourceId(item.id);
                      setIsSourceMenuOpen(false);
                    }}
                  >
                    {item.name}
                  </div>
                );
              })}
            </div>

            <div className={styles.episodeList} ref={episodeListRef}>
              {viewingSource?.linkList?.map((item: any, index: number) => {
                const episodeKey = makeEpisodeKey(viewingSourceId, index);
                const isActive = visibleActiveEpisodeKey === episodeKey;
                return (
                  <div
                    key={index}
                    ref={isActive ? activeEpRef : undefined}
                    className={`${styles.epItem} ${isActive ? styles.active : ""}`}
                    title={item.episode}
                    onClick={() => {
                      if (currentEpisodeKey === episodeKey) return;

                      applyPlaybackSelection(viewingSourceId, index, item);
                      router.replace(buildPlayPath(String(filmId), viewingSourceId, index), {
                        scroll: false,
                      });
                    }}
                    onMouseEnter={(event) => {
                      const span = event.currentTarget.querySelector<HTMLSpanElement>(
                        `.${styles.epText}`,
                      );
                      if (span && span.scrollWidth > span.clientWidth) {
                        const overflow = span.scrollWidth - span.clientWidth;
                        const duration = overflow / 50 / 0.6;
                        span.style.setProperty("--scroll-distance", `-${overflow}px`);
                        span.style.setProperty("--scroll-duration", `${duration.toFixed(2)}s`);
                        span.classList.add(styles.marquee);
                      }
                    }}
                    onMouseLeave={(event) => {
                      const span = event.currentTarget.querySelector<HTMLSpanElement>(
                        `.${styles.epText}`,
                      );
                      if (span) {
                        span.classList.remove(styles.marquee);
                        span.style.removeProperty("--scroll-distance");
                        span.style.removeProperty("--scroll-duration");
                      }
                    }}
                  >
                    <span className={styles.epText}>{item.episode}</span>
                  </div>
                );
              })}
            </div>

            <div className={styles.sideFooter}>
              <div
                className={`${styles.footerBtn} ${autoplay ? styles.active : ""}`}
                onClick={() => setAutoplay(!autoplay)}
              >
                <PlayCircleOutlined />
                <span>{autoplay ? "自动播放 开" : "自动播放 关"}</span>
              </div>
              {hasNext && (
                <div className={styles.footerBtn} onClick={handlePlayNext}>
                  <StepForwardOutlined />
                  <span>下一集</span>
                </div>
              )}
            </div>
          </div>
        </div>
      </div>

      <div className={styles.infoArea}>
        <div className={styles.introHeading}>剧情简介</div>
        <div className={styles.intro}>
          {currentFilm.descriptor.content
            ? currentFilm.descriptor.content.replace(/<[^>]+>/g, "").trim()
            : "暂无简介"}
        </div>
      </div>

      <div className={styles.recommendation}>
        <h2 className={styles.sectionTitle}>相关推荐</h2>
        <FilmList list={relatedFilms} className={styles.classifyGrid} />
      </div>
    </div>
  );
}
