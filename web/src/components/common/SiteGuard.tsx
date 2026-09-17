"use client";

import React, { createContext, useContext, useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { Result } from "antd";
import { ApiGet } from "@/lib/client-api";
import AppLoading from "@/components/public/Loading";
import type { TipConfig } from "@/lib/tip";
import type { NoticeConfig } from "@/lib/notice";

export interface SiteConfig {
  siteName: string;
  /** 网站访问地址（公网根），Logo 跳转与站外链接复用 */
  siteUrl?: string;
  logo: string;
  keyword: string;
  describe: string;
  state: boolean;
  hint: string;
  privateAccess?: boolean;
  provideKey?: string;
  tip?: TipConfig;
  notice?: NoticeConfig;
}

interface SiteConfigContextType {
  config: SiteConfig | null;
  loading: boolean;
  refresh: () => Promise<void>;
}

const SiteConfigContext = createContext<SiteConfigContextType>({
  config: null,
  loading: true,
  refresh: async () => { },
});

export const useSiteConfig = () => useContext(SiteConfigContext);

export default function SiteGuard({
  children,
  initialConfig,
  initialHasAuth = false,
}: {
  children: React.ReactNode;
  initialConfig: SiteConfig | null;
  initialHasAuth?: boolean;
}) {
  const pathname = usePathname();
  const router = useRouter();
  const [config, setConfig] = useState<SiteConfig | null>(initialConfig);
  const [loading, setLoading] = useState(!initialConfig);
  const [hasAuth, setHasAuth] = useState(initialHasAuth);

  useEffect(() => {
    setHasAuth(initialHasAuth);
  }, [initialHasAuth]);

  const fetchConfig = async () => {
    try {
      const resp = await ApiGet("/config/basic");
      if (resp.code === 0) {
        setConfig(resp.data);
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (!initialConfig) {
      void fetchConfig();
    }
  }, [initialConfig]);

  // 页面类型判断
  const isManagePage = pathname.startsWith("/manage");
  const isLoginPage = pathname === "/login";

  // 私有化模式拦截检测
  const isPrivateRequired = Boolean(config?.privateAccess);

  useEffect(() => {
    if (!loading && isPrivateRequired && !hasAuth && !isManagePage && !isLoginPage) {
      const query = typeof window !== "undefined" ? window.location.search : "";
      const target = encodeURIComponent(pathname + query);
      router.replace(`/login?redirect=${target}`);
    }
  }, [loading, isPrivateRequired, hasAuth, isManagePage, isLoginPage, pathname, router]);

  if (loading || (isPrivateRequired && !hasAuth && !isManagePage && !isLoginPage)) {
    return (
      <div
        style={{
          minHeight: "100vh",
          position: "relative",
        }}
      >
        <AppLoading text={loading ? "正在加载站点配置..." : "正在验证身份..."} />
      </div>
    );
  }

  // 维护模式拦截 (非管理后台页面)
  if (config && !config.state && !isManagePage && !isLoginPage) {
    return (
      <div style={{ height: "100vh", display: "flex", alignItems: "center", justifyContent: "center", padding: 20 }}>
        <Result
          status="warning"
          title="网站维护中"
          subTitle={config.hint || "由于系统维护工作，本站暂时无法访问，请稍后再试。"}
        />
      </div>
    );
  }

  return (
    <SiteConfigContext.Provider value={{ config, loading, refresh: fetchConfig }}>
      {children}
    </SiteConfigContext.Provider>
  );
}
