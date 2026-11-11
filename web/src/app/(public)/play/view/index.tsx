"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import { useRouter } from "next/navigation";
import { LoadingOutlined } from "@ant-design/icons";
import VideoPlayer from "@/components/public/VideoPlayer";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";
import { buildPlayPath } from "@/lib/playNavigation";
import { trackPageView } from "@/lib/track-page-view";
import { readPreferEnabled, writePreferEnabled } from "@/lib/playPreferSettings";
import {
  pickBestProbeResult,
  probePlaySources,
  type SourceProbeResult,
} from "@/lib/playSourceProbe";
import RelatedFilmsSection from "./RelatedFilmsSection";
import PlayHeaderCard from "./PlayHeaderCard";
import PlaySidebar from "./PlaySidebar";
import {
  formatLocalUpdateTime,
  formatActorNames,
  resolveFilmScore,
  makeEpisodeKey,
  buildPlayLink,
  buildInitialPlaybackState,
} from "./formatters";
import styles from "./index.module.less";

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
  // 当前视频的真实宽高比（"宽 / 高"）。换集时保留上一集的值，直到新视频的元数据到达，
  // 避免先跳回 16:9 再跳到新比例。
  const [videoAspect, setVideoAspect] = useState<string | null>(null);
  const handleVideoSize = useCallback((width: number, height: number) => {
    setVideoAspect(`${width} / ${height}`);
  }, []);
  const [isSourceMenuOpen, setIsSourceMenuOpen] = useState(false);
  const [openingRelatedId, setOpeningRelatedId] = useState("");
  const [preferEnabled, setPreferEnabled] = useState(true);
  const [preferBootstrapped, setPreferBootstrapped] = useState(false);
  const [isProbing, setIsProbing] = useState(false);
  const [probeReady, setProbeReady] = useState(false);
  const [probeBySourceId, setProbeBySourceId] = useState<Record<string, SourceProbeResult>>(
    {},
  );
  const [preferredSourceId, setPreferredSourceId] = useState("");

  const activeEpRef = useRef<HTMLDivElement>(null);
  const activeTabRef = useRef<HTMLDivElement>(null);
  const sourceTabsRef = useRef<HTMLDivElement>(null);
  const episodeListRef = useRef<HTMLDivElement>(null);
  const sourceMenuRef = useRef<HTMLDivElement>(null);
  const manualSourceLockRef = useRef(false);
  const preferRunIdRef = useRef(0);
  const preferAbortRef = useRef<AbortController | null>(null);
  const playingSourceIdRef = useRef(playingSourceId);
  const currentIndexRef = useRef(initialPlaybackState.current?.index ?? 0);
  const playInitialTimeRef = useRef(initialPlaybackState.playInitialTime);

  const currentFilm = data?.detail;
  const viewingSource = currentFilm?.list?.find((item: any) => item.id === viewingSourceId);
  const playingSource = currentFilm?.list?.find((item: any) => item.id === playingSourceId);

  const reportedPlaybackKeys = useRef<Set<string>>(new Set());

  useEffect(() => {
    playingSourceIdRef.current = playingSourceId;
  }, [playingSourceId]);

  useEffect(() => {
    currentIndexRef.current = typeof current?.index === "number" ? current.index : 0;
  }, [current?.index]);

  useEffect(() => {
    playInitialTimeRef.current = playInitialTime;
  }, [playInitialTime]);

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

  const collectProbeTargets = useCallback(
    (episodeIndex: number) => {
      const sources = Array.isArray(currentFilm?.list) ? currentFilm.list : [];
      return sources
        .map((source: any) => {
          const link = source?.linkList?.[episodeIndex]?.link;
          if (!link) return null;
          return {
            sourceId: String(source.id),
            url: String(link),
            sourceName: String(source.name || source.id),
            episodeItem: source.linkList[episodeIndex],
          };
        })
        .filter(Boolean) as Array<{
        sourceId: string;
        url: string;
        sourceName: string;
        episodeItem: any;
      }>;
    },
    [currentFilm],
  );

  const runSourcePrefer = useCallback(
    async (options?: { force?: boolean; episodeIndex?: number }) => {
      const force = Boolean(options?.force);
      const episodeIndex =
        typeof options?.episodeIndex === "number"
          ? options.episodeIndex
          : currentIndexRef.current;

      if (!force && manualSourceLockRef.current) {
        setProbeReady(true);
        setIsProbing(false);
        return;
      }

      const targets = collectProbeTargets(episodeIndex);
      if (targets.length <= 1) {
        setProbeBySourceId({});
        setPreferredSourceId(targets[0]?.sourceId || "");
        setProbeReady(true);
        setIsProbing(false);
        return;
      }

      preferAbortRef.current?.abort();
      const controller = new AbortController();
      preferAbortRef.current = controller;
      const runId = preferRunIdRef.current + 1;
      preferRunIdRef.current = runId;

      setIsProbing(true);
      setProbeReady(false);
      setProbeBySourceId({});

      try {
        const results = await probePlaySources(
          targets.map((item) => ({ sourceId: item.sourceId, url: item.url })),
          { signal: controller.signal },
        );
        if (runId !== preferRunIdRef.current) return;

        const nextMap: Record<string, SourceProbeResult> = {};
        results.forEach((item) => {
          nextMap[item.sourceId] = item;
        });
        setProbeBySourceId(nextMap);

        const best = pickBestProbeResult(results);
        setPreferredSourceId(best?.sourceId || "");

        if (best && best.sourceId !== playingSourceIdRef.current) {
          const matched = targets.find((item) => item.sourceId === best.sourceId);
          if (matched) {
            applyPlaybackSelection(
              matched.sourceId,
              episodeIndex,
              matched.episodeItem,
              playInitialTimeRef.current,
            );
            router.replace(buildPlayPath(String(filmId), matched.sourceId, episodeIndex), {
              scroll: false,
            });
            message.success(`已优选播放源：${matched.sourceName}`);
          }
        }
      } catch (error) {
        if ((error as DOMException)?.name === "AbortError") {
          return;
        }
        console.warn("Source prefer failed:", error);
      } finally {
        if (runId === preferRunIdRef.current) {
          setIsProbing(false);
          setProbeReady(true);
        }
      }
    },
    [applyPlaybackSelection, collectProbeTargets, filmId, message, router],
  );

  useEffect(() => {
    const enabled = readPreferEnabled();
    setPreferEnabled(enabled);
    setPreferBootstrapped(true);
    manualSourceLockRef.current = false;

    if (!enabled) {
      setProbeReady(true);
      setIsProbing(false);
      setPreferredSourceId("");
      setProbeBySourceId({});
      return;
    }

    void runSourcePrefer({ force: true });

    return () => {
      preferAbortRef.current?.abort();
    };
    // 仅在切换影片时自动测速；开关/重测走显式 handler
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filmId]);

  const handleTogglePrefer = useCallback(() => {
    const next = !preferEnabled;
    writePreferEnabled(next);
    setPreferEnabled(next);
    if (!next) {
      preferAbortRef.current?.abort();
      setIsProbing(false);
      setProbeReady(true);
      setPreferredSourceId("");
      setProbeBySourceId({});
      return;
    }
    manualSourceLockRef.current = false;
    void runSourcePrefer({ force: true });
  }, [preferEnabled, runSourcePrefer]);

  const handleRetestPrefer = useCallback(() => {
    if (!preferEnabled || isProbing) return;
    manualSourceLockRef.current = false;
    void runSourcePrefer({ force: true });
  }, [preferEnabled, isProbing, runSourcePrefer]);

  const handlePlayStart = useCallback(() => {
    setPlayerError(false);
    if (!currentFilm || !playingSourceId) return;
    const epIdx = current?.index ?? 0;
    const key = `${filmId}:${playingSourceId}:${epIdx}`;
    if (reportedPlaybackKeys.current.has(key)) {
      return;
    }
    reportedPlaybackKeys.current.add(key);
    trackPageView({
      action: "play",
      resource: String(filmId),
      collect_source: playingSourceId,
      resource_title: currentFilm.name || "",
      resource_poster: currentFilm.picture || "",
      resource_cat: currentFilm.descriptor?.cName || "",
      path: buildPlayPath(String(filmId), playingSourceId, epIdx),
    });
  }, [currentFilm, playingSourceId, filmId, current]);

  const handlePlayerError = useCallback(
    (err?: any) => {
      console.warn("Playback error encountered on source:", playingSourceId, err);
      setPlayerError(true);
      message.error("该视频源加载失败，请尝试切换其他播放源或重新加载。");
    },
    [playingSourceId, message],
  );

  const holdPlayerForPrefer =
    !preferBootstrapped || (preferEnabled && (!probeReady || isProbing));

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

  const handleOpenRelatedFilm = useCallback(
    (id: string, href: string) => {
      if (!id || id === String(filmId)) {
        return;
      }
      setOpeningRelatedId(id);
      window.location.assign(href);
    },
    [filmId],
  );

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

  const localUpdateTime = formatLocalUpdateTime(currentFilm.localUpdateTime);
  const filmScore = resolveFilmScore(currentFilm.descriptor);
  const actorText = formatActorNames(currentFilm.descriptor.actor);
  const remarks = String(currentFilm.descriptor.remarks || "").trim();

  return (
    <div className={styles.container}>
      {openingRelatedId && (
        <div className={styles.pageOpeningMask}>
          <LoadingOutlined />
          <span>正在重新加载影片...</span>
        </div>
      )}
      <div className={styles.bgWrapper}>
        {/* 播放页背景图来自动态视频资源，当前不走 next/image 优化链路 */}
        {currentFilm.picture ? (
          /* eslint-disable-next-line @next/next/no-img-element */
          <img src={currentFilm.picture} className={styles.bgPoster} alt="background" />
        ) : null}
        <div className={styles.mask} />
      </div>

      <div className={styles.mainContent}>
        <div className={styles.leftColumn}>
          <PlayHeaderCard
            name={currentFilm.name}
            remarks={remarks}
            filmScore={filmScore}
            localUpdateTime={localUpdateTime}
            updateReason={currentFilm.updateReason}
            cName={currentFilm.descriptor.cName}
            year={currentFilm.descriptor.year}
            area={currentFilm.descriptor.area}
            actorText={actorText}
          />

          <div
            className={`${styles.playerWrapper} ${playerError ? styles.isPlayerError : ""}`}
            style={videoAspect ? ({ "--video-aspect": videoAspect } as React.CSSProperties) : undefined}
          >
            {holdPlayerForPrefer ? (
              <div className={styles.preferOverlay}>
                <LoadingOutlined />
                <span>正在测速优选最佳播放源...</span>
                <span className={styles.preferOverlayHint}>可在右侧关闭「测速优选」立即播放</span>
              </div>
            ) : (
              current?.link && (
                <VideoPlayer
                  key={`${playingSourceId}-${current.index}-${current.link}`}
                  src={current.link}
                  filmId={currentFilm?.id}
                  filmTitle={currentFilm?.name}
                  episode={typeof current.index === "number" ? current.index + 1 : 1}
                  initialTime={playInitialTime}
                  autoplay={autoplay}
                  onEnded={() => autoplay && handlePlayNext()}
                  onTimeUpdate={handleTimeUpdate}
                  onPlayStart={handlePlayStart}
                  onError={handlePlayerError}
                  onVideoSize={handleVideoSize}
                />
              )
            )}
          </div>
        </div>

        <PlaySidebar
          filmName={currentFilm.name}
          currentEpisodeName={current?.episode}
          sources={currentFilm?.list}
          viewingSourceId={viewingSourceId}
          playingSourceId={playingSourceId}
          viewingSource={viewingSource}
          isSourceMenuOpen={isSourceMenuOpen}
          visibleActiveEpisodeKey={visibleActiveEpisodeKey}
          currentEpisodeKey={currentEpisodeKey}
          autoplay={autoplay}
          preferEnabled={preferEnabled}
          isProbing={isProbing}
          preferredSourceId={preferredSourceId}
          probeBySourceId={probeBySourceId}
          hasNext={hasNext}
          sourceMenuRef={sourceMenuRef}
          sourceTabsRef={sourceTabsRef}
          activeTabRef={activeTabRef}
          episodeListRef={episodeListRef}
          activeEpRef={activeEpRef}
          onToggleSourceMenu={() => setIsSourceMenuOpen((prev) => !prev)}
          onSelectViewingSource={(id) => {
            if (viewingSourceId !== id) {
              setViewingSourceId(id);
            }
            setIsSourceMenuOpen(false);
          }}
          onSelectEpisode={(sourceId, index, item) => {
            manualSourceLockRef.current = true;
            preferAbortRef.current?.abort();
            setIsProbing(false);
            setProbeReady(true);
            applyPlaybackSelection(sourceId, index, item);
            router.replace(buildPlayPath(String(filmId), sourceId, index), {
              scroll: false,
            });
          }}
          onToggleAutoplay={() => setAutoplay(!autoplay)}
          onTogglePrefer={handleTogglePrefer}
          onRetestPrefer={handleRetestPrefer}
          onPlayNext={handlePlayNext}
        />
      </div>

      <div className={styles.infoArea}>
        <div className={styles.introHeading}>剧情简介</div>
        <div className={styles.intro}>
          {currentFilm.descriptor.content
            ? currentFilm.descriptor.content.replace(/<[^>]+>/g, "").trim()
            : "暂无简介"}
        </div>
      </div>

      <RelatedFilmsSection
        filmId={filmId}
        initialList={data?.relate}
        onOpenPlayPage={handleOpenRelatedFilm}
      />
    </div>
  );
}
