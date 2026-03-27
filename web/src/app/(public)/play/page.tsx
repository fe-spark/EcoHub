"use client";

import React, {
  useState,
  useEffect,
  useRef,
  Suspense,
  useCallback,
} from "react";
import { useRouter, useSearchParams } from "next/navigation";
import {
  StepForwardOutlined,
  PlayCircleOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/api";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import VideoPlayer from "@/components/public/VideoPlayer";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";

function parseInitialTimeParam(value: string | null): number {
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
  const params = new URLSearchParams({
    id: String(filmId),
    source: sourceId,
    episode: String(episodeIndex),
  });

  if (currentTime > 0) {
    params.set("currentTime", String(Math.floor(currentTime)));
  }

  return `/play?${params.toString()}`;
}

function resolvePlayback(detail: any, requestedSourceId?: string | null, requestedEpisode?: string | null) {
  const sourceList = detail?.list ?? [];
  if (!sourceList.length) return null;

  const hasRequestedSource = requestedSourceId
    ? sourceList.some((item: any) => item.id === requestedSourceId)
    : false;
  const sourceId = hasRequestedSource ? requestedSourceId! : sourceList[0].id;
  const source = sourceList.find((item: any) => item.id === sourceId);
  if (!source) return null;

  const parsedEpisode = Number.parseInt(requestedEpisode ?? "0", 10);
  const normalizedEpisode = Number.isInteger(parsedEpisode) && parsedEpisode >= 0
    ? parsedEpisode
    : 0;

  if (!source.linkList?.length) {
    return {
      sourceId,
      source,
      episodeIndex: 0,
      currentPlay: null,
    };
  }

  const episodeIndex =
    normalizedEpisode < source.linkList.length ? normalizedEpisode : 0;

  return {
    sourceId,
    source,
    episodeIndex,
    currentPlay: source.linkList[episodeIndex],
  };
}

function PlayerContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const id = searchParams.get("id");
  const sourceId = searchParams.get("source");
  const episodeIdx = searchParams.get("episode");
  const initialTime = searchParams.get("currentTime");
  const { message } = useAppMessage();

  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);
  const [playingSourceId, setPlayingSourceId] = useState("");
  const [viewingSourceId, setViewingSourceId] = useState("");
  const [current, setCurrent] = useState<any>(null);
  const [playInitialTime, setPlayInitialTime] = useState(() =>
    parseInitialTimeParam(initialTime),
  );
  const [autoplay, setAutoplay] = useState(true);
  const [playerError, setPlayerError] = useState(false);
  const [isSourceMenuOpen, setIsSourceMenuOpen] = useState(false);

  const activeEpRef = useRef<HTMLDivElement>(null);
  const activeTabRef = useRef<HTMLDivElement>(null);
  const sourceTabsRef = useRef<HTMLDivElement>(null);
  const episodeListRef = useRef<HTMLDivElement>(null);
  const sourceMenuRef = useRef<HTMLDivElement>(null);

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


  // 1. 数据加载与状态同步
  useEffect(() => {
    if (!id) return;

    const nextInitialTime = parseInitialTimeParam(initialTime);
    const currentFilmId = Number.parseInt(id, 10);

    if (data?.detail?.id === currentFilmId) {
      const resolved = resolvePlayback(data.detail, sourceId, episodeIdx);
      if (resolved) {
        applyPlaybackSelection(
          resolved.sourceId,
          resolved.episodeIndex,
          resolved.currentPlay,
          nextInitialTime,
        );
      }
      setLoading(false);
      return;
    }

    const load = async () => {
      // Only show page-level loading on initial load (when data is null)
      if (!data) {
        setLoading(true);
      }
      try {
        const resp = await ApiGet("/filmPlayInfo", {
          id,
          playFrom: sourceId,
          episode: episodeIdx || 0,
        });
        if (resp.code === 0) {
          const nextSourceId = resp.data.currentPlayFrom;

          setData(resp.data);
          applyPlaybackSelection(
            nextSourceId,
            resp.data.currentEpisode,
            resp.data.current,
            nextInitialTime,
          );
          setPlayerError(false); // Reset error state on new resource load
        } else {
          message.error(resp.msg);
        }
      } finally {
        setLoading(false);
      }
    };

    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [id, sourceId, episodeIdx, initialTime, message, applyPlaybackSelection]);

  // 计算衍生数据，减少冗余逻辑
  const detail = data?.detail;
  const relate = data?.relate;
  const viewingSource = detail?.list?.find((s: any) => s.id === viewingSourceId);
  const playingSource = detail?.list?.find((s: any) => s.id === playingSourceId);
  const currentEpisodeKey =
    current && playingSourceId
      ? makeEpisodeKey(playingSourceId, current.index)
      : "";
  const visibleActiveEpisodeKey =
    viewingSourceId === playingSourceId ? currentEpisodeKey : "";
  const hasNext =
    playingSource &&
    current &&
    current.index < (playingSource?.linkList?.length ?? 0) - 1;

  useEffect(() => {
    setIsSourceMenuOpen(false);
  }, [viewingSourceId]);

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

  // 自动滚动定位：当前集数和播放源
  useEffect(() => {
    const scrollToTarget = (container: HTMLElement | null, target: HTMLElement | null, isHorizontal = false) => {
      if (!container || !target) return;
      container.scrollTo({
        [isHorizontal ? "left" : "top"]: (isHorizontal ? target.offsetLeft - container.offsetWidth / 2 + target.offsetWidth / 2 : target.offsetTop - container.offsetHeight / 2 + target.offsetHeight / 2),
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

  const handlePlayNext = useCallback(() => {
    if (hasNext) {
      const nextEpisodeIndex = current.index + 1;
      const nextEpisode = playingSource?.linkList?.[nextEpisodeIndex];
      if (nextEpisode) {
        applyPlaybackSelection(playingSourceId, nextEpisodeIndex, nextEpisode);
      }
      router.replace(`/play?id=${id}&source=${playingSourceId}&episode=${current.index + 1}`, { scroll: false });
    } else {
      message.info("已经是最后一集了");
    }
  }, [hasNext, id, playingSourceId, current?.index, router, message, playingSource, applyPlaybackSelection]);

  const persistHistory = useCallback(
    (currentTime?: number, duration?: number) => {
      if (!detail || !current || !playingSourceId) return;

      const historyMap = readHistoryMap();
      const historyKey = String(detail.id);
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
        id: detail.id,
        name: detail.name,
        picture: detail.picture,
        sourceId: playingSourceId,
        episodeIndex: current.index,
        sourceName: playingSource?.name || "默认源",
        episode: current.episode || "正在观看",
        timeStamp: Date.now(),
        link: buildPlayLink(detail.id, playingSourceId, current.index, nextCurrentTime),
        currentTime: nextCurrentTime,
        duration: nextDuration,
        devices: window.innerWidth <= 768,
      };

      writeHistoryMap(historyMap);
    },
    [detail, current, playingSourceId, playingSource],
  );

  useEffect(() => {
    persistHistory();
  }, [persistHistory]);

  // 2. 核心逻辑：播放进度保存
  const handleTimeUpdate = useCallback(
    (currentTime: number, duration: number) => {
      persistHistory(currentTime, duration);
    },
    [persistHistory],
  );

  if (loading) return <AppLoading text="" padding="88px 0" />;
  if (!data) return null;

  return (
    <div className={styles.container}>
      <div className={styles.bgWrapper}>
        <img src={detail.picture} className={styles.bgPoster} alt="background" />
        <div className={styles.mask} />
      </div>

      <div className={styles.mainContent}>
        <div className={styles.leftColumn}>
          <div className={styles.topInfoCard}>
            <div className={styles.leftSection}>
              <h1 className={styles.filmTitle}>
                <a onClick={() => router.push(`/filmDetail?link=${detail.id}`)} style={{ cursor: "pointer" }}>
                  {detail.name}
                </a>
              </h1>
              <div className={styles.meta}>
                <span className={styles.active}>{detail.descriptor.remarks}</span>
                <span>|</span>
                <span>{detail.descriptor.cName}</span>
                <span>|</span>
                <span>{detail.descriptor.year}</span>
                <span>|</span>
                <span>{detail.descriptor.area}</span>
              </div>
            </div>
            <div className={styles.rightSection}>
              <div className={styles.extraInfo}>
                <div className={styles.scoreLabel}>综合评分</div>
                <div className={styles.scoreValue}>
                  {detail.descriptor.score || "9.0"}
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
              <div className={styles.subtitle}>{detail.name} - {current?.episode}</div>
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
                  {detail?.list?.map((item: any) => {
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
              {detail?.list?.map((item: any) => {
                const isActive = viewingSourceId === item.id;
                return (
                  <div
                    key={item.id}
                    ref={isActive ? activeTabRef : null}
                    className={`${styles.tab} ${isActive ? styles.active : ""}`}
                    onClick={() => {
                      if (viewingSourceId === item.id) return;
                      setViewingSourceId(item.id);
                    }}
                  >
                    {item.name}
                  </div>
                );
              })}
            </div>

            <div className={styles.episodeList} ref={episodeListRef}>
              {viewingSource?.linkList?.map((v: any, i: number) => {
                const episodeKey = makeEpisodeKey(viewingSourceId, i);
                const isActive = visibleActiveEpisodeKey === episodeKey;
                return (
                  <div
                    key={i}
                    ref={isActive ? activeEpRef : undefined}
                    className={`${styles.epItem} ${isActive ? styles.active : ""}`}
                    title={v.episode}
                    onClick={() => {
                      if (currentEpisodeKey === episodeKey) return;

                      applyPlaybackSelection(viewingSourceId, i, v);
                      router.replace(`/play?id=${id}&source=${viewingSourceId}&episode=${i}`, { scroll: false });
                    }}
                    onMouseEnter={(e) => {
                      const span = e.currentTarget.querySelector<HTMLSpanElement>(`.${styles.epText}`);
                      if (span && span.scrollWidth > span.clientWidth) {
                        const overflow = span.scrollWidth - span.clientWidth;
                        const duration = overflow / 50 / 0.6;
                        span.style.setProperty("--scroll-distance", `-${overflow}px`);
                        span.style.setProperty("--scroll-duration", `${duration.toFixed(2)}s`);
                        span.classList.add(styles.marquee);
                      }
                    }}
                    onMouseLeave={(e) => {
                      const span = e.currentTarget.querySelector<HTMLSpanElement>(`.${styles.epText}`);
                      if (span) {
                        span.classList.remove(styles.marquee);
                        span.style.removeProperty("--scroll-distance");
                        span.style.removeProperty("--scroll-duration");
                      }
                    }}
                  >
                    <span className={styles.epText}>{v.episode}</span>
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
          {detail.descriptor.content ? detail.descriptor.content.replace(/<[^>]+>/g, "").trim() : "暂无简介"}
        </div>
      </div>

      <div className={styles.recommendation}>
        <h2 className={styles.sectionTitle}>相关推荐</h2>
        <FilmList list={relate} className={styles.classifyGrid} />
      </div>
    </div>
  );
}

export default function PlayPage() {
  return (
    <Suspense fallback={null}>
      <PlayerContent />
    </Suspense>
  );
}
