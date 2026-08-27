"use client";

import React, { useEffect, useRef, useState } from "react";
import styles from "./ThemeDock.module.less";
import type { ThemeMode } from "@/lib/theme";

export type { ThemeMode };

const ICON_SUN = (
  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <circle cx="12" cy="12" r="5" />
    <line x1="12" y1="1" x2="12" y2="3" />
    <line x1="12" y1="21" x2="12" y2="23" />
    <line x1="4.22" y1="4.22" x2="5.64" y2="5.64" />
    <line x1="18.36" y1="18.36" x2="19.78" y2="19.78" />
    <line x1="1" y1="12" x2="3" y2="12" />
    <line x1="21" y1="12" x2="23" y2="12" />
    <line x1="4.22" y1="19.78" x2="5.64" y2="18.36" />
    <line x1="18.36" y1="5.64" x2="19.78" y2="4.22" />
  </svg>
);
const ICON_MOON = (
  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M21 12.79A9 9 0 1111.21 3 7 7 0 0021 12.79z" />
  </svg>
);
const ICON_SYSTEM = (
  <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <rect x="2" y="3" width="20" height="14" rx="2" ry="2" />
    <line x1="8" y1="21" x2="16" y2="21" />
    <line x1="12" y1="17" x2="12" y2="21" />
  </svg>
);

const OPTIONS: { key: ThemeMode; icon: React.ReactNode; label: string }[] = [
  { key: "light", icon: ICON_SUN, label: "浅色" },
  { key: "dark", icon: ICON_MOON, label: "深色" },
  { key: "system", icon: ICON_SYSTEM, label: "跟随系统" },
];

interface Props {
  mode: ThemeMode;
  onSelect: (m: ThemeMode) => void;
}

export default function ThemeDock({ mode, onSelect }: Props) {
  const dockRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState(false);

  useEffect(() => {
    if (!expanded) return;
    const onDown = (e: MouseEvent) => {
      if (dockRef.current && !dockRef.current.contains(e.target as Node)) {
        setExpanded(false);
      }
    };
    document.addEventListener("mousedown", onDown);
    return () => document.removeEventListener("mousedown", onDown);
  }, [expanded]);

  const activeOption = OPTIONS.find((o) => o.key === mode) ?? OPTIONS[2];

  return (
    <div
      ref={dockRef}
      className={`${styles.dock} ${expanded ? styles.expanded : ""}`}
    >
      {/* 选项弹出面板（向上展开） */}
      <div className={styles.panel} role="menu" aria-label="主题模式选择">
        {OPTIONS.map((opt) => (
          <button
            key={opt.key}
            type="button"
            className={`${styles.option} ${mode === opt.key ? styles.active : ""}`}
            onClick={() => {
              onSelect(opt.key);
              setExpanded(false);
            }}
          >
            {opt.icon}
            <span>{opt.label}</span>
          </button>
        ))}
      </div>

      {/* 底部固定圆形触发按钮 */}
      <button
        type="button"
        className={styles.trigger}
        onClick={() => setExpanded((v) => !v)}
        title={`当前主题：${activeOption.label}，点击切换`}
        aria-label="切换主题外观"
      >
        {activeOption.icon}
      </button>
    </div>
  );
}
