"use client";

import { Suspense, useCallback, useEffect } from "react";

import {
  BellOutlined,
  SafetyCertificateOutlined,
  FileTextOutlined,
  CompassOutlined,
  ApiOutlined,
} from "@ant-design/icons";
import { useRouter, useSearchParams } from "next/navigation";
import ManagePageHeader from "@/app/manage/components/page-header";
import NotifyConfigPageView from "@/app/manage/system/notify/view";
import ProxyConfigPageView from "@/app/manage/system/proxy/view";
import DataSecurityPageView from "@/app/manage/system/security/view";
import SystemLogsPageView from "@/app/manage/system/logs/view";
import TMDBConfigPageView from "@/app/manage/system/tmdb/view";
import styles from "./index.module.less";

type MainTab = "notify" | "proxy" | "tmdb" | "security" | "logs";

const MAIN_TABS: { key: MainTab; label: string; icon: React.ReactNode }[] = [
  { key: "notify", label: "通知配置", icon: <BellOutlined /> },
  { key: "proxy", label: "网络代理", icon: <ApiOutlined /> },
  { key: "tmdb", label: "刮削配置", icon: <CompassOutlined /> },
  { key: "security", label: "数据安全", icon: <SafetyCertificateOutlined /> },
  { key: "logs", label: "运行日志", icon: <FileTextOutlined /> },
];

function normalizeMainTab(raw: string | null): MainTab {
  if (raw === "proxy") return "proxy";
  if (raw === "tmdb") return "tmdb";
  if (raw === "security") return "security";
  if (raw === "logs") return "logs";
  return "notify";
}

function SystemSettingsBody() {
  const router = useRouter();
  const searchParams = useSearchParams();

  useEffect(() => {
    if (searchParams.get("tab") === "website") {
      router.replace("/manage/system/website");
    }
  }, [router, searchParams]);

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
      case "proxy":
        return (
          <div className={styles.tabPaneScrollable}>
            <ProxyConfigPageView embedded />
          </div>
        );
      case "tmdb":
        return (
          <div className={styles.tabPaneScrollable}>
            <TMDBConfigPageView embedded />
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
      case "notify":
      default:
        return (
          <div className={styles.tabPaneScrollable}>
            <NotifyConfigPageView embedded />
          </div>
        );
    }
  };

  return (
    <div className={styles.page}>
      <ManagePageHeader
        className={styles.pageHeader}
        title="系统设置"
        description="通知配置、网络代理、刮削配置、数据安全与运行日志。"
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

/** 系统设置：一级菜单入口，自绘 Tab 承载通知、数据安全与系统日志（不依赖 antd Tabs 内部结构，高度约束自控） */

export default function SystemSettingsPageView() {
  return (
    <Suspense fallback={null}>
      <SystemSettingsBody />
    </Suspense>
  );
}
