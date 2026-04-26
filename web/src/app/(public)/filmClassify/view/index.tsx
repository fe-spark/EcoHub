"use client";

import { useRouter } from "next/navigation";
import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";

export default function FilmClassifyPageView({
  data,
  pid,
}: {
  data: any;
  pid: string;
}) {
  const router = useRouter();
  const { title, content } = data;

  const renderSection = (titleStr: string, list: any[], sort: string) => (
    <div className={styles.section}>
      <div className={styles.sectionHeader}>
        <span className={styles.titleText}>{titleStr}</span>
        <a
          className={styles.moreBtn}
          onClick={() => router.push(`/filmClassifySearch?Pid=${pid}&Sort=${sort}`)}
        >
          更多 &gt;
        </a>
      </div>
      <FilmList list={list} className={styles.classifyGrid} />
    </div>
  );

  return (
    <div className={styles.container}>
      <div className={styles.title}>
        <a className={styles.active} onClick={() => router.push(`/filmClassify?Pid=${pid}`)}>
          {title.name}
        </a>
        <div className={styles.line} />
        <a onClick={() => router.push(`/filmClassifySearch?Pid=${pid}`)}>{title.name}库</a>
      </div>

      <div className={styles.content}>
        {renderSection("时间", content.news, "year")}
        {renderSection("排行榜", content.top, "hits")}
        {renderSection("最近更新", content.recent, "update_stamp")}
      </div>
    </div>
  );
}
