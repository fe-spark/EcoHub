"use client";

import React, { Suspense, useCallback } from "react";
import {
  BellOutlined,
  HeartOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { useRouter, useSearchParams } from "next/navigation";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import BasicConfigCard from "./basic-config-card";
import TipConfigCard from "./tip-config-card";
import NoticeConfigCard from "./notice-config-card";
import styles from "./index.module.less";

type WebsiteTab = "basic" | "tip" | "notice";

const WEBSITE_TABS: { key: WebsiteTab; label: string; icon: React.ReactNode }[] = [
  { key: "basic", label: "基本信息", icon: <SettingOutlined /> },
  { key: "tip", label: "赞赏支持", icon: <HeartOutlined /> },
  { key: "notice", label: "站点公告", icon: <BellOutlined /> },
];

function normalizeWebsiteTab(raw: string | null): WebsiteTab {
  if (raw === "tip") return "tip";
  if (raw === "notice") return "notice";
  return "basic";
}

interface SiteConfigPageViewProps {
  /** 嵌入系统设置 Tabs 时隐藏独立页头 */
  embedded?: boolean;
}

function SiteConfigBody({ embedded = false }: SiteConfigPageViewProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const currentTab = normalizeWebsiteTab(searchParams.get("tab"));
  const { canWrite } = useManagePermission();

  const replaceTabQuery = useCallback(
    (tab: WebsiteTab) => {
      const params = new URLSearchParams(searchParams.toString());
      params.set("tab", tab);
      const qs = params.toString();
      router.replace(qs ? `/manage/system/website?${qs}` : "/manage/system/website");
    },
    [router, searchParams],
  );

  const renderTabPane = () => {
    switch (currentTab) {
      case "tip":
        return (
          <div className={styles.tabPaneScrollable}>
            <TipConfigCard canWrite={canWrite} />
          </div>
        );
      case "notice":
        return (
          <div className={styles.tabPaneScrollable}>
            <NoticeConfigCard canWrite={canWrite} />
          </div>
        );
      case "basic":
      default:
        return (
          <div className={styles.tabPaneScrollable}>
            <BasicConfigCard canWrite={canWrite} />
          </div>
        );
    }
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          className={styles.pageHeader}
          title="网站配置"
          description="维护站点基本信息"
        />
      )}

      <div className={styles.tabBar} role="tablist" aria-label="网站配置分类">
        {WEBSITE_TABS.map((tab) => {
          const active = currentTab === tab.key;
          return (
            <button
              key={tab.key}
              type="button"
              role="tab"
              aria-selected={active}
              className={`${styles.tabItem} ${active ? styles.tabItemActive : ""}`}
              onClick={() => replaceTabQuery(tab.key)}
            >
              <span className={styles.tabIcon}>{tab.icon}</span>
              <span>{tab.label}</span>
            </button>
          );
        })}
      </div>

      <div className={styles.tabContent} role="tabpanel">
        {renderTabPane()}
      </div>
    </div>
  );
}

export default function SiteConfigPageView(props: SiteConfigPageViewProps) {
  return (
    <Suspense fallback={null}>
      <SiteConfigBody {...props} />
    </Suspense>
  );
}
