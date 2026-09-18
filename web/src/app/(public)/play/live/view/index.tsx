"use client";

import React, { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { PlayCircleOutlined, StepForwardOutlined } from "@ant-design/icons";
import VideoPlayer from "@/components/public/VideoPlayer";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";
import { buildLivePlayPath, livePlayHistoryID } from "@/lib/playNavigation";
import styles from "./index.module.less";

function parseInitialTimeParam(value?: string): number {
  if (!value) {
    return 0;
  }
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function formatActorNames(value?: string) {
  const raw = String(value || "").trim();
  if (!raw) {
    return "暂无";
  }
  return raw.replace(/\s*[，,、]\s*/g, " / ");
}

export default function LivePlayView({
  data,
  sourceId,
  sid,
  initialTime,
  emptyMessage,
}: {
  data: any;
  sourceId: string;
  sid: string;
  initialTime?: string;
  emptyMessage?: string;
}) {
  const router = useRouter();
  const { message } = useAppMessage();
  const detail = data?.detail;
  const playList: any[] = Array.isArray(detail?.list) ? detail.list : [];
  const defaultSourceId = data?.currentPlayFrom || playList[0]?.id || "";
  const defaultIndex = Number(data?.currentEpisode) || 0;

  const [playingSourceId, setPlayingSourceId] = useState(defaultSourceId);
  const [episodeIndex, setEpisodeIndex] = useState(defaultIndex);
  const [playInitialTime, setPlayInitialTime] = useState(parseInitialTimeParam(initialTime));
  const [autoplay, setAutoplay] = useState(true);
  const [playerError, setPlayerError] = useState(false);

  const playingSource = playList.find((item) => item.id === playingSourceId) || playList[0];
  const episodes: any[] = playingSource?.linkList || [];
  const current = episodes[episodeIndex] || episodes[0];
  const hasNext = episodeIndex < episodes.length - 1;
  const remarks = String(detail?.descriptor?.remarks || "").trim();
  const metaChips = [detail?.descriptor?.cName, detail?.descriptor?.year, detail?.descriptor?.area].filter(Boolean);

  const selectEpisode = (source: string, index: number, resumeTime = 0) => {
    setPlayingSourceId(source);
    setEpisodeIndex(index);
    setPlayInitialTime(resumeTime);
    setPlayerError(false);
    router.replace(buildLivePlayPath(sourceId, sid, index), { scroll: false });
  };

  const persistHistory = useCallback(
    (currentTime?: number, duration?: number) => {
      if (!detail?.name || !current) {
        return;
      }
      const historyKey = livePlayHistoryID(sourceId, sid);
      if (!historyKey) {
        return;
      }
      const historyMap = readHistoryMap();
      const previous = historyMap[historyKey];
      historyMap[historyKey] = {
        ...(previous ?? {}),
        id: historyKey,
        name: detail.name,
        picture: detail.picture,
        sourceId: playingSource?.id || sourceId,
        episodeIndex,
        sourceName: playingSource?.name || "采集源",
        episode: current.episode || "正在观看",
        timeStamp: Date.now(),
        link: buildLivePlayPath(sourceId, sid, episodeIndex, currentTime),
        currentTime: typeof currentTime === "number" ? currentTime : previous?.currentTime || 0,
        duration: typeof duration === "number" ? duration : previous?.duration || 0,
        devices: window.innerWidth <= 768,
      };
      writeHistoryMap(historyMap);
    },
    [detail, current, sourceId, sid, playingSource, episodeIndex],
  );

  useEffect(() => {
    persistHistory();
  }, [persistHistory]);

  if (!data || !detail) {
    return (
      <div className={styles.emptyPage}>
        <div className={styles.emptyCard}>
          <div className={styles.emptyEyebrow}>Live Play</div>
          <h1 className={styles.emptyTitle}>当前内容无法播放</h1>
          <p className={styles.emptyDescription}>{emptyMessage || "未获取到采集源播放地址。"}</p>
          <div className={styles.emptyActions}>
            <button type="button" className={styles.emptyPrimaryAction} onClick={() => router.back()}>
              返回搜索
            </button>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.container}>
      <div className={styles.bgWrapper}>
        {/* eslint-disable-next-line @next/next/no-img-element */}
        {detail.picture ? <img src={detail.picture} className={styles.bgPoster} alt="" /> : null}
        <div className={styles.mask} />
      </div>

      <div className={styles.mainContent}>
        <div className={styles.leftColumn}>
          <div className={styles.topInfoCard}>
            <div className={styles.titleMain}>
              <h1 className={styles.filmTitle}>{detail.name}</h1>
              {remarks ? <span className={styles.statusTag}>{remarks}</span> : null}
            </div>
            {metaChips.length > 0 && (
              <div className={styles.metaChips}>
                {metaChips.map((chip: string) => (
                  <span key={chip} className={styles.metaChip}>
                    {chip}
                  </span>
                ))}
              </div>
            )}
            <div className={styles.actorRow}>
              <span className={styles.actorLabel}>主演</span>
              <span className={styles.actorValue}>{formatActorNames(detail.descriptor?.actor)}</span>
            </div>
          </div>

          <div className={`${styles.playerWrapper} ${playerError ? styles.isPlayerError : ""}`}>
            {current?.link ? (
              <VideoPlayer
                key={current.link}
                src={current.link}
                initialTime={playInitialTime}
                autoplay={autoplay}
                onEnded={() => {
                  if (autoplay && hasNext) {
                    selectEpisode(playingSourceId, episodeIndex + 1);
                  }
                }}
                onTimeUpdate={(time, duration) => persistHistory(time, duration)}
                onError={() => {
                  setPlayerError(true);
                  message.error("当前地址无法播放，请稍后重试或返回搜索。");
                }}
              />
            ) : (
              <div className={styles.playerEmpty}>暂无播放地址</div>
            )}
          </div>
        </div>

        <aside className={styles.sidebar}>
          <div className={styles.sideHeader}>
            <div className={styles.sideTitle}>选集</div>
            <div className={styles.sideSubtitle}>
              {detail.name}
              {current?.episode ? ` · ${current.episode}` : ""}
            </div>
          </div>

          {playList.length > 1 && (
            <div className={styles.lineBar} aria-label="播放线路">
              {playList.map((item: any) => (
                <button
                  type="button"
                  key={item.id}
                  className={`${styles.lineChip} ${item.id === playingSourceId ? styles.active : ""}`}
                  onClick={() => selectEpisode(item.id, 0)}
                >
                  {item.name}
                </button>
              ))}
            </div>
          )}

          <div className={styles.episodeList}>
            {episodes.map((item: any, index: number) => (
              <button
                type="button"
                key={`${playingSourceId}:${index}`}
                className={`${styles.epItem} ${index === episodeIndex ? styles.active : ""}`}
                title={item.episode}
                onClick={() => {
                  if (index !== episodeIndex) {
                    selectEpisode(playingSourceId, index);
                  }
                }}
              >
                {item.episode || `第${index + 1}集`}
              </button>
            ))}
          </div>

          <div className={styles.sideFooter}>
            <button
              type="button"
              className={`${styles.footerBtn} ${autoplay ? styles.active : ""}`}
              onClick={() => setAutoplay((prev) => !prev)}
            >
              <PlayCircleOutlined />
              <span>{autoplay ? "自动播放 开" : "自动播放 关"}</span>
            </button>
            {hasNext && (
              <button
                type="button"
                className={styles.footerBtn}
                onClick={() => selectEpisode(playingSourceId, episodeIndex + 1)}
              >
                <StepForwardOutlined />
                <span>下一集</span>
              </button>
            )}
          </div>
        </aside>
      </div>

      <section className={styles.infoArea}>
        <h2 className={styles.introHeading}>剧情简介</h2>
        <p className={styles.intro}>
          {detail.descriptor?.content
            ? String(detail.descriptor.content).replace(/<[^>]+>/g, "").trim()
            : "暂无简介"}
        </p>
      </section>
    </div>
  );
}
