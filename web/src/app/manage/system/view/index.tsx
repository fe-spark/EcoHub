"use client";

import { Suspense, useCallback, useMemo } from "react";
import { Tabs } from "antd";

import {
  GlobalOutlined,
  BellOutlined,
  SafetyCertificateOutlined,
  FileTextOutlined,
} from "@ant-design/icons";
import { useRouter, useSearchParams } from "next/navigation";
import ManagePageHeader from "@/app/manage/components/page-header";
import SiteConfigPageView from "@/app/manage/system/website/view";
import BannersPageView from "@/app/manage/system/banners/view";
import NotifyConfigPageView from "@/app/manage/system/notify/view";
import DataSecurityPageView from "@/app/manage/system/security/view";
import SystemLogsPageView from "@/app/manage/system/logs/view";
import styles from "./index.module.less";

type MainTab = "website" | "notify" | "security" | "logs";

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

  const mainItems = useMemo(
    () => [
      {
        key: "website",
        label: "网站配置",
        icon: <GlobalOutlined />,
        children: (
          <div className={styles.websitePane}>
            <SiteConfigPageView embedded />
            <BannersPageView embedded />
          </div>
        ),
      },
      {
        key: "notify",
        label: "通知配置",
        icon: <BellOutlined />,
        children: <NotifyConfigPageView embedded />,
      },
      {
        key: "security",
        label: "数据安全",
        icon: <SafetyCertificateOutlined />,
        children: <DataSecurityPageView embedded />,
      },
      {
        key: "logs",
        label: "系统日志",
        icon: <FileTextOutlined />,
        children: <SystemLogsPageView embedded />,
      },
    ],
    [],
  );

  return (
    <div className={styles.page}>
      <ManagePageHeader
        title="系统设置"
        description="网站配置（基本信息与首页封面）、通知配置、数据安全（备份 / 重置）与系统日志。"
      />
      <Tabs
        className={styles.tabs}
        activeKey={mainTab}
        destroyOnHidden
        onChange={(key) => replaceQuery(normalizeMainTab(key))}
        items={mainItems}
      />
    </div>
  );
}

/** 系统设置：一级菜单入口，内部 Tabs 承载网站配置、通知、数据安全与系统日志 */

export default function SystemSettingsPageView() {
  return (
    <Suspense fallback={null}>
      <SystemSettingsBody />
    </Suspense>
  );
}


