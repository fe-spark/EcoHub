"use client";

import React from "react";
import { Drawer } from "antd";
import {
  HomeOutlined,
  FireOutlined,
  SunOutlined,
  MoonOutlined,
  DesktopOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";

interface NavItem {
  id: string | number;
  name: string;
}

interface MobileNavDrawerProps {
  open: boolean;
  siteName?: string;
  navList: NavItem[];
  isHomeActive: boolean;
  isCategoryActive: (id: string | number) => boolean;
  mode: string;
  onClose: () => void;
  onNavigateHome: () => void;
  onNavigateCategory: (id: string | number) => void;
  onSetMode: (mode: "light" | "dark" | "system") => void;
}

export default function MobileNavDrawer({
  open,
  siteName,
  navList,
  isHomeActive,
  isCategoryActive,
  mode,
  onClose,
  onNavigateHome,
  onNavigateCategory,
  onSetMode,
}: MobileNavDrawerProps) {
  return (
    <Drawer
      title={<div className={styles.drawerTitle}>{siteName || "Menu"}</div>}
      placement="left"
      onClose={onClose}
      open={open}
      size={280}
      zIndex={1300}
      className={styles.mobileDrawer}
    >
      <div className={styles.mobileDrawerBody}>
        <div className={styles.mobileNav}>
          <div
            className={`${styles.mobileNavItem} ${isHomeActive ? styles.mobileNavItemActive : ""}`}
            onClick={onNavigateHome}
          >
            <HomeOutlined /> <span>首页</span>
          </div>
          {navList.map((nav) => {
            const isActive = isCategoryActive(nav.id);
            return (
              <div
                key={nav.id}
                className={`${styles.mobileNavItem} ${isActive ? styles.mobileNavItemActive : ""}`}
                onClick={() => onNavigateCategory(nav.id)}
              >
                <FireOutlined /> <span>{nav.name}</span>
              </div>
            );
          })}
        </div>

        {/* 移动端抽屉底部：主题外观切换 */}
        <div className={styles.mobileThemeSection}>
          <div className={styles.mobileThemeLabel}>外观模式</div>
          <div className={styles.mobileThemeSegment}>
            <button
              type="button"
              className={`${styles.themeOptionBtn} ${mode === "light" ? styles.themeOptionActive : ""}`}
              onClick={() => onSetMode("light")}
            >
              <SunOutlined /> <span>浅色</span>
            </button>
            <button
              type="button"
              className={`${styles.themeOptionBtn} ${mode === "dark" ? styles.themeOptionActive : ""}`}
              onClick={() => onSetMode("dark")}
            >
              <MoonOutlined /> <span>深色</span>
            </button>
            <button
              type="button"
              className={`${styles.themeOptionBtn} ${mode === "system" ? styles.themeOptionActive : ""}`}
              onClick={() => onSetMode("system")}
            >
              <DesktopOutlined /> <span>系统</span>
            </button>
          </div>
        </div>
      </div>
    </Drawer>
  );
}
