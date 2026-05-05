import React, { useEffect, useState } from "react";
import { LoadingOutlined } from "@ant-design/icons";
import FilmList from "@/components/public/FilmList";
import { ApiGet } from "@/lib/client-api";
import styles from "./index.module.less";

interface RelatedFilmsSectionProps {
  filmId: string;
  initialList?: any[];
  onOpenPlayPage: (id: string, href: string) => void;
}

export default function RelatedFilmsSection({
  filmId,
  initialList,
  onOpenPlayPage,
}: RelatedFilmsSectionProps) {
  const [relatedFilms, setRelatedFilms] = useState<any[] | null>(
    initialList?.length ? initialList : null,
  );

  useEffect(() => {
    let cancelled = false;

    if (!filmId) return undefined;

    ApiGet<any[]>("/filmRelate", { id: filmId })
      .then((res) => {
        if (!cancelled) {
          setRelatedFilms(Array.isArray(res.data) ? res.data : []);
        }
      })
      .catch(() => {
        if (!cancelled) {
          setRelatedFilms([]);
        }
      });

    return () => {
      cancelled = true;
    };
  }, [filmId]);

  return (
    <div className={styles.recommendation}>
      <h2 className={styles.sectionTitle}>相关推荐</h2>
      {relatedFilms === null ? (
        <div className={styles.recommendationLoading}>
          <LoadingOutlined />
          <span>相关推荐加载中...</span>
        </div>
      ) : (
        <FilmList
          list={relatedFilms}
          className={styles.classifyGrid}
          onOpenPlayPage={onOpenPlayPage}
        />
      )}
    </div>
  );
}
