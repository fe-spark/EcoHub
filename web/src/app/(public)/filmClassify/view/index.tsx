"use client";

import FilmList from "@/components/public/FilmList";
import styles from "./index.module.less";
import { useContentNavigate } from "@/components/public/PublicContentLoading";

export default function FilmClassifyPageView({
  data,
  pid,
}: {
  data: any;
  pid: string;
}) {
  const { title, content } = data;
  const { navigate } = useContentNavigate();

  const handleNavigate = (url: string, label: string = "页面加载中") => {
    navigate(url, label);
  };

  const renderSection = (titleStr: string, list: any[], sort: string) => (
    <div className={styles.section}>
      <div className={styles.sectionHeader}>
        <span className={styles.titleText}>{titleStr}</span>
        <a
          className={styles.moreBtn}
          onClick={() => handleNavigate(`/filmClassifySearch?Pid=${pid}&Sort=${sort}`, "分类检索中...")}
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
        <a className={styles.active} onClick={() => handleNavigate(`/filmClassify?Pid=${pid}`, "分类加载中...")}>
          {title.name}
        </a>
        <div className={styles.line} />
        <a onClick={() => handleNavigate(`/filmClassifySearch?Pid=${pid}`, "片库加载中...")}>{title.name}库</a>
      </div>

      <div className={styles.content}>
        {renderSection("时间", content.news, "year")}
        {renderSection("排行榜", content.top, "hits")}
        {renderSection("最近更新", content.recent, "update_stamp")}
      </div>
    </div>
  );
}
