"use client";

import React, { useMemo } from "react";
import { PlayCircleOutlined, StepForwardOutlined } from "@ant-design/icons";
import type { SourceProbeResult } from "@/lib/playSourceProbe";
import PlaySourcePicker from "./PlaySourcePicker";
import PlayEpisodeList from "./PlayEpisodeList";
import styles from "./PlaySidebar.module.less";

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
  preferEnabled: boolean;
  isProbing: boolean;
  probingSourceIds: string[];
  preferredSourceId?: string;
  probeBySourceId: Record<string, SourceProbeResult>;
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
  onTogglePrefer: () => void;
  onRetestPrefer: () => void;
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
  preferEnabled,
  isProbing,
  probingSourceIds,
  preferredSourceId,
  probeBySourceId,
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
  onTogglePrefer,
  onRetestPrefer,
  onPlayNext,
}: PlaySidebarProps) {
  const linkList: any[] = useMemo(() => {
    return Array.isArray(viewingSource?.linkList) ? viewingSource.linkList : [];
  }, [viewingSource]);

  return (
    <div className={styles.sidebarWrapper}>
      <div className={styles.sidebar}>
        <div className={styles.sideHeader} title={currentEpisodeName ? `${filmName} - ${currentEpisodeName}` : filmName}>
          <div className={styles.title}>正在播放</div>
          {currentEpisodeName ? (
            <span className={styles.currentEpisodeBadge}>{currentEpisodeName}</span>
          ) : null}
        </div>

        <PlaySourcePicker
          sources={sources}
          viewingSourceId={viewingSourceId}
          playingSourceId={playingSourceId}
          viewingSource={viewingSource}
          isSourceMenuOpen={isSourceMenuOpen}
          preferEnabled={preferEnabled}
          isProbing={isProbing}
          probingSourceIds={probingSourceIds}
          preferredSourceId={preferredSourceId}
          probeBySourceId={probeBySourceId}
          sourceMenuRef={sourceMenuRef}
          sourceTabsRef={sourceTabsRef}
          activeTabRef={activeTabRef}
          onToggleSourceMenu={onToggleSourceMenu}
          onSelectViewingSource={onSelectViewingSource}
          onTogglePrefer={onTogglePrefer}
          onRetestPrefer={onRetestPrefer}
        />

        <PlayEpisodeList
          viewingSourceId={viewingSourceId}
          linkList={linkList}
          visibleActiveEpisodeKey={visibleActiveEpisodeKey}
          currentEpisodeKey={currentEpisodeKey}
          episodeListRef={episodeListRef}
          activeEpRef={activeEpRef}
          onSelectEpisode={onSelectEpisode}
        />

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
