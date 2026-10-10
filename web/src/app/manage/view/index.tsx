"use client";

import React, { useState } from "react";
import { Button, Card } from "antd";
import Link from "next/link";
import {
  AppstoreOutlined,
  DatabaseOutlined,
  LinkOutlined,
  PictureOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import ManagePageHeader from "@/app/manage/components/page-header";
import CollectOverview from "@/app/manage/collect/view/collect-overview";
import SubscribeModal from "@/app/manage/components/subscribe-modal";
import InventoryStats from "./inventory-stats";
import styles from "./index.module.less";

interface QuickEntryItem {
  key: string;
  icon: React.ComponentType;
  title: string;
  description: string;
  href?: string;
  isAction?: boolean;
}

const quickEntries: QuickEntryItem[] = [
  {
    key: "film",
    icon: VideoCameraOutlined,
    title: "影片列表",
    description: "按采集源查看、更新和编辑入库影片。",
    href: "/manage/film",
  },
  {
    key: "collect",
    icon: DatabaseOutlined,
    title: "采集中心",
    description: "配置采集站顺序、首选站与批量采集任务。",
    href: "/manage/collect",
  },
  {
    key: "category",
    icon: AppstoreOutlined,
    title: "分类管理",
    description: "按采集站查看分类、显示状态和排序。",
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
  {
    key: "subscribe",
    icon: LinkOutlined,
    title: "订阅地址",
    description: "TVBox 订阅与播放器 API 接口。",
    isAction: true,
  },
];

export default function ManagePageView() {
  const [subscribeModalOpen, setSubscribeModalOpen] = useState(false);

  return (
    <div className={styles.dashboard}>
      <ManagePageHeader
        title="工作台"
        description="采集运行概况、片库规模与常用入口。"
        actions={
          <Button
            icon={<LinkOutlined />}
            onClick={() => setSubscribeModalOpen(true)}
          >
            订阅地址
          </Button>
        }
      />

      <CollectOverview />

      <InventoryStats />

      <Card className={styles.panelCard} title="快捷入口">
        <div className={styles.entryGrid}>
          {quickEntries.map((entry) => {
            const Icon = entry.icon;
            if (entry.isAction) {
              return (
                <div
                  key={entry.key}
                  className={styles.entryCard}
                  role="button"
                  tabIndex={0}
                  onClick={() => setSubscribeModalOpen(true)}
                  style={{ cursor: "pointer" }}
                >
                  <div className={styles.entryCardHead}>
                    <div className={styles.entryIconWrap}>
                      <Icon />
                    </div>
                    <div className={styles.entryTitle}>{entry.title}</div>
                  </div>
                  <div className={styles.stepDesc}>{entry.description}</div>
                </div>
              );
            }
            return (
              <Link
                key={entry.key}
                href={entry.href || "#"}
                className={styles.entryCard}
              >
                <div className={styles.entryCardHead}>
                  <div className={styles.entryIconWrap}>
                    <Icon />
                  </div>
                  <div className={styles.entryTitle}>{entry.title}</div>
                </div>
                <div className={styles.stepDesc}>{entry.description}</div>
              </Link>
            );
          })}
        </div>
      </Card>

      <SubscribeModal
        open={subscribeModalOpen}
        onClose={() => setSubscribeModalOpen(false)}
      />
    </div>
  );
}
