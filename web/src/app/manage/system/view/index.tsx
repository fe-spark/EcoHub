"use client";

import { Suspense, useCallback } from "react";

import {
  GlobalOutlined,
  BellOutlined,
  SafetyCertificateOutlined,
  FileTextOutlined,
} from "@ant-design/icons";
import { useRouter, useSearchParams } from "next/navigation";
import ManagePageHeader from "@/app/manage/components/page-header";
import SiteConfigPageView from "@/app/manage/system/website/view";
import NotifyConfigPageView from "@/app/manage/system/notify/view";
import DataSecurityPageView from "@/app/manage/system/security/view";
import SystemLogsPageView from "@/app/manage/system/logs/view";
import styles from "./index.module.less";

type MainTab = "website" | "notify" | "security" | "logs";

const MAIN_TABS: { key: MainTab; label: string; icon: React.ReactNode }[] = [
  { key: "website", label: "网站配置", icon: <GlobalOutlined /> },
  { key: "notify", label: "通知配置", icon: <BellOutlined /> },
  { key: "security", label: "数据安全", icon: <SafetyCertificateOutlined /> },
  { key: "logs", label: "系统日志", icon: <FileTextOutlined /> },
];

function normalizeMainTab(raw: string | null): MainTab {
  if (raw === "notify") return "notify";
  if (raw === "security") return "security";
  if (raw === "logs") return "logs";
  return "website";
}

function SystemSettingsBody() {
  const router = useRouter();
  const searchParams = useSearchParams();

  const mainTab = normalizeMainTab(searchParams.get("tab"));

  const replaceQuery = useCallback(
    (tab: MainTab) => {
      const params = new URLSearchParams(searchParams.toString());
      params.set("tab", tab);
      params.delete("sub");
      const qs = params.toString();
      router.replace(qs ? `/manage/system?${qs}` : "/manage/system");
    },
    [router, searchParams],
  );

  const renderPane = () => {
    switch (mainTab) {
      case "notify":
        return (
          <div className={styles.tabPaneScrollable}>
            <NotifyConfigPageView embedded />
          </div>
        );
      case "security":
        return (
          <div className={styles.tabPaneScrollable}>
            <DataSecurityPageView embedded />
          </div>
        );
      case "logs":
        return <SystemLogsPageView embedded />;
      case "website":
      default:
        return (
          <div className={styles.tabPaneScrollable}>
            <div className={styles.websitePane}>
              <SiteConfigPageView embedded />
            </div>
          </div>
        );
    }
  };

  return (
    <div className={styles.page}>
      <ManagePageHeader
        title="系统设置"
        description="网站配置（基本信息、赞赏）、通知配置、数据安全（备份 / 重置）与系统日志。"
      />
      <div className={styles.tabBar} role="tablist" aria-label="系统设置分类">
        {MAIN_TABS.map((tab) => {
          const active = mainTab === tab.key;
          return (
            <button
              key={tab.key}
              type="button"
              role="tab"
              aria-selected={active}
              className={`${styles.tabItem} ${active ? styles.tabItemActive : ""}`}
              onClick={() => replaceQuery(tab.key)}
            >
              <span className={styles.tabIcon}>{tab.icon}</span>
              <span>{tab.label}</span>
            </button>
          );
        })}
      </div>
      <div className={styles.tabContent} role="tabpanel">
        {renderPane()}
      </div>
    </div>
  );
}

/** 系统设置：一级菜单入口，自绘 Tab 承载网站配置、通知、数据安全与系统日志（不依赖 antd Tabs 内部结构，高度约束自控） */

export default function SystemSettingsPageView() {
  return (
    <Suspense fallback={null}>
      <SystemSettingsBody />
    </Suspense>
  );
}
