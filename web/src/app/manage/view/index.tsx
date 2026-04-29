"use client";

import React from "react";
import { Card } from "antd";
import Link from "next/link";
import {
  AppstoreOutlined,
  DatabaseOutlined,
  PictureOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

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
    title: "采集站点",
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
    title: "图片素材",
    description: "上传、预览和整理站内会用到的封面图与素材图。",
    href: "/manage/file",
  },
];

export default function ManagePageView() {
  return (
    <div className={styles.dashboard}>
      <ManagePageHeader
        title="管理后台"
        description="保留高频管理入口，直接进入最常用的后台页面。"
      />

      <Card className={styles.panelCard}>
        <section className={styles.sectionBlock}>
          <div className={styles.entryGrid}>
            {quickEntries.map((entry) => {
              const Icon = entry.icon;
              return (
                <Link key={entry.key} href={entry.href} className={styles.entryCard}>
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
