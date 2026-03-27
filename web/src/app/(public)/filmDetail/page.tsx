"use client";

import React, { useState, useEffect, Suspense, useCallback } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button, Space } from "antd";
import {
  CaretRightOutlined,
  RocketOutlined,
  ArrowDownOutlined,
  ArrowUpOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/api";
import FilmList from "@/components/public/FilmList";
import AppLoading from "@/components/public/Loading";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { readHistoryMap } from "@/lib/historyStorage";

import { FALLBACK_IMG } from "@/lib/fallbackImg";

function FilmDetailContent() {
  const router = useRouter();
  const searchParams = useSearchParams();
  const { message } = useAppMessage();
  const link = searchParams.get("link");

  const [data, setData] = useState<any>(null);
  const [loading, setLoading] = useState(true);


  useEffect(() => {
    if (!link) return;

    const load = async () => {
      setLoading(true);
      try {
        const resp = await ApiGet("/filmDetail", { id: link });
        if (resp.code === 0) {
          const detail = resp.data.detail;
          detail.name = detail.name.replace(/(～.*～)/g, "");
          if (detail.descriptor?.content) {
            let content = detail.descriptor.content;
            content = content.replace(/<br\s*\/?>/gi, "\n");
            content = content.replace(/<\/p>/gi, "\n");
            content = content.replace(/(&.*;)|( )|(　　)|(<[^>]+>)/g, "");
            content = content.replace(/\n+/g, "\n");
            detail.descriptor.content = content.trim();
          }
          setData(resp.data);
        } else {
          message.error(resp.msg);
        }
      } finally {
        setLoading(false);
      }
    };

    void load();
  }, [link, message]);

  const handlePlayClick = () => {
    // 尝试从历史中获取播放进度
    const historyMap = readHistoryMap();
    const savedState = historyMap[detail.id];
    // 如果历史中存在播放页面的完整路由记录，直接跳转续播
    if (
      savedState &&
      savedState.link &&
      savedState.link.includes("/play")
    ) {
      router.push(savedState.link);
      return;
    }
    // 否则默认播放由第一组数据源提供的第一集
    router.push(`/play?id=${link}&source=${detail.list[0].id}&episode=${0}`);
  };

  if (loading) {
    return <AppLoading text="" padding="88px 0" />;
  }

  if (!data) return null;

  const { detail, relate } = data;

  return (
    <div className={styles.container}>
      {/* Background Layer */}
      <div className={styles.bgWrapper}>
        <div
          className={styles.bgPoster}
          style={{ backgroundImage: `url(${detail.picture})` }}
        />
        <div className={styles.bgMask} />
      </div>

      <div className={styles.content}>
        <div className={styles.left}>
          <img
            src={detail.picture || FALLBACK_IMG}
            className={styles.poster}
            alt={detail.name}
          />
        </div>

        <div className={styles.right}>
          <h1 className={styles.title}>{detail.name}</h1>

          <div className={styles.meta}>
            {detail.descriptor.cName && (
              <span className={styles.metaItem}>{detail.descriptor.cName}</span>
            )}
            {detail.descriptor.classTag
              ?.split(",")
              .filter((t: string) => t.trim())
              .map((t: string, i: number) => (
                <span key={i} className={styles.metaItem}>
                  {t}
                </span>
              ))}
            {detail.descriptor.year && (
              <span className={styles.metaItem}>{detail.descriptor.year}</span>
            )}
            {detail.descriptor.area && (
              <span className={styles.metaItem}>{detail.descriptor.area}</span>
            )}
          </div>

          <div className={styles.actions}>
            <Button
              type="primary"
              className={styles.playBtn}
              icon={<CaretRightOutlined />}
              onClick={handlePlayClick}
            >
              立即播放
            </Button>
            <Button
              className={styles.collectBtn}
              icon={<RocketOutlined />}
              onClick={() => message.info("功能开发中...")}
            >
              收藏
            </Button>
          </div>

          <div className={styles.intro}>
            <h2 className={styles.sectionTitle}>简介</h2>
            <div className={styles.descContent}>
              {detail.descriptor.content || "暂无简介"}
            </div>
          </div>
        </div>
      </div>

      {/* Recommendations */}
      <div className={styles.recommendation}>
        <h2 className={styles.sectionTitle} style={{ marginBottom: 24 }}>
          相关推荐
        </h2>
        <FilmList list={relate} className={styles.classifyGrid} />
      </div>
    </div>
  );
}

export default function FilmDetailPage() {
  return (
    <Suspense fallback={null}>
      <FilmDetailContent />
    </Suspense>
  );
}
