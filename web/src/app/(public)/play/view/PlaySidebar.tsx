"use client";

import React from "react";
import { PlayCircleOutlined, StepForwardOutlined } from "@ant-design/icons";
import { makeEpisodeKey } from "./formatters";
import styles from "./index.module.less";

interface PlaySidebarProps {
  filmName: string;
  currentEpisodeName?: string;
  sources: any[];
  viewingSourceId: string;
  playingSourceId: string;
  viewingSource: any;
  isSourceMenuOpen: boolean;
  visibleActiveEpisodeKey: string;
  currentEpisodeKey: string;
  autoplay: boolean;
  hasNext: boolean;
  sourceMenuRef: React.RefObject<HTMLDivElement | null>;
  sourceTabsRef: React.RefObject<HTMLDivElement | null>;
  activeTabRef: React.RefObject<HTMLDivElement | null>;
  episodeListRef: React.RefObject<HTMLDivElement | null>;
  activeEpRef: React.RefObject<HTMLDivElement | null>;
  onToggleSourceMenu: () => void;
  onSelectViewingSource: (id: string) => void;
  onSelectEpisode: (sourceId: string, index: number, item: any) => void;
  onToggleAutoplay: () => void;
  onPlayNext: () => void;
}

export default function PlaySidebar({
  filmName,
  currentEpisodeName,
  sources,
  viewingSourceId,
  playingSourceId,
  viewingSource,
  isSourceMenuOpen,
  visibleActiveEpisodeKey,
  currentEpisodeKey,
  autoplay,
  hasNext,
  sourceMenuRef,
  sourceTabsRef,
  activeTabRef,
  episodeListRef,
  activeEpRef,
  onToggleSourceMenu,
  onSelectViewingSource,
  onSelectEpisode,
  onToggleAutoplay,
  onPlayNext,
}: PlaySidebarProps) {
  return (
    <div className={styles.sidebarWrapper}>
      <div className={styles.sidebar}>
        <div className={styles.sideHeader}>
          <div className={styles.title}>正在播放</div>
          <div className={styles.subtitle}>
            {filmName} - {currentEpisodeName}
          </div>
        </div>

        <div className={styles.sourcePicker} ref={sourceMenuRef}>
          <button
            type="button"
            className={`${styles.sourcePickerTrigger} ${isSourceMenuOpen ? styles.open : ""}`}
            aria-expanded={isSourceMenuOpen}
            onClick={onToggleSourceMenu}
          >
            <div className={styles.sourcePickerMeta}>
              <span className={styles.sourcePickerLabel}>播放源</span>
              <span className={styles.sourcePickerValue}>
                {viewingSource?.name || "选择播放源"}
                {viewingSource?.proxy && (
                  <span className={styles.proxyBadge}>代理</span>
                )}
              </span>
            </div>
            <span className={styles.sourcePickerArrow} aria-hidden="true" />
          </button>

          {isSourceMenuOpen && (
            <div className={styles.sourcePickerMenu}>
              {sources?.map((item: any) => {
                const isViewing = viewingSourceId === item.id;
                const isPlaying = playingSourceId === item.id;
                const episodeCount = item.linkList?.length ?? 0;

                return (
                  <div
                    key={item.id}
                    className={`${styles.sourcePickerOption} ${isViewing ? styles.active : ""}`}
                    onClick={() => onSelectViewingSource(item.id)}
                  >
                    <span className={styles.sourcePickerOptionMain}>
                      {item.name}
                      {item.proxy && (
                        <span className={styles.proxyBadge}>代理</span>
                      )}
                    </span>
                    <div className={styles.sourcePickerOptionMeta}>
                      {isPlaying && <span className={styles.playingBadge}>正在播放</span>}
                      <span>{episodeCount} 集</span>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
        <div className={styles.sourcePickerDivider} />

        <div className={styles.sourceTabs} ref={sourceTabsRef}>
          {sources?.map((item: any) => {
            const isActive = viewingSourceId === item.id;
            return (
              <div
                key={item.id}
                ref={isActive ? activeTabRef : null}
                className={`${styles.tab} ${isActive ? styles.active : ""}`}
                onClick={() => onSelectViewingSource(item.id)}
              >
                {item.name}
                {item.proxy && (
                  <span className={styles.proxyBadge}>代理</span>
                )}
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
                  onSelectEpisode(viewingSourceId, index, item);
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
            onClick={onToggleAutoplay}
          >
            <PlayCircleOutlined />
            <span>{autoplay ? "自动播放 开" : "自动播放 关"}</span>
          </div>
          {hasNext && (
            <div className={styles.footerBtn} onClick={onPlayNext}>
              <StepForwardOutlined />
              <span>下一集</span>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
