"use client";

import { useEffect, useRef, useState } from "react";
import { CalendarOutlined } from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";

const REFRESH_MS = 15 * 1000;

interface DailyFilm {
  id: string;
  mid?: string;
  name: string;
  picture: string;
  year: string;
  cName: string;
  area: string;
  language?: string;
  classTag?: string;
  remarks: string;
  blurb?: string;
}

function filmId(item: DailyFilm) {
  return String(item.id ?? item.mid ?? "").trim();
}

export default function DailyUpdates() {
  const [list, setList] = useState<DailyFilm[] | null>(null);
  const listRef = useRef<DailyFilm[] | null>(null);

  useEffect(() => {
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | undefined;

    const scheduleNext = () => {
      if (cancelled) {
        return;
      }
      timer = setTimeout(() => {
        void load();
      }, REFRESH_MS);
    };

    const load = async () => {
      const exclude = (listRef.current ?? [])
        .map(filmId)
        .filter(Boolean)
        .join(",");
      const url = exclude
        ? `/api/index/dailyUpdates?exclude=${encodeURIComponent(exclude)}`
        : "/api/index/dailyUpdates";
      try {
        const res = await fetch(url, { cache: "no-store" });
        if (!res.ok) {
          throw new Error(String(res.status));
        }
        const json = (await res.json()) as { code: number; data?: DailyFilm[] };
        if (cancelled) {
          return;
        }
        if (json.code !== 0) {
          throw new Error(String(json.code));
        }
        const next = Array.isArray(json.data) ? json.data : [];
        listRef.current = next;
        setList(next);
        scheduleNext();
      } catch {
        if (!cancelled) {
          scheduleNext();
        }
      }
    };

    void load();
    return () => {
      cancelled = true;
      if (timer) {
        clearTimeout(timer);
      }
    };
  }, []);

  if (!list || list.length === 0) {
    return null;
  }

  return (
    <section className={styles.section}>
      <div className={styles.sectionHeader}>
        <div className={styles.left}>
          <CalendarOutlined className={`${styles.icon} ${styles.dailyIcon}`} />
          <span>每日更新</span>
        </div>
        <div className={styles.nav}>
          <span className={styles.dailyHint}>近 24 小时</span>
        </div>
      </div>
      <div className={styles.sectionBody}>
        <div className={styles.filmGrid}>
          <FilmList
            list={list.map((m) => ({
              ...m,
              id: filmId(m),
            }))}
            className={styles.homeList}
            col={6}
          />
        </div>
      </div>
    </section>
  );
}
