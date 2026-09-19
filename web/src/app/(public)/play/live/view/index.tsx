"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import {
  StepForwardOutlined,
  PlayCircleOutlined,
  ExclamationCircleOutlined,
  DatabaseOutlined,
} from "@ant-design/icons";
import VideoPlayer from "@/components/public/VideoPlayer";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap, writeHistoryMap } from "@/lib/historyStorage";
import { buildLivePlayPath, livePlayHistoryID } from "@/lib/playNavigation";
import LiveRelatedFilmsSection from "./LiveRelatedFilmsSection";
import styles from "./index.module.less";

function parseInitialTimeParam(value?: string): number {
  if (!value) return 0;
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

function formatPeople(value?: string) {
  const raw = String(value || "").trim();
  if (!raw) return "";
  return raw.replace(/\s*[，,、]\s*/g, " / ");
}

function stripHtml(value?: string) {
  return String(value || "")
    .replace(/<[^>]+>/g, "")
    .trim();
}

interface LivePlayViewProps {
  data: any;
  sourceId: string;
  sid: string;
  initialTime?: string;
  emptyMessage?: string;
}

function recordLivePlayHistory(
  detail: any,
  sourceId: string,
  sid: string,
  playingSource: any,
  safeIndex: number,
  current: any,
  currentTime?: number,
  duration?: number,
) {
  if (!detail?.name || !current) return;
  const historyKey = livePlayHistoryID(sourceId, sid);
  if (!historyKey) return;

  const historyMap = readHistoryMap();
  const previous = historyMap[historyKey];
  historyMap[historyKey] = {
    ...(previous ?? {}),
    id: historyKey,
    name: detail.name,
    picture: detail.picture,
    sourceId: playingSource?.id || sourceId,
    episodeIndex: safeIndex,
    sourceName: playingSource?.name || "采集源",
    episode: current.episode || "正在观看",
    timeStamp: Date.now(),
    link: buildLivePlayPath(sourceId, sid, safeIndex, currentTime),
    currentTime: typeof currentTime === "number" ? currentTime : previous?.currentTime || 0,
    duration: typeof duration === "number" ? duration : previous?.duration || 0,
    devices: typeof window !== "undefined" ? window.innerWidth <= 768 : false,
  };
  writeHistoryMap(historyMap);
}

export default function LivePlayView({
  data,
  sourceId,
  sid,
  initialTime,
  emptyMessage,
}: LivePlayViewProps) {
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
  const episodes: any[] = Array.isArray(playingSource?.linkList) ? playingSource.linkList : [];
  const safeIndex = Math.min(Math.max(episodeIndex, 0), Math.max(episodes.length - 1, 0));
  const current = episodes[safeIndex] || episodes[0];
  const hasNext = safeIndex < episodes.length - 1;

  const remarks = String(detail?.descriptor?.remarks || "").trim();
  const actorText = formatPeople(detail?.descriptor?.actor);
  const directorText = formatPeople(detail?.descriptor?.director);
  const intro = stripHtml(detail?.descriptor?.content || detail?.descriptor?.blurb);

  const metaChips = [
    detail?.descriptor?.cName,
    detail?.descriptor?.year,
    detail?.descriptor?.area,
  ].filter(Boolean);

  const sourceDisplayName =
    playingSource?.name && playingSource.name !== "默认源"
      ? playingSource.name
      : (sourceId || playingSource?.name || "采集源");

  const selectEpisode = (source: string, index: number, resumeTime = 0) => {
    setPlayingSourceId(source);
    setEpisodeIndex(index);
    setPlayInitialTime(resumeTime);
    setPlayerError(false);
    router.replace(buildLivePlayPath(sourceId, sid, index), { scroll: false });
  };

  const handleBack = () => {
    router.push("/search");
  };

  useEffect(() => {
    recordLivePlayHistory(detail, sourceId, sid, playingSource, safeIndex, current);
  }, [detail, sourceId, sid, playingSource, safeIndex, current]);

  const handleTimeUpdate = (currentTime: number, duration: number) => {
    recordLivePlayHistory(
      detail,
      sourceId,
      sid,
      playingSource,
      safeIndex,
      current,
      currentTime,
      duration,
    );
  };

  const activeEpRef = useRef<HTMLButtonElement | null>(null);

  useEffect(() => {
    if (activeEpRef.current) {
      activeEpRef.current.scrollIntoView({
        block: "nearest",
        behavior: "smooth",
      });
    }
  }, [safeIndex, playingSourceId]);

  if (!data || !detail) {
    return (
      <div className={styles.emptyContainer}>
        <div className={styles.emptyCard}>
          <div className={styles.emptyIconWrap}>
            <ExclamationCircleOutlined className={styles.emptyIcon} />
          </div>
          <h1 className={styles.emptyTitle}>当前内容无法播放</h1>
          <p className={styles.emptyDesc}>
            {emptyMessage || "未能从采集源获取到有效的播放信息，请返回搜索更换关键词或稍后再试。"}
          </p>
          <button type="button" className={styles.emptyBtn} onClick={handleBack}>
            返回搜索
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className={styles.liveContainer}>
      {/* 顶部：标题与元数据 */}
      <section className={styles.topHeaderCard}>
        <div className={styles.titleRow}>
          <div className={styles.titleMain}>
            <h1 className={styles.titleText}>{detail.name}</h1>
            {current?.episode && (
              <span className={styles.currentEpTag}>{current.episode}</span>
            )}
            {remarks && remarks !== current?.episode ? (
              <span className={styles.remarksTag}>{remarks}</span>
            ) : null}
          </div>

          {sourceDisplayName && (
            <div className={styles.sourceBadge} title={`当前采集源：${sourceDisplayName}`}>
              <DatabaseOutlined className={styles.sourceIcon} />
              <span className={styles.sourceNameText}>{sourceDisplayName}</span>
            </div>
          )}
        </div>

        <div className={styles.metaRow}>
          {metaChips.length > 0 && (
            <div className={styles.chips}>
              {metaChips.map((chip: string) => (
                <span key={chip} className={styles.chip}>
                  {chip}
                </span>
              ))}
            </div>
          )}
          {directorText && (
            <div className={styles.personItem}>
              <span className={styles.personLabel}>导演：</span>
              <span className={styles.personVal}>{directorText}</span>
            </div>
          )}
          {actorText && (
            <div className={styles.personItem}>
              <span className={styles.personLabel}>主演：</span>
              <span className={styles.personVal}>{actorText}</span>
            </div>
          )}
        </div>
      </section>

      {/* 中间：左右等高分栏（左播放器，右选集） */}
      <div className={styles.mainLayout}>
        {/* 左侧：播放器 */}
        <div className={styles.playerCol}>
          <div className={`${styles.playerStage} ${playerError ? styles.hasError : ""}`}>
            {current?.link ? (
              <VideoPlayer
                key={current.link}
                src={current.link}
                poster={detail.picture}
                initialTime={playInitialTime}
                autoplay={autoplay}
                onEnded={() => {
                  if (autoplay && hasNext) {
                    selectEpisode(playingSourceId, safeIndex + 1);
                  }
                }}
                onTimeUpdate={handleTimeUpdate}
                onError={() => {
                  setPlayerError(true);
                  message.error("当前地址无法播放，请稍后重试或尝试切换线路。");
                }}
              />
            ) : (
              <div className={styles.emptyStream}>
                <ExclamationCircleOutlined className={styles.emptyStreamIcon} />
                <p>暂无播放地址</p>
              </div>
            )}
          </div>
        </div>

        {/* 右侧：选集与快捷播控（与左侧播放器等高对齐） */}
        <aside className={styles.episodesCol}>
          <div className={styles.sideCard}>
            <div className={styles.sideHeader}>
              <div className={styles.sideTitleGroup}>
                <span className={styles.sideTitle}>选集</span>
                <span className={styles.sideCount}>共 {episodes.length} 集</span>
              </div>

              <div className={styles.sideActions}>
                <button
                  type="button"
                  role="switch"
                  aria-checked={autoplay}
                  className={`${styles.autoplayToggle} ${autoplay ? styles.active : ""}`}
                  onClick={() => setAutoplay((prev) => !prev)}
                  title={autoplay ? "已开启自动连播" : "已关闭自动连播"}
                >
                  <PlayCircleOutlined />
                  <span className={styles.toggleText}>连播</span>
                  <span className={styles.toggleSwitch} aria-hidden="true" />
                </button>
              </div>
            </div>

            {/* 线路切换胶囊（仅在有多条线路时展示） */}
            {playList.length > 1 && (
              <div className={styles.linesBar} aria-label="播放线路">
                <div className={styles.linesList}>
                  {playList.map((item: any) => {
                    const isActive = item.id === playingSourceId;
                    return (
                      <button
                        type="button"
                        key={item.id}
                        className={`${styles.lineBtn} ${isActive ? styles.active : ""}`}
                        onClick={() => {
                          if (isActive) return;
                          const nextList = Array.isArray(item.linkList) ? item.linkList : [];
                          const nextIndex = Math.min(safeIndex, Math.max(nextList.length - 1, 0));
                          selectEpisode(item.id, nextIndex);
                        }}
                      >
                        {item.name}
                      </button>
                    );
                  })}
                </div>
              </div>
            )}

            {/* 选集网格滚动区 */}
            <div className={styles.episodesScrollArea}>
              {episodes.length === 0 ? (
                <div className={styles.episodesEmpty}>暂无可选剧集</div>
              ) : (
                <div className={styles.epGrid}>
                  {episodes.map((item: any, index: number) => {
                    const isActive = index === safeIndex;
                    const label = item.episode || `第${index + 1}集`;
                    return (
                      <button
                        type="button"
                        key={`${playingSourceId}:${index}`}
                        ref={isActive ? activeEpRef : null}
                        className={`${styles.epBtn} ${isActive ? styles.active : ""}`}
                        title={label}
                        onClick={() => {
                          if (!isActive) {
                            selectEpisode(playingSourceId, index);
                          }
                        }}
                      >
                        <span className={styles.epBtnText}>{label}</span>
                        {isActive && (
                          <span className={styles.playingIndicator} aria-hidden="true" />
                        )}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>

            {/* 底部下一集按钮 */}
            {hasNext && (
              <div className={styles.sideFooter}>
                <button
                  type="button"
                  className={styles.nextBtn}
                  onClick={() => selectEpisode(playingSourceId, safeIndex + 1)}
                  title="播放下一集"
                >
                  <StepForwardOutlined />
                  <span>播放下一集</span>
                </button>
              </div>
            )}
          </div>
        </aside>
      </div>

      {/* 底部：剧情简介区（放左右分栏下方） */}
      {intro && (
        <section className={styles.introCard}>
          <h2 className={styles.introHeading}>剧情简介</h2>
          <p className={styles.introContent}>{intro}</p>
        </section>
      )}

      {/* 底部：同类相关推荐 */}
      <LiveRelatedFilmsSection
        sourceId={sourceId}
        sid={sid}
        cid={detail?.rawCid || detail?.cid}
      />
    </div>
  );
}
