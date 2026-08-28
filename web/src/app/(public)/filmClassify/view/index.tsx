"use client";

import React from "react";
import { useContentNavigate } from "@/components/public/PublicContentLoading";
import FilmList from "@/components/public/FilmList";
import {
  AppstoreOutlined,
  ClockCircleOutlined,
  CompassOutlined,
  FireOutlined,
  RightOutlined,
  SyncOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";

interface CategoryData {
  title?: {
    id?: number | string;
    name?: string;
    pid?: number;
  };
  content?: {
    news?: any[];
    top?: any[];
    recent?: any[];
  };
}

export default function FilmClassifyPageView({
  data,
  pid,
}: {
  data: CategoryData;
  pid: string;
}) {
  const { title, content } = data || {};
  const { navigate } = useContentNavigate();
  const categoryName = title?.name || "分类";

  const handleNavigate = (url: string, label: string = "页面加载中") => {
    navigate(url, label);
  };

  const renderSection = (
    titleStr: string,
    subtitleStr: string,
    icon: React.ReactNode,
    list: any[] | undefined,
    sort: string,
  ) => {
    const safeList = Array.isArray(list) ? list : [];
    if (safeList.length === 0) {
      return null;
    }
    return (
      <section className={styles.section} aria-label={titleStr}>
        <div className={styles.sectionHeader}>
          <div className={styles.titleArea}>
            <div className={styles.titleMain}>
              <span className={styles.iconBadge}>{icon}</span>
              <h2 className={styles.titleText}>{titleStr}</h2>
            </div>
            <span className={styles.subtitleText}>{subtitleStr}</span>
          </div>
          <button
            type="button"
            className={styles.moreBtn}
            onClick={() =>
              handleNavigate(
                `/filmClassifySearch?Pid=${pid}&Sort=${sort}`,
                "片库检索中...",
              )
            }
          >
            <span>查看全部</span>
            <RightOutlined className={styles.arrowIcon} />
          </button>
        </div>
        <FilmList list={safeList} col={6} />
      </section>
    );
  };

  return (
    <div className={styles.container}>
      {/* 专区 Hero 头部 */}
      <header className={styles.heroHeader}>
        <div className={styles.heroGlow} aria-hidden />
        <div className={styles.heroContent}>
          <div className={styles.heroMeta}>
            <span className={styles.eyebrow}>
              <CompassOutlined className={styles.eyebrowIcon} />
              <span>专区精选</span>
            </span>
            <h1 className={styles.heroTitle}>{categoryName}专区</h1>
          </div>

          <div className={styles.tabSwitcher}>
            <button
              type="button"
              className={`${styles.tabBtn} ${styles.active}`}
              onClick={() =>
                handleNavigate(`/filmClassify?Pid=${pid}`, "分类专区加载中...")
              }
            >
              <AppstoreOutlined />
              <span>精选推荐</span>
            </button>
            <button
              type="button"
              className={styles.tabBtn}
              onClick={() =>
                handleNavigate(`/filmClassifySearch?Pid=${pid}`, "片库加载中...")
              }
            >
              <CompassOutlined />
              <span>全量片库</span>
            </button>
          </div>
        </div>
      </header>

      {/* 3 大精选专题板块 */}
      <div className={styles.content}>
        {renderSection(
          "最新上映",
          "院线精选与全网新上线内容",
          <ClockCircleOutlined />,
          content?.news,
          "year",
        )}
        {renderSection(
          "热度排行",
          "全网高评分与观众热门推荐榜",
          <FireOutlined />,
          content?.top,
          "hits",
        )}
        {renderSection(
          "最近更新",
          "最新入库与更新进度追踪",
          <SyncOutlined />,
          content?.recent,
          "update_stamp",
        )}
      </div>
    </div>
  );
}
