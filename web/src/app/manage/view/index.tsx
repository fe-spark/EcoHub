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
    description: "查找、更新和编辑主站影片。",
    href: "/manage/film",
  },
  {
    key: "collect",
    icon: DatabaseOutlined,
    title: "采集中心",
    description: "管理采集站，开始或停止采集。",
    href: "/manage/collect",
  },
  {
    key: "category",
    icon: AppstoreOutlined,
    title: "分类管理",
    description: "调整分类排序，以及是否在前台显示。",
    href: "/manage/collect/category",
  },
  {
    key: "category-rules",
    icon: DatabaseOutlined,
    title: "分类规则",
    description: "把采集站的分类对应到前台显示的名称。",
    href: "/manage/collect/category/rules",
  },
  {
    key: "assets",
    icon: PictureOutlined,
    title: "素材中心",
    description: "管理上传的海报和封面。",
    href: "/manage/file",
  },
  {
    key: "subscribe",
    icon: LinkOutlined,
    title: "订阅地址",
    description: "复制 TVBox、MacCMS 和客户端订阅地址。",
    isAction: true,
  },
];

export default function ManagePageView() {
  const [subscribeModalOpen, setSubscribeModalOpen] = useState(false);

  return (
    <div className={styles.dashboard}>
      <ManagePageHeader
        title="工作台"
        description="查看采集进度、片库规模和常用入口。"
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

      <Card
        className={styles.panelCard}
        classNames={{ header: styles.panelCardHead, title: styles.panelCardTitle, body: styles.panelCardBody }}
        title="快捷入口"
      >
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
