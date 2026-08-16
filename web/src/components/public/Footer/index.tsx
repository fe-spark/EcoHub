"use client";

import React from "react";
import { useRouter, usePathname } from "next/navigation";
import {
  GithubOutlined,
  HomeOutlined,
  SyncOutlined,
  HistoryOutlined,
  HeartOutlined,
  UserOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";
import { useSiteConfig } from "@/components/common/SiteGuard";
import SiteLogo from "@/components/public/SiteLogo";
import { PROJECT_GITHUB_URL } from "@/lib/project";

export default function Footer() {
  const { config } = useSiteConfig();
  const currentYear = new Date().getFullYear();

  return (
    <footer className={styles.footer}>
      <div className={styles.footerInfo}>
        <SiteLogo
          src={config?.logo}
          className={styles.footerLogo}
          fetchPriority="low"
        />
        <span className={styles.footerSiteName}>{config?.siteName}</span>
      </div>
      <p className={styles.copyright}>
        © {currentYear} {config?.siteName}. All rights reserved.
      </p>
      <p className={styles.projectLink}>
        <a
          href={PROJECT_GITHUB_URL}
          target="_blank"
          rel="noopener noreferrer"
          title="打开 GitHub 项目地址"
        >
          <GithubOutlined />
          <span>GitHub 项目地址</span>
        </a>
      </p>
      <p className={styles.disclaimer}>
        本站所有内容均来自互联网分享站点所提供的公开引用资源，未提供资源上传、存储服务。
      </p>
    </footer>
  );
}
