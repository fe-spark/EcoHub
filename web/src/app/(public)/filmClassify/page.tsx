"use client";

import React, { useState, useEffect, Suspense, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { ApiGet } from "@/lib/api";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";

function ClassifyContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const pid = searchParams.get("Pid");
  const { message } = useAppMessage();

  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);

  const fetchData = useCallback(async () => {
    if (!pid) return;
    setLoading(true);
    try {
      const resp = await ApiGet("/filmClassify", { Pid: pid });
      if (resp.code === 0) {
        setData(resp.data);
      } else {
        message.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  }, [pid, message]);

  useEffect(() => {
    void fetchData();
  }, [fetchData]);

  if (loading && !data) {
    return <AppLoading text="" padding="88px 0" />;
  }

  if (!data) return null;

  const { title, content } = data;

  const renderSection = (titleStr: string, list: any[], sort: string) => (
    <div className={styles.section}>
      <div className={styles.sectionHeader}>
        <span className={styles.titleText}>{titleStr}</span>
        <a
          className={styles.moreBtn}
          onClick={() =>
            router.push(`/filmClassifySearch?Pid=${pid}&Sort=${sort}`)
          }
        >
          更多 &gt;
        </a>
      </div>
      <FilmList
        list={list}
        loading={loading}
        className={styles.classifyGrid}
      />
    </div>
  );

  return (
    <div className={styles.container}>
      <div className={styles.title}>
        <a
          className={styles.active}
          onClick={() => router.push(`/filmClassify?Pid=${pid}`)}
        >
          {title.name}
        </a>
        <div className={styles.line} />
        <a onClick={() => router.push(`/filmClassifySearch?Pid=${pid}`)}>
          {title.name}库
        </a>
      </div>

      <div className={styles.content}>
        {renderSection("最新上映", content.news, "release_stamp")}
        {renderSection("排行榜", content.top, "hits")}
        {renderSection("最近更新", content.recent, "update_stamp")}
      </div>
    </div>
  );
}

export default function FilmClassifyPage() {
  return (
    <Suspense fallback={null}>
      <ClassifyContent />
    </Suspense>
  );
}
