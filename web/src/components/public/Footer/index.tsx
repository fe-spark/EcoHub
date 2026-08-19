"use client";

import React, { useState } from "react";
import { GithubOutlined, HeartOutlined } from "@ant-design/icons";
import styles from "./index.module.less";
import { useSiteConfig } from "@/components/common/SiteGuard";
import SiteLogo from "@/components/public/SiteLogo";
import TipModal from "@/components/public/TipModal";
import { PROJECT_GITHUB_URL } from "@/lib/project";
import { hasVisibleTip } from "@/lib/tip";

export default function Footer() {
  const { config } = useSiteConfig();
  const currentYear = new Date().getFullYear();
  const [tipOpen, setTipOpen] = useState(false);
  const showTip = hasVisibleTip(config?.tip);

  return (
    <footer className={styles.footer}>
      <div className={styles.bar}>
        <div className={styles.brand}>
          <SiteLogo
            src={config?.logo}
            className={styles.footerLogo}
            fetchPriority="low"
          />
          <span className={styles.footerSiteName}>{config?.siteName}</span>
        </div>
        <p className={styles.meta}>
          <span>© {currentYear} {config?.siteName}</span>
          <span className={styles.dot} aria-hidden>
            ·
          </span>
          <a
            href={PROJECT_GITHUB_URL}
            target="_blank"
            rel="noopener noreferrer"
            title="打开 GitHub 项目地址"
          >
            <GithubOutlined />
            <span>GitHub</span>
          </a>
          {showTip ? (
            <>
              <span className={styles.dot} aria-hidden>
                ·
              </span>
              <button
                type="button"
                className={styles.tipBtn}
                onClick={() => setTipOpen(true)}
                aria-label="打开赞赏"
              >
                <HeartOutlined />
                <span>赞赏</span>
              </button>
            </>
          ) : null}
        </p>
      </div>
      <p className={styles.disclaimer}>
        本站所有内容均来自互联网分享站点所提供的公开引用资源，未提供资源上传、存储服务。
      </p>
      {showTip ? (
        <TipModal open={tipOpen} tip={config?.tip} onClose={() => setTipOpen(false)} />
      ) : null}
    </footer>
  );
}
