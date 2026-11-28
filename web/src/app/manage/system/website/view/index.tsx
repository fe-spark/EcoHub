"use client";

import React, { Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { Alert } from "antd";
import {
  BellOutlined,
  HeartOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { useRouter, useSearchParams } from "next/navigation";
import { useManagePermission } from "@/lib/manage-permission";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import BasicConfigCard from "./basic-config-card";
import AccessConfigCard from "./access-config-card";
import TipConfigCard from "./tip-config-card";
import NoticeConfigCard from "./notice-config-card";
import DanmakuConfigCard from "./danmaku-config-card";
import MediaProxyConfigCard from "./media-proxy-config-card";
import type { ConfigCardHandle } from "./types";
import styles from "./index.module.less";

type WebsiteTab = "basic" | "tip" | "notice";
type BasicModuleKey = "basic" | "access" | "mediaProxy" | "danmaku";

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

function BasicConfigTab({
  canWrite,
  onDirtyChange,
}: {
  canWrite: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}) {
  const basicRef = useRef<ConfigCardHandle>(null);
  const accessRef = useRef<ConfigCardHandle>(null);
  const mediaProxyRef = useRef<ConfigCardHandle>(null);
  const danmakuRef = useRef<ConfigCardHandle>(null);

  const [dirtyMap, setDirtyMap] = useState<Record<BasicModuleKey, boolean>>({
    basic: false,
    access: false,
    mediaProxy: false,
    danmaku: false,
  });
  const [saving, setSaving] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const updateDirty = useCallback((key: BasicModuleKey, dirty: boolean) => {
    setDirtyMap((prev) => (prev[key] === dirty ? prev : { ...prev, [key]: dirty }));
  }, []);

  const handleBasicDirty = useCallback(
    (dirty: boolean) => updateDirty("basic", dirty),
    [updateDirty],
  );
  const handleAccessDirty = useCallback(
    (dirty: boolean) => updateDirty("access", dirty),
    [updateDirty],
  );
  const handleMediaProxyDirty = useCallback(
    (dirty: boolean) => updateDirty("mediaProxy", dirty),
    [updateDirty],
  );
  const handleDanmakuDirty = useCallback(
    (dirty: boolean) => updateDirty("danmaku", dirty),
    [updateDirty],
  );

  const dirtyCount = useMemo(
    () => Object.values(dirtyMap).filter(Boolean).length,
    [dirtyMap],
  );

  useEffect(() => {
    onDirtyChange?.(dirtyCount > 0);
  }, [dirtyCount, onDirtyChange]);

  const handleDiscardAll = useCallback(() => {
    basicRef.current?.discard();
    accessRef.current?.discard();
    mediaProxyRef.current?.discard();
    danmakuRef.current?.discard();
  }, []);

  const handleSaveAll = useCallback(async () => {
    if (!canWrite) return;
    const handles = [
      basicRef.current,
      accessRef.current,
      mediaProxyRef.current,
      danmakuRef.current,
    ].filter((h): h is ConfigCardHandle => Boolean(h?.isDirty()));

    if (handles.length === 0) return;

    for (const handle of handles) {
      if (!handle.validate()) {
        return;
      }
    }

    setSaving(true);
    let successCount = 0;
    try {
      // 顺序保存各子模块，避免多实例场景下同时覆写单行 site_config_records。
      for (const handle of handles) {
        if (await handle.save()) {
          successCount++;
        }
      }
      if (successCount > 0) {
        await refreshSiteConfig();
      }
      if (successCount === handles.length) {
        message.success("配置已保存");
      }
    } finally {
      setSaving(false);
    }
  }, [canWrite, message, refreshSiteConfig]);

  return (
    <>
      <div className={styles.tabPaneScrollable}>
        <BasicConfigCard
          ref={basicRef}
          canWrite={canWrite}
          onDirtyChange={handleBasicDirty}
        />
        <AccessConfigCard
          ref={accessRef}
          canWrite={canWrite}
          onDirtyChange={handleAccessDirty}
        />
        <MediaProxyConfigCard
          ref={mediaProxyRef}
          canWrite={canWrite}
          onDirtyChange={handleMediaProxyDirty}
        />
        <DanmakuConfigCard
          ref={danmakuRef}
          canWrite={canWrite}
          onDirtyChange={handleDanmakuDirty}
        />
      </div>

      <UnsavedChangesBar
        visible={dirtyCount > 0}
        saving={saving}
        message={
          dirtyCount > 1
            ? `有 ${dirtyCount} 个模块存在未保存的更改`
            : "有未保存的更改"
        }
        onDiscard={handleDiscardAll}
        onSave={() => void handleSaveAll()}
      />
    </>
  );
}

interface SiteConfigPageViewProps {
  /** 嵌入系统设置 Tabs 时隐藏独立页头 */
  embedded?: boolean;
}

function SiteConfigBody({ embedded = false }: SiteConfigPageViewProps) {
  const router = useRouter();
  const searchParams = useSearchParams();
  const currentTab = normalizeWebsiteTab(searchParams.get("tab"));
  const { canWrite, isAdmin } = useManagePermission();
  const { modal } = useAppMessage();
  const canOperate = canWrite && isAdmin;
  const [tabDirty, setTabDirty] = useState(false);

  const replaceTabQuery = useCallback(
    (tab: WebsiteTab) => {
      if (tab === currentTab) return;
      const switchTab = () => {
        setTabDirty(false);
        const params = new URLSearchParams(searchParams.toString());
        params.set("tab", tab);
        const qs = params.toString();
        router.replace(qs ? `/manage/system/website?${qs}` : "/manage/system/website");
      };
      if (tabDirty) {
        modal.confirm({
          title: "有未保存的更改",
          content: "当前标签页有尚未保存的配置修改，切换后将丢失这些修改，确定继续吗？",
          okText: "放弃并切换",
          okButtonProps: { danger: true },
          cancelText: "继续编辑",
          onOk: switchTab,
        });
        return;
      }
      switchTab();
    },
    [currentTab, modal, router, searchParams, tabDirty],
  );

  const renderTabPane = () => {
    switch (currentTab) {
      case "tip":
        return (
          <div className={styles.tabPaneScrollable}>
            <TipConfigCard canWrite={canOperate} onDirtyChange={setTabDirty} />
          </div>
        );
      case "notice":
        return (
          <div className={styles.tabPaneScrollable}>
            <NoticeConfigCard canWrite={canOperate} onDirtyChange={setTabDirty} />
          </div>
        );
      case "basic":
      default:
        return <BasicConfigTab canWrite={canOperate} onDirtyChange={setTabDirty} />;
    }
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          className={styles.pageHeader}
          title="网站配置"
          description="设置站点名称、公告和访问方式。"
        />
      )}

      {!isAdmin && (
        <Alert
          type="info"
          showIcon
          message="当前为只读查看模式，修改网站配置与运维选项需超级管理员权限"
          style={{ marginBottom: 16 }}
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
