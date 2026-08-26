"use client";

import React, { Suspense } from "react";
import Header from "@/components/public/Header";
import Footer from "@/components/public/Footer";
import ScrollToTop from "@/components/public/ScrollToTop";
import NoticeModal from "@/components/public/NoticeModal";
import { useSiteConfig } from "@/components/common/SiteGuard";
import {
  PublicContentLoadingProvider,
  PublicContentLoadingPanel,
  usePublicContentLoading,
} from "@/components/public/PublicContentLoading";
import styles from "./index.module.less";

interface NavItem {
  id: string;
  name: string;
}

function PublicMain({ children }: { children: React.ReactNode }) {
  const { active, label } = usePublicContentLoading();

  return (
    <main className={`${styles.publicMain} page-entry`}>
      {/*
        必须同时保留 children 挂载（display:none），不能 {active ? loading : children}：
        否则发起 navigate 的页面会卸载，useTransition 收尾监听消失 → API 200 仍永远 loading。
      */}
      {active ? (
        <PublicContentLoadingPanel
          className={styles.publicMainLoading}
          label={label}
          fill
        />
      ) : null}
      <div
        className={active ? styles.mainContentHidden : styles.mainContentVisible}
        aria-hidden={active}
      >
        {children}
      </div>
    </main>
  );
}

export default function PublicLayoutView({
  children,
  navList,
}: {
  children: React.ReactNode;
  navList: NavItem[];
}) {
  const { config } = useSiteConfig();

  return (
    // useSearchParams 在 Provider 内：包一层 Suspense 避免 CSR bailout 警告
    <Suspense fallback={null}>
      <PublicContentLoadingProvider>
        <div className={styles.layoutWrapper}>
          <ScrollToTop />
          <Header navList={navList} />
          <PublicMain>{children}</PublicMain>
          <Footer />
          <NoticeModal notice={config?.notice} />
        </div>
      </PublicContentLoadingProvider>
    </Suspense>
  );
}
