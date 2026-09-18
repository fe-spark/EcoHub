"use client";

import { useEffect, useRef, useState } from "react";
import styles from "./SourceTabs.module.less";

export type SearchSourceTab = {
  id?: string;
  name?: string;
  count?: number;
  loading?: boolean;
};

export default function SourceTabs({
  sources,
  activeId,
  onChange,
}: {
  sources: SearchSourceTab[];
  activeId: string;
  onChange: (sourceId: string) => void;
}) {
  const scrollerRef = useRef<HTMLDivElement>(null);
  const [fadeStart, setFadeStart] = useState(false);
  const [fadeEnd, setFadeEnd] = useState(false);

  const updateOverflow = () => {
    const el = scrollerRef.current;
    if (!el) {
      return;
    }
    const max = el.scrollWidth - el.clientWidth;
    setFadeStart(el.scrollLeft > 6);
    setFadeEnd(max - el.scrollLeft > 6);
  };

  useEffect(() => {
    const el = scrollerRef.current;
    if (!el) {
      return;
    }
    updateOverflow();
    const observer = new ResizeObserver(updateOverflow);
    observer.observe(el);
    el.addEventListener("scroll", updateOverflow, { passive: true });
    const onWheel = (event: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth) {
        return;
      }
      if (Math.abs(event.deltaY) <= Math.abs(event.deltaX)) {
        return;
      }
      event.preventDefault();
      el.scrollLeft += event.deltaY;
    };
    el.addEventListener("wheel", onWheel, { passive: false });
    return () => {
      observer.disconnect();
      el.removeEventListener("scroll", updateOverflow);
      el.removeEventListener("wheel", onWheel);
    };
  }, [sources]);

  useEffect(() => {
    const el = scrollerRef.current;
    const active = el?.querySelector('[aria-pressed="true"]');
    if (!el || !(active instanceof HTMLElement)) {
      return;
    }
    const left = active.offsetLeft - (el.clientWidth - active.offsetWidth) / 2;
    el.scrollTo({ left: Math.max(0, left), behavior: "smooth" });
  }, [activeId, sources.length]);

  if (!Array.isArray(sources) || sources.length <= 1) {
    return null;
  }

  return (
    <div
      className={`${styles.wrap} ${fadeStart ? styles.fadeStart : ""} ${fadeEnd ? styles.fadeEnd : ""}`}
    >
      <div ref={scrollerRef} className={styles.scroller} aria-label="按采集源查看">
        {sources.map((item) => {
          const id = String(item.id || "");
          const active = id === activeId;
          const count = Number(item.count);
          return (
            <button
              type="button"
              key={id || "all"}
              className={`${styles.chip} ${active ? styles.active : ""}`}
              aria-pressed={active}
              aria-busy={item.loading ? true : undefined}
              onClick={() => onChange(id)}
            >
              <span className={styles.name}>{item.name || "未命名源"}</span>
              <span className={styles.meta}>
                {item.loading ? (
                  <span className={styles.spinner} aria-label="加载中" />
                ) : (
                  <span className={styles.count}>{Number.isFinite(count) ? count : 0}</span>
                )}
              </span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
