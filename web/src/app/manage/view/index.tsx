"use client";

import React, { useEffect, useState } from "react";
import { Button, Card, Typography } from "antd";
import Link from "next/link";
import {
  AppstoreOutlined,
  DatabaseOutlined,
  FileTextOutlined,
  FolderOpenOutlined,
  LinkOutlined,
  PictureOutlined,
  VideoCameraOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import CollectOverview from "@/app/manage/collect/view/collect-overview";
import SubscribeModal from "@/app/manage/components/subscribe-modal";
import styles from "./index.module.less";

interface FilmInventoryStats {
  films: number;
  categories: number;
  failures: number;
}

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
  {
    key: "subscribe",
    icon: LinkOutlined,
    title: "订阅地址",
    description: "TVBox 订阅与播放器 API 接口。",
    isAction: true,
  },
];

export default function ManagePageView() {
  const { isAdmin } = useManagePermission();
  const [stats, setStats] = useState<FilmInventoryStats | null>(null);
  const [subscribeModalOpen, setSubscribeModalOpen] = useState(false);

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

      <Card
        className={styles.panelCard}
        title="当前影视数据规模"
        extra={
          isAdmin ? (
            <Link href="/manage/system?tab=security" className={styles.statsLink}>
              数据安全
            </Link>
          ) : null
        }
      >
        <div className={styles.statsGrid}>
          {[
            {
              title: "影视库存",
              value: stats?.films,
              icon: VideoCameraOutlined,
            },
            {
              title: "分类",
              value: stats?.categories,
              icon: FolderOpenOutlined,
            },
            {
              title: "失败记录",
              value: stats?.failures,
              icon: FileTextOutlined,
            },
          ].map((item) => {
            const Icon = item.icon;
            return (
              <div key={item.title} className={styles.statsCard}>
                <div className={styles.statsIcon}>
                  <Icon />
                </div>
                <div className={styles.statsBody}>
                  <div className={styles.statsValue}>{item.value ?? "—"}</div>
                  <div className={styles.statsTitle}>{item.title}</div>
                </div>
              </div>
            );
          })}
        </div>
        {isAdmin && (
          <Typography.Text className={styles.statsNote}>
            反映当前库内影视相关体量。清空影视与采集派生数据请前往「系统设置 · 数据安全」。
          </Typography.Text>
        )}
      </Card>

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
