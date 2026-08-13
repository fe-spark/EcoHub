"use client";

import { useEffect, useState } from "react";
import { CalendarOutlined } from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";

const REFRESH_MS = 2 * 60 * 1000;

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

export default function DailyUpdates() {
  const [list, setList] = useState<DailyFilm[] | null>(null);

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
      try {
        const res = await fetch("/api/index/dailyUpdates", { cache: "no-store" });
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
              id: String(m.id ?? m.mid ?? ""),
            }))}
            className={styles.homeList}
            col={6}
          />
        </div>
      </div>
    </section>
  );
}
