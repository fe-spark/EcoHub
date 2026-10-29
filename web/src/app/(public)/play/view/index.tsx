"use client";

import React, { useState, useEffect, useRef, useCallback } from "react";
import { useRouter } from "next/navigation";
import { LoadingOutlined, StepForwardOutlined, PlayCircleOutlined } from "@ant-design/icons";
import VideoPlayer from "@/components/public/VideoPlayer";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";
import { buildPlayPath } from "@/lib/playNavigation";
import { trackPageView } from "@/lib/track-page-view";
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
  const [isSourceMenuOpen, setIsSourceMenuOpen] = useState(false);
  const [openingRelatedId, setOpeningRelatedId] = useState("");

  const activeEpRef = useRef<HTMLDivElement>(null);
  const activeTabRef = useRef<HTMLDivElement>(null);
  const sourceTabsRef = useRef<HTMLDivElement>(null);
  const episodeListRef = useRef<HTMLDivElement>(null);
  const sourceMenuRef = useRef<HTMLDivElement>(null);

  const currentFilm = data?.detail;
  const viewingSource = currentFilm?.list?.find((item: any) => item.id === viewingSourceId);
  const playingSource = currentFilm?.list?.find((item: any) => item.id === playingSourceId);

  const reportedPlaybackKeys = useRef<Set<string>>(new Set());
  const failedSourcesRef = useRef<Set<string>>(new Set());

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
      if (!playingSourceId || !currentFilm?.list || currentFilm.list.length === 0) {
        setPlayerError(true);
        message.error("该视频源加载失败，请尝试重新加载。");
        return;
      }

      failedSourcesRef.current.add(playingSourceId);

      // 寻找下一个未失败且包含播放地址的备用线路
      const nextSource = currentFilm.list.find(
        (item: any) =>
          item.id !== playingSourceId &&
          !failedSourcesRef.current.has(item.id) &&
          Array.isArray(item.linkList) &&
          item.linkList.length > 0,
      );

      if (nextSource) {
        const targetEpisodeIndex =
          current?.index !== undefined &&
          current.index >= 0 &&
          current.index < nextSource.linkList.length
            ? current.index
            : 0;
        const targetPlay = nextSource.linkList[targetEpisodeIndex];
        if (targetPlay) {
          message.warning(
            `播放源【${playingSource?.name || "当前源"}】连接失败，正在自动切换至【${nextSource.name}】...`,
          );
          applyPlaybackSelection(nextSource.id, targetEpisodeIndex, targetPlay, playInitialTime);
          router.replace(
            buildPlayPath(String(filmId), nextSource.id, targetEpisodeIndex),
            { scroll: false },
          );
          return;
        }
      }

      // 所有播放源均失败，展示播放器错误视图
      setPlayerError(true);
      message.error("该视频源加载失败，暂无其他可用播放源。");
    },
    [
      playingSourceId,
      currentFilm,
      current,
      playInitialTime,
      playingSource,
      applyPlaybackSelection,
      router,
      filmId,
      message,
    ],
  );

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

          <div className={`${styles.playerWrapper} ${playerError ? styles.isPlayerError : ""}`}>
            {current?.link && (
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
              />
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
            failedSourcesRef.current.delete(sourceId);
            applyPlaybackSelection(sourceId, index, item);
            router.replace(buildPlayPath(String(filmId), sourceId, index), {
              scroll: false,
            });
          }}
          onToggleAutoplay={() => setAutoplay(!autoplay)}
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
