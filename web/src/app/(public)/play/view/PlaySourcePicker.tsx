"use client";

import React from "react";
import { ReloadOutlined } from "@ant-design/icons";
import { formatProbeLabel, type SourceProbeResult } from "@/lib/playSourceProbe";
import styles from "./PlaySourcePicker.module.less";

interface PlaySourcePickerProps {
  sources: any[];
  viewingSourceId: string;
  playingSourceId: string;
  viewingSource: any;
  isSourceMenuOpen: boolean;
  preferEnabled: boolean;
  isProbing: boolean;
  probingSourceIds: string[];
  preferredSourceId?: string;
  probeBySourceId: Record<string, SourceProbeResult>;
  sourceMenuRef: React.RefObject<HTMLDivElement | null>;
  sourceTabsRef: React.RefObject<HTMLDivElement | null>;
  activeTabRef: React.RefObject<HTMLDivElement | null>;
  onToggleSourceMenu: () => void;
  onSelectViewingSource: (id: string) => void;
  onTogglePrefer: () => void;
  onRetestPrefer: () => void;
}

export default function PlaySourcePicker({
  sources,
  viewingSourceId,
  playingSourceId,
  viewingSource,
  isSourceMenuOpen,
  preferEnabled,
  isProbing,
  probingSourceIds,
  preferredSourceId,
  probeBySourceId,
  sourceMenuRef,
  sourceTabsRef,
  activeTabRef,
  onToggleSourceMenu,
  onSelectViewingSource,
  onTogglePrefer,
  onRetestPrefer,
}: PlaySourcePickerProps) {
  const probeOf = (sourceId: string) => {
    const result = probeBySourceId[sourceId];
    const label = formatProbeLabel(result, probingSourceIds.includes(sourceId) && !result);
    if (!label) return null;
    const isBest = preferredSourceId === sourceId && result?.status === "ok";
    const toneClass = isBest
      ? styles.toneBest
      : result?.status === "ok"
        ? styles.toneOk
        : result?.status === "timeout" || result?.status === "unreachable"
          ? styles.toneBad
          : styles.tonePending;
    return { label, isBest, toneClass };
  };

  const viewingProbe = viewingSourceId ? probeOf(viewingSourceId) : null;

  return (
    <div className={styles.sourceBlock}>
      <div className={styles.sourceHead}>
        <span className={styles.sourceHeadTitle}>播放源</span>
        <div className={styles.sourceHeadActions}>
          <span className={styles.preferLabel}>测速优选</span>
          <button
            type="button"
            className={`${styles.preferSwitch} ${preferEnabled ? styles.on : ""}`}
            role="switch"
            aria-checked={preferEnabled}
            aria-label="测速优选"
            onClick={onTogglePrefer}
          >
            <span className={styles.preferKnob} />
          </button>
          {preferEnabled ? (
            <button
              type="button"
              className={styles.retestBtn}
              disabled={isProbing}
              title={isProbing ? "测速中" : "重新测速"}
              aria-label={isProbing ? "测速中" : "重新测速"}
              onClick={onRetestPrefer}
            >
              <ReloadOutlined spin={isProbing} />
            </button>
          ) : null}
        </div>
      </div>

      <div className={styles.sourcePicker} ref={sourceMenuRef}>
        <button
          type="button"
          className={`${styles.sourcePickerTrigger} ${isSourceMenuOpen ? styles.open : ""}`}
          aria-expanded={isSourceMenuOpen}
          onClick={onToggleSourceMenu}
        >
          <span className={styles.sourcePickerValue}>
            {viewingSource?.name || "选择播放源"}
            {viewingSource?.proxy && <span className={styles.proxyBadge}>代理</span>}
          </span>
          <span className={styles.sourcePickerAside}>
            {viewingProbe ? (
              <span className={`${styles.sourceSpeed} ${viewingProbe.toneClass}`}>
                {viewingProbe.label}
              </span>
            ) : null}
            <span className={styles.sourcePickerArrow} aria-hidden="true" />
          </span>
        </button>

        {isSourceMenuOpen && (
          <div className={styles.sourcePickerMenu}>
            {sources?.map((item: any) => {
              const isViewing = viewingSourceId === item.id;
              const isPlaying = playingSourceId === item.id;
              const episodeCount = item.linkList?.length ?? 0;
              const probe = probeOf(item.id);

              return (
                <div
                  key={item.id}
                  className={`${styles.sourcePickerOption} ${isViewing ? styles.active : ""}`}
                  onClick={() => onSelectViewingSource(item.id)}
                >
                  <span className={styles.sourcePickerOptionBody}>
                    <span className={styles.sourcePickerOptionMain}>
                      {item.name}
                      {item.proxy && <span className={styles.proxyBadge}>代理</span>}
                    </span>
                    <span className={styles.sourcePickerOptionSub}>
                      <span>{episodeCount} 集</span>
                      {isPlaying ? <span className={styles.playingText}>正在播放</span> : null}
                    </span>
                  </span>
                  {probe ? (
                    <span className={`${styles.sourceMetric} ${probe.toneClass}`}>{probe.label}</span>
                  ) : null}
                </div>
              );
            })}
          </div>
        )}
      </div>

      <div className={styles.sourceTabs} ref={sourceTabsRef}>
        {sources?.map((item: any) => {
          const isActive = viewingSourceId === item.id;
          const probe = probeOf(item.id);
          const tabProbe = probe?.toneClass === styles.tonePending ? null : probe;
          return (
            <div
              key={item.id}
              ref={isActive ? activeTabRef : null}
              className={`${styles.tab} ${isActive ? styles.active : ""}`}
              onClick={() => onSelectViewingSource(item.id)}
              title={tabProbe ? `${item.name} · ${tabProbe.label}` : item.name}
            >
              <span className={styles.tabName}>
                {item.name}
                {item.proxy && <span className={styles.proxyBadge}>代理</span>}
              </span>
              {tabProbe ? (
                <span className={`${styles.tabSpeed} ${tabProbe.toneClass}`}>{tabProbe.label}</span>
              ) : null}
            </div>
          );
        })}
      </div>
    </div>
  );
}
