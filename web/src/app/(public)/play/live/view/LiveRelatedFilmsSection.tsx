"use client";

import React, { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { LoadingOutlined } from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import { ApiGet } from "@/lib/client-api";
import { buildLivePlayPath } from "@/lib/playNavigation";
import styles from "./index.module.less";

interface LiveRelatedFilmsSectionProps {
  sourceId: string;
  sid: string;
  cid?: number | string;
}

export default function LiveRelatedFilmsSection({
  sourceId,
  sid,
  cid,
}: LiveRelatedFilmsSectionProps) {
  const router = useRouter();
  const [relatedFilms, setRelatedFilms] = useState<any[] | null>(null);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    let cancelled = false;
    if (!sourceId) return undefined;

    setLoading(true);
    ApiGet<any[]>("/liveFilmRelate", {
      source: sourceId,
      cid: cid || 0,
      sid: sid || 0,
    })
      .then((res) => {
        if (!cancelled) {
          setRelatedFilms(Array.isArray(res.data) ? res.data : []);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setRelatedFilms([]);
        }
      })
      .finally(() => {
        if (!cancelled) {
          setLoading(false);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [sourceId, sid, cid]);

  if (!loading && (!relatedFilms || relatedFilms.length === 0)) {
    return null;
  }

  return (
    <section className={styles.relatedSection} aria-label="同类相关推荐">
      <div className={styles.relatedHeader}>
        <h2 className={styles.relatedTitle}>相关推荐</h2>
      </div>

      {loading && (!relatedFilms || relatedFilms.length === 0) ? (
        <div className={styles.relatedLoading}>
          <LoadingOutlined className={styles.relatedSpinner} />
          <span>正在获取同类推荐...</span>
        </div>
      ) : (
        <FilmList
          list={relatedFilms || []}
          onOpenPlayPage={(id) => {
            if (id) {
              router.push(buildLivePlayPath(sourceId, id));
            }
          }}
        />
      )}
    </section>
  );
}
