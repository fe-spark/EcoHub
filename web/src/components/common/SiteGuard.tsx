"use client";

import React, { createContext, useContext, useEffect, useState } from "react";
import { usePathname } from "next/navigation";
import { Result } from "antd";
import { ApiGet } from "@/lib/client-api";
import AppLoading from "@/components/public/Loading";

export interface SiteConfig {
  siteName: string;
  domain: string;
  logo: string;
  keyword: string;
  describe: string;
  state: boolean;
  hint: string;
  isVideoProxy: boolean;
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
}: {
  children: React.ReactNode;
  initialConfig: SiteConfig | null;
}) {
  const pathname = usePathname();
  const [config, setConfig] = useState<SiteConfig | null>(initialConfig);
  const [loading, setLoading] = useState(!initialConfig);

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

  if (loading) {
    return (
      <div
        style={{
          minHeight: "100vh",
          position: "relative",
        }}
      >
        <AppLoading text="正在加载站点配置..." />
      </div>
    );
  }

  // 维护模式拦截 (非管理后台页面)
  const isManagePage = pathname.startsWith("/manage");
  const isLoginPage = pathname === "/login";

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
