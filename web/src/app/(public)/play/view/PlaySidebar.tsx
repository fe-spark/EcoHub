"use client";

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  PlayCircleOutlined,
  StepForwardOutlined,
  AppstoreOutlined,
  BarsOutlined,
  SortAscendingOutlined,
  SortDescendingOutlined,
  ThunderboltOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { makeEpisodeKey } from "./formatters";
import {
  formatProbeLabel,
  type SourceProbeResult,
} from "@/lib/playSourceProbe";
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

/** 集数超过 RANGE_THRESHOLD 时按 RANGE_SIZE 集一段分页显示（1-50、51-100…），避免几百上千个按钮挤在一个列表里 */
const RANGE_THRESHOLD = 100;
const RANGE_SIZE = 50;

function parseEpisodeMeta(rawTitle: string, index: number) {
  const title = (rawTitle || "").trim();

  // 匹配日期格式: 2024-01-06 或 2024.01.06 或 2024/01/06
  const dateMatch = title.match(/^(\d{4}[-/.年]\d{1,2}[-/.月]\d{1,2}[日]?)(.*)$/);
  if (dateMatch) {
    const badge = dateMatch[1].replace(/[年月]/g, "-").replace(/日/g, "");
    const rest = dateMatch[2].replace(/^[:：\s\-]+/, "").trim();
    return {
      badge,
      title: rest || badge,
      fullTitle: title,
    };
  }

  // 匹配 第XX集 / 第XX期 / EPXX
  const epMatch = title.match(/^(第?\s*\d+\s*[集期话回]|EP?\s*\d+)(.*)$/i);
  if (epMatch) {
    const badge = epMatch[1].trim();
    const rest = epMatch[2].replace(/^[:：\s\-]+/, "").trim();
    return {
      badge,
      title: rest || badge,
      fullTitle: title,
    };
  }

  return {
    badge: String(index + 1).padStart(2, "0"),
    title: title || `第 ${index + 1} 集`,
    fullTitle: title,
  };
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

  const renderProbeMeta = (sourceId: string) => {
    const result = probeBySourceId[sourceId];
    const label = formatProbeLabel(result, isProbing && !result);
    if (!label) return null;
    const isBest = preferredSourceId === sourceId && result?.status === "ok";
    const toneClass =
      result?.status === "ok"
        ? styles.probeOk
        : result?.status === "timeout" || result?.status === "unreachable"
          ? styles.probeBad
          : styles.probePending;
    return (
      <span className={`${styles.probeBadge} ${toneClass}`}>
        {isBest ? "优选 · " : ""}
        {label}
      </span>
    );
  };

  // 智能识别当前源是适合列表视图（综艺/长标题）还是网格视图（短集数/数字）
  const isLongTitle = useMemo(() => {
    if (!linkList.length) return false;
    const sample = linkList.slice(0, 10);
    const hasLong = sample.some((item: any) => (item?.episode?.length || 0) > 6);
    const avgLen =
      sample.reduce((acc: number, item: any) => acc + (item?.episode?.length || 0), 0) /
      sample.length;
    return hasLong || avgLen > 4.5;
  }, [linkList]);

  const [userSelectedMode, setUserSelectedMode] = useState<"grid" | "list" | null>(null);
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc");

  const viewMode = userSelectedMode ?? (isLongTitle ? "list" : "grid");

  // 当前播放的是本源第几集（看的不是正在播放的源时为 -1）
  const activeIndex = useMemo(() => {
    if (!visibleActiveEpisodeKey) return -1;
    return linkList.findIndex(
      (_: any, index: number) => makeEpisodeKey(viewingSourceId, index) === visibleActiveEpisodeKey,
    );
  }, [linkList, viewingSourceId, visibleActiveEpisodeKey]);

  // 分段：默认落在正在播放的那一段；用户手动切段后保持，直到换源或换集
  const rangeCount = linkList.length > RANGE_THRESHOLD ? Math.ceil(linkList.length / RANGE_SIZE) : 1;
  const autoRange = activeIndex >= 0 ? Math.floor(activeIndex / RANGE_SIZE) : 0;
  const rangeKey = `${viewingSourceId}:${activeIndex}:${linkList.length}`;
  const [pickedRange, setPickedRange] = useState<{ key: string; range: number } | null>(null);
  const rangeIndex = Math.min(
    rangeCount - 1,
    pickedRange && pickedRange.key === rangeKey ? pickedRange.range : autoRange,
  );
  const ranges = useMemo(() => {
    const list = Array.from({ length: rangeCount }, (_, i) => ({
      index: i,
      label: `${i * RANGE_SIZE + 1}-${Math.min((i + 1) * RANGE_SIZE, linkList.length)}`,
    }));
    return sortOrder === "desc" ? list.reverse() : list;
  }, [rangeCount, linkList.length, sortOrder]);

  const displayEpisodes = useMemo(() => {
    if (!linkList.length) return [];
    const start = rangeCount > 1 ? rangeIndex * RANGE_SIZE : 0;
    const end = rangeCount > 1 ? start + RANGE_SIZE : linkList.length;
    const listWithIndex = linkList.slice(start, end).map((item: any, i: number) => ({
      ...item,
      originalIndex: start + i,
    }));
    return sortOrder === "desc" ? [...listWithIndex].reverse() : listWithIndex;
  }, [linkList, sortOrder, rangeCount, rangeIndex]);

  // 选中的分段横向滚到可见处（只滚分段条自己，不带动整页）
  const rangeTabsRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const bar = rangeTabsRef.current;
    const tab = bar?.querySelector<HTMLElement>('[aria-selected="true"]');
    if (!bar || !tab) return;
    bar.scrollTo({ left: tab.offsetLeft - bar.clientWidth / 2 + tab.offsetWidth / 2 });
  }, [rangeIndex, rangeCount, sortOrder]);

  const selectRange = (index: number) => {
    setPickedRange({ key: rangeKey, range: index });
    episodeListRef.current?.scrollTo({ top: 0 });
  };

  return (
    <div className={styles.sidebarWrapper}>
      <div className={styles.sidebar}>
        <div className={styles.sideHeader}>
          <div className={styles.title}>正在播放</div>
          <div className={styles.subtitle} title={`${filmName} - ${currentEpisodeName || ""}`}>
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
              {viewingSourceId ? (
                <span className={styles.sourcePickerProbe}>
                  {renderProbeMeta(viewingSourceId)}
                </span>
              ) : null}
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
                      {renderProbeMeta(item.id)}
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
            const probeLabel = formatProbeLabel(
              probeBySourceId[item.id],
              isProbing && !probeBySourceId[item.id],
            );
            return (
              <div
                key={item.id}
                ref={isActive ? activeTabRef : null}
                className={`${styles.tab} ${isActive ? styles.active : ""}`}
                onClick={() => onSelectViewingSource(item.id)}
                title={probeLabel ? `${item.name} · ${probeLabel}` : item.name}
              >
                {item.name}
                {item.proxy && (
                  <span className={styles.proxyBadge}>代理</span>
                )}
                {preferredSourceId === item.id && preferEnabled ? (
                  <span className={styles.preferMark}>优</span>
                ) : null}
              </div>
            );
          })}
        </div>

        <div className={styles.episodesToolbar}>
          <div className={styles.episodesToolbarLeft}>
            <span className={styles.episodesToolbarTitle}>选集</span>
            <span className={styles.episodesCount}>共 {linkList.length} 集</span>
          </div>
          <div className={styles.episodesToolbarRight}>
            <button
              type="button"
              className={`${styles.toolbarBtn} ${sortOrder === "desc" ? styles.toolbarBtnActive : ""}`}
              onClick={() => setSortOrder((prev) => (prev === "asc" ? "desc" : "asc"))}
              title={sortOrder === "desc" ? "倒序" : "正序"}
            >
              {sortOrder === "desc" ? <SortDescendingOutlined /> : <SortAscendingOutlined />}
              <span>{sortOrder === "desc" ? "倒序" : "正序"}</span>
            </button>
            <div className={styles.viewModeSwitch}>
              <button
                type="button"
                className={`${styles.viewBtn} ${viewMode === "grid" ? styles.viewBtnActive : ""}`}
                onClick={() => setUserSelectedMode("grid")}
                title="网格"
              >
                <AppstoreOutlined />
              </button>
              <button
                type="button"
                className={`${styles.viewBtn} ${viewMode === "list" ? styles.viewBtnActive : ""}`}
                onClick={() => setUserSelectedMode("list")}
                title="列表"
              >
                <BarsOutlined />
              </button>
            </div>
          </div>
        </div>

        {rangeCount > 1 && (
          <div className={styles.rangeTabs} role="tablist" aria-label="选集分段" ref={rangeTabsRef}>
            {ranges.map((range) => (
              <button
                key={range.index}
                type="button"
                role="tab"
                aria-selected={range.index === rangeIndex}
                className={`${styles.rangeTab} ${range.index === rangeIndex ? styles.active : ""} ${
                  range.index === autoRange && activeIndex >= 0 ? styles.playing : ""
                }`}
                onClick={() => selectRange(range.index)}
              >
                {range.label}
              </button>
            ))}
          </div>
        )}

        <div
          className={`${styles.episodeList} ${viewMode === "list" ? styles.listMode : styles.gridMode}`}
          ref={episodeListRef}
        >
          {displayEpisodes.map((item: any) => {
            const episodeKey = makeEpisodeKey(viewingSourceId, item.originalIndex);
            const isActive = visibleActiveEpisodeKey === episodeKey;

            if (viewMode === "list") {
              const meta = parseEpisodeMeta(item.episode, item.originalIndex);
              return (
                <div
                  key={item.originalIndex}
                  ref={isActive ? activeEpRef : undefined}
                  className={`${styles.epListItem} ${isActive ? styles.active : ""}`}
                  title={meta.fullTitle}
                  onClick={() => {
                    if (currentEpisodeKey === episodeKey) return;
                    onSelectEpisode(viewingSourceId, item.originalIndex, item);
                  }}
                >
                  <span className={styles.epListBadge}>{meta.badge}</span>
                  <div className={styles.epListMain}>
                    <span className={styles.epListTitle}>{meta.title}</span>
                  </div>
                  <div className={styles.epStatusArea}>
                    {isActive ? (
                      <div className={styles.waveBars} aria-hidden="true">
                        <span className={styles.waveBar} />
                        <span className={styles.waveBar} />
                        <span className={styles.waveBar} />
                      </div>
                    ) : null}
                  </div>
                </div>
              );
            }

            return (
              <div
                key={item.originalIndex}
                ref={isActive ? activeEpRef : undefined}
                className={`${styles.epGridItem} ${isActive ? styles.active : ""}`}
                title={item.episode}
                onClick={() => {
                  if (currentEpisodeKey === episodeKey) return;
                  onSelectEpisode(viewingSourceId, item.originalIndex, item);
                }}
              >
                <span className={styles.epGridText}>{item.episode}</span>
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
          <div
            className={`${styles.footerBtn} ${preferEnabled ? styles.active : ""}`}
            onClick={onTogglePrefer}
          >
            <ThunderboltOutlined />
            <span>{preferEnabled ? "测速优选 开" : "测速优选 关"}</span>
          </div>
          {preferEnabled && (
            <div
              className={`${styles.footerBtn} ${isProbing ? styles.disabled : ""}`}
              onClick={() => {
                if (!isProbing) onRetestPrefer();
              }}
            >
              <ReloadOutlined spin={isProbing} />
              <span>{isProbing ? "测速中" : "重新测速"}</span>
            </div>
          )}
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
