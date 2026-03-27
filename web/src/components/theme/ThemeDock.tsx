"use client";

import React, { useEffect, useRef, useState, useCallback } from "react";
import styles from "./ThemeDock.module.less";

export type ThemeMode = "dark" | "light" | "system";

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

const SNAP_KEY = "theme-dock-snap";
const EDGE_X = 0;
const MARGIN_Y = 12;
const DOCK_W = 44;
const DOCK_H = 44;
const DRAG_THRESHOLD = 5;

interface SnapState {
  snappedLeft: boolean;
  y: number;
}

function clampY(y: number) {
  return Math.max(MARGIN_Y, Math.min(y, window.innerHeight - DOCK_H - MARGIN_Y));
}

function snapX(snappedLeft: boolean) {
  return snappedLeft ? EDGE_X : Math.max(EDGE_X, window.innerWidth - DOCK_W - EDGE_X);
}

function normalizeSnap(s: SnapState): SnapState {
  return {
    snappedLeft: !!s.snappedLeft,
    y: Number.isFinite(s.y) ? clampY(s.y) : clampY(window.innerHeight / 2 - DOCK_H / 2),
  };
}

function loadSnap(): SnapState | null {
  try {
    const raw = localStorage.getItem(SNAP_KEY);
    if (raw) return JSON.parse(raw);
  } catch {}
  return null;
}

function saveSnap(s: SnapState) {
  localStorage.setItem(SNAP_KEY, JSON.stringify(s));
}

interface Props {
  mode: ThemeMode;
  onSelect: (m: ThemeMode) => void;
}

export default function ThemeDock({ mode, onSelect }: Props) {
  const dockRef = useRef<HTMLDivElement>(null);

  // ── 位置 ─────────────────────────────────────────────────────
  // 组件由父级在 mounted 后才渲染，window 一定存在，可安全用懒初始化
  const [snap, setSnap] = useState<SnapState>(
    () => normalizeSnap(loadSnap() ?? { snappedLeft: true, y: window.innerHeight / 2 - DOCK_H / 2 })
  );

  // ── 展开/收起 ─────────────────────────────────────────────────
  const [expanded, setExpanded] = useState(false);
  const [tempPos, setTempPos] = useState<{ x: number; y: number } | null>(null);
  const [snapping, setSnapping] = useState(false);
  const dragging = tempPos !== null;
  const snapTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    return () => {
      if (snapTimerRef.current) {
        clearTimeout(snapTimerRef.current);
      }
    };
  }, []);

  useEffect(() => {
    const onResize = () => {
      setSnap((prev) => {
        const next = normalizeSnap(prev);
        if (next.y !== prev.y) {
          saveSnap(next);
        }
        return next;
      });
    };
    window.addEventListener("resize", onResize);
    return () => window.removeEventListener("resize", onResize);
  }, []);

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

  // ── 拖拽 ─────────────────────────────────────────────────────
  const dragRef = useRef<{
    startX: number;
    startY: number;
    elemLeft: number;
    elemTop: number;
    moved: boolean;
  } | null>(null);

  const onPointerDown = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "mouse" && e.button !== 0) return;
    const el = dockRef.current;
    if (!el) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const rect = el.getBoundingClientRect();
    dragRef.current = {
      startX: e.clientX,
      startY: e.clientY,
      elemLeft: rect.left,
      elemTop: rect.top,
      moved: false,
    };
  }, []);

  const onPointerMove = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    const ds = dragRef.current;
    if (!ds) return;

    const dx = e.clientX - ds.startX;
    const dy = e.clientY - ds.startY;
    if (!ds.moved && (Math.abs(dx) > DRAG_THRESHOLD || Math.abs(dy) > DRAG_THRESHOLD)) {
      ds.moved = true;
      // 开始拖拽时收起面板
      setExpanded(false);
    }
    if (!ds.moved) return;

    const newX = ds.elemLeft + dx;
    const newY = ds.elemTop + dy;
    const clampedX = Math.max(EDGE_X, Math.min(newX, window.innerWidth - DOCK_W - EDGE_X));
    const clampedY = clampY(newY);

    setTempPos({ x: clampedX, y: clampedY });
  }, []);

  const onPointerUp = useCallback((e: React.PointerEvent<HTMLDivElement>) => {
    const el = dockRef.current;
    if (!el) return;
    e.currentTarget.releasePointerCapture(e.pointerId);

    const ds = dragRef.current;
    dragRef.current = null;

    if (!ds) return;

    if (!ds.moved) {
      // 普通点击：切换展开
      setExpanded((v) => !v);
      return;
    }

    // 拖拽结束：基于拖拽中的临时坐标计算吸附
    const finalX = tempPos?.x ?? el.getBoundingClientRect().left;
    const finalY = clampY(tempPos?.y ?? el.getBoundingClientRect().top);
    const isLeft = finalX + DOCK_W / 2 < window.innerWidth / 2;

    setTempPos(null);
    const next: SnapState = { snappedLeft: isLeft, y: finalY };
    setSnap(next);
    saveSnap(next);

    setSnapping(true);
    if (snapTimerRef.current) {
      clearTimeout(snapTimerRef.current);
    }
    snapTimerRef.current = setTimeout(() => setSnapping(false), 260);
  }, [tempPos]);

  const onPointerCancel = useCallback(() => {
    dragRef.current = null;
    setTempPos(null);
  }, []);

  // ── 渲染 ─────────────────────────────────────────────────────
  const posStyle: React.CSSProperties = tempPos
    ? { left: tempPos.x, top: tempPos.y }
    : { left: snapX(snap.snappedLeft), top: snap.y };

  const activeOption = OPTIONS.find((o) => o.key === mode) ?? OPTIONS[2];

  return (
    <div
      ref={dockRef}
      className={[
        styles.dock,
        snap.snappedLeft ? styles.left : styles.right,
        dragging ? styles.dragging : "",
        snapping ? styles.snapping : "",
        expanded ? styles.expanded : "",
      ]
        .filter(Boolean)
        .join(" ")}
      style={posStyle}
      onPointerDown={onPointerDown}
      onPointerMove={onPointerMove}
      onPointerUp={onPointerUp}
      onPointerCancel={onPointerCancel}
    >
      {/* 圆形触发按钮 */}
      <div className={styles.trigger}>{activeOption.icon}</div>

      {/* 弹出选项面板 */}
      <div className={styles.panel}>
        {OPTIONS.map((opt) => (
          <button
            key={opt.key}
            type="button"
            className={[styles.option, mode === opt.key ? styles.active : ""]
              .filter(Boolean)
              .join(" ")}
            onPointerDown={(e) => e.stopPropagation()}
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
    </div>
  );
}
