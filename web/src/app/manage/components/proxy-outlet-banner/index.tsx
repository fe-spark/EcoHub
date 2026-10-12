"use client";

import React, { useEffect, useState } from "react";
import { Tag, Button, Spin } from "antd";
import {
  GlobalOutlined,
  DisconnectOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet } from "@/lib/client-api";
import styles from "./index.module.less";

export type ProxyModuleType = "tmdb" | "notify" | "spider" | "upgrade";

interface ProxyConfigResp {
  enabled: boolean;
  proxyUrl: string;
  modules?: {
    spider?: boolean;
    tmdb?: boolean;
    notify?: boolean;
    upgrade?: boolean;
  };
}

interface ProxyOutletBannerProps {
  module: ProxyModuleType;
  className?: string;
}

export default function ProxyOutletBanner({
  module,
  className,
}: ProxyOutletBannerProps) {
  const router = useRouter();
  const [loading, setLoading] = useState(true);
  const [globalEnabled, setGlobalEnabled] = useState(false);
  const [moduleEnabled, setModuleEnabled] = useState(false);
  const [proxyUrl, setProxyUrl] = useState("");

  useEffect(() => {
    let unmounted = false;
    const fetchStatus = async () => {
      try {
        const resp = await ApiGet<ProxyConfigResp>("/manage/proxy/config");
        if (!unmounted && resp.code === 0 && resp.data) {
          const gEnabled = Boolean(resp.data.enabled && resp.data.proxyUrl?.trim());
          const mEnabled = Boolean(resp.data.modules?.[module] ?? true);
          setGlobalEnabled(gEnabled);
          setModuleEnabled(mEnabled);
          setProxyUrl(resp.data.proxyUrl || "");
        }
      } catch {
        // 容错直连
      } finally {
        if (!unmounted) {
          setLoading(false);
        }
      }
    };
    void fetchStatus();
    return () => {
      unmounted = true;
    };
  }, [module]);

  const handleNavigate = () => {
    router.push("/manage/system/proxy");
  };

  if (loading) {
    return (
      <div className={`${styles.banner} ${className || ""}`}>
        <div className={styles.left}>
          <Spin size="small" />
          <span className={styles.desc}>正在检查网络出站状态...</span>
        </div>
      </div>
    );
  }

  const isProxyActive = globalEnabled && moduleEnabled;

  return (
    <div className={`${styles.banner} ${className || ""}`}>
      <div className={styles.left}>
        {isProxyActive ? (
          <>
            <Tag color="processing" icon={<GlobalOutlined />}>
              统一网络代理
            </Tag>
            <span className={styles.title}>
              当前外部请求经由代理转发
              {proxyUrl ? ` (${proxyUrl})` : ""}
            </span>
          </>
        ) : (
          <>
            <Tag color="default" icon={<DisconnectOutlined />}>
              网络直连
            </Tag>
            <span className={styles.desc}>
              {!globalEnabled
                ? "系统全局网络代理未开启，当前外部请求直连访问"
                : "全局网络代理已开启，但本模块代理通道已关闭，当前直连访问"}
            </span>
          </>
        )}
      </div>
      <Button
        type="link"
        size="small"
        icon={<SettingOutlined />}
        className={styles.actionBtn}
        onClick={handleNavigate}
      >
        {!globalEnabled ? "配置网络代理" : !moduleEnabled ? "开启模块代理" : "管理网络代理"}
      </Button>
    </div>
  );
}
