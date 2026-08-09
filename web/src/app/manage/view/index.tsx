"use client";

import React, { useCallback, useEffect, useState } from "react";
import { Card, Col, Row, Statistic, Typography } from "antd";
import Link from "next/link";
import {
  AppstoreOutlined,
  DatabaseOutlined,
  FileTextOutlined,
  FolderOpenOutlined,
  PictureOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import ManagePageHeader from "@/app/manage/components/page-header";
import CollectOverview from "@/app/manage/collect/view/collect-overview";
import styles from "./index.module.less";

interface FilmInventoryStats {
  films: number;
  snapshots: number;
  categories: number;
  failures: number;
}

const quickEntries = [
  {
    key: "film",
    icon: VideoCameraOutlined,
    title: "影片列表",
    description: "快速查看、更新和编辑主库存影片。",
    href: "/manage/film",
  },
  {
    key: "collect",
    icon: DatabaseOutlined,
    title: "采集中心",
    description: "配置主站、附属站与批量采集任务。",
    href: "/manage/collect",
  },
  {
    key: "category",
    icon: AppstoreOutlined,
    title: "分类管理",
    description: "维护当前主站分类框架、显示状态与排序。",
    href: "/manage/collect/category",
  },
  {
    key: "category-rules",
    icon: DatabaseOutlined,
    title: "分类规则",
    description: "配置来源分类到展示分类的合并映射。",
    href: "/manage/collect/category/rules",
  },
  {
    key: "assets",
    icon: PictureOutlined,
    title: "素材中心",
    description: "上传、预览和整理站内会用到的封面图与素材图。",
    href: "/manage/file",
  },
];

export default function ManagePageView() {
  const [stats, setStats] = useState<FilmInventoryStats | null>(null);

  useEffect(() => {
    let active = true;
    ApiGet<FilmInventoryStats>("/manage/spider/clear/stats")
      .then((resp) => {
        if (active && resp.code === 0 && resp.data) {
          setStats(resp.data);
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, []);

  return (
    <div className={styles.dashboard}>
      <ManagePageHeader
        title="工作台"
        description="采集运行概况、影视数据规模与常用入口。"
      />

      <CollectOverview />

      <Card
        className={styles.panelCard}
        size="small"
        title="当前影视数据规模"
        extra={
          <Link href="/manage/system?tab=security" className={styles.statsLink}>
            数据安全
          </Link>
        }
      >
        <Row gutter={[16, 16]}>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="影视库存"
              value={stats?.films ?? "—"}
              prefix={<VideoCameraOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="列表快照"
              value={stats?.snapshots ?? "—"}
              prefix={<DatabaseOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="分类"
              value={stats?.categories ?? "—"}
              prefix={<FolderOpenOutlined />}
            />
          </Col>
          <Col xs={12} sm={12} md={6}>
            <Statistic
              title="失败记录"
              value={stats?.failures ?? "—"}
              prefix={<FileTextOutlined />}
            />
          </Col>
        </Row>
        <Typography.Text type="secondary" className={styles.statsNote}>
          反映当前库内影视相关体量。清空影视与采集派生数据请前往「系统设置 · 数据安全」。
        </Typography.Text>
      </Card>

      <Card className={styles.panelCard} title="快捷入口" size="small">
        <section className={styles.sectionBlock}>
          <div className={styles.entryGrid}>
            {quickEntries.map((entry) => {
              const Icon = entry.icon;
              return (
                <Link
                  key={entry.key}
                  href={entry.href}
                  className={styles.entryCard}
                >
                  <div className={styles.entryCardHead}>
                    <Icon className={styles.entryIcon} />
                    <div className={styles.entryTitle}>{entry.title}</div>
                  </div>
                  <div className={styles.stepDesc}>{entry.description}</div>
                </Link>
              );
            })}
          </div>
        </section>
      </Card>
    </div>
  );
}
