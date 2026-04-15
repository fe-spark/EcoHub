"use client";

import React from "react";
import { useRouter, usePathname } from "next/navigation";
import {
  HomeOutlined,
  SyncOutlined,
  HistoryOutlined,
  HeartOutlined,
  UserOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";
import { useSiteConfig } from "@/components/common/SiteGuard";

export default function Footer() {
  const { config } = useSiteConfig();
  const currentYear = new Date().getFullYear();

  return (
    <footer className={styles.footer}>
      <div className={styles.footerInfo}>
        {/* 站点 logo 来自后台配置的动态地址，这里不强依赖 next/image 的远程配置 */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        {config?.logo && <img src={config.logo} alt="logo" className={styles.footerLogo} />}
        <span className={styles.footerSiteName}>{config?.siteName}</span>
      </div>
      <p className={styles.copyright}>
        © {currentYear} {config?.siteName}. All rights reserved.
      </p>
      <p className={styles.disclaimer}>
        本站所有内容均来自互联网分享站点所提供的公开引用资源，未提供资源上传、存储服务。
      </p>
    </footer>
  );
}
