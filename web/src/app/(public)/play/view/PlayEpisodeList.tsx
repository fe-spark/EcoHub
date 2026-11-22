"use client";

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  AppstoreOutlined,
  BarsOutlined,
  SortAscendingOutlined,
  SortDescendingOutlined,
} from "@ant-design/icons";
import { makeEpisodeKey } from "./formatters";
import styles from "./PlayEpisodeList.module.less";

interface PlayEpisodeListProps {
  viewingSourceId: string;
  linkList: any[];
  visibleActiveEpisodeKey: string;
  currentEpisodeKey: string;
  episodeListRef: React.RefObject<HTMLDivElement | null>;
  activeEpRef: React.RefObject<HTMLDivElement | null>;
  onSelectEpisode: (sourceId: string, index: number, item: any) => void;
}

/** 集数超过 RANGE_THRESHOLD 时按 RANGE_SIZE 集一段分页显示（1-50、51-100…），避免几百上千个按钮挤在一个列表里 */
const RANGE_THRESHOLD = 100;
const RANGE_SIZE = 50;

function parseEpisodeMeta(rawTitle: string, index: number) {
  const title = (rawTitle || "").trim();

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

export default function PlayEpisodeList({
  viewingSourceId,
  linkList,
  visibleActiveEpisodeKey,
  currentEpisodeKey,
  episodeListRef,
  activeEpRef,
  onSelectEpisode,
}: PlayEpisodeListProps) {
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

  const activeIndex = useMemo(() => {
    if (!visibleActiveEpisodeKey) return -1;
    return linkList.findIndex(
      (_: any, index: number) => makeEpisodeKey(viewingSourceId, index) === visibleActiveEpisodeKey,
    );
  }, [linkList, viewingSourceId, visibleActiveEpisodeKey]);

  const rangeCount = linkList.length > RANGE_THRESHOLD ? Math.ceil(linkList.length / RANGE_SIZE) : 1;
  const autoRange = activeIndex >= 0 ? Math.floor(activeIndex / RANGE_SIZE) : 0;
  // 手选段只跟片源和集数走。放进正在播的集，连播一集就会丢掉倒序跳到的最后一段。
  const rangeKey = `${viewingSourceId}:${linkList.length}`;
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
    <div className={styles.episodeSection}>
      <div className={styles.episodesToolbar}>
        <div className={styles.episodesToolbarLeft}>
          <span className={styles.episodesToolbarTitle}>选集</span>
          <span className={styles.episodesCount}>共 {linkList.length} 集</span>
        </div>
        <div className={styles.episodesToolbarRight}>
          <button
            type="button"
            className={`${styles.toolbarBtn} ${sortOrder === "desc" ? styles.toolbarBtnActive : ""}`}
            onClick={() => {
              const next = sortOrder === "asc" ? "desc" : "asc";
              if (rangeCount > 1) {
                // 倒序从最后一段开始，否则仍停在当前段，只把这一段倒过来。
                const range =
                  next === "desc"
                    ? rangeCount - 1
                    : activeIndex >= 0
                      ? Math.floor(activeIndex / RANGE_SIZE)
                      : 0;
                setPickedRange({ key: rangeKey, range });
              }
              setSortOrder(next);
              episodeListRef.current?.scrollTo({ top: 0 });
            }}
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
    </div>
  );
}
