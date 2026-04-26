"use client";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Button, Drawer, Empty, Input } from "antd";
import {
  SearchOutlined,
  HistoryOutlined,
  DeleteOutlined,
  MenuOutlined,
  HomeOutlined,
  FireOutlined,
  DownOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { clearHistoryMap, readHistoryMap } from "@/lib/historyStorage";

interface NavItem {
  id: string;
  name: string;
}

const QUICK_NAV_LIMIT = 8;

interface HistoryItem {
  id: string;
  name: string;
  episode: string;
  link: string;
  timeStamp: number;
}

export default function Header({ navList }: { navList: NavItem[] }) {
  const [keyword, setKeyword] = useState("");
  const { config: siteInfo } = useSiteConfig();
  const [historyList, setHistoryList] = useState<HistoryItem[]>([]);
  const [scrolled, setScrolled] = useState(false);
  const [mobileMenuVisible, setMobileMenuVisible] = useState(false);
  const [desktopCatalogOpen, setDesktopCatalogOpen] = useState(false);
  const router = useRouter();
  const searchParams = useSearchParams();
  const { message } = useAppMessage();
  const desktopCatalogRef = useRef<HTMLDivElement>(null);

  const urlSearch = searchParams.get("search") || "";
  useEffect(() => {
    setKeyword(urlSearch);
  }, [urlSearch]);

  useEffect(() => {
    const handleScroll = () => {
      const scrollY = window.scrollY || document.documentElement.scrollTop;
      setScrolled(scrollY > 20);
    };
    window.addEventListener("scroll", handleScroll);
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  const loadHistory = useCallback(() => {
    const historyMap = readHistoryMap();
    const list = Object.values(historyMap) as HistoryItem[];
    list.sort((a, b) => b.timeStamp - a.timeStamp);
    setHistoryList(list);
  }, []);

  const handleClearHistory = (e: React.MouseEvent) => {
    e.stopPropagation();
    clearHistoryMap();
    setHistoryList([]);
    message.success("已清空历史记录");
  };

  const handleSearch = () => {
    if (!keyword.trim()) {
      message.error("请输入搜索关键词");
      return;
    }
    router.push(`/search?search=${encodeURIComponent(keyword)}`);
  };

  const [showHistory, setShowHistory] = useState(false);
  const historyRef = useRef<HTMLDivElement>(null);
  const quickNavs = navList.slice(0, QUICK_NAV_LIMIT);

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (historyRef.current && !historyRef.current.contains(event.target as Node)) {
        setShowHistory(false);
      }
      if (desktopCatalogRef.current && !desktopCatalogRef.current.contains(event.target as Node)) {
        setDesktopCatalogOpen(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
  }, []);

  useEffect(() => {
    const handleEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setDesktopCatalogOpen(false);
        setMobileMenuVisible(false);
        setShowHistory(false);
      }
    };

    document.addEventListener("keydown", handleEscape);
    return () => document.removeEventListener("keydown", handleEscape);
  }, []);

  const toggleHistory = () => {
    const nextShow = !showHistory;
    setShowHistory(nextShow);
    if (nextShow) {
      loadHistory();
    }
  };

  const historyContent = (
    <div className={`${styles.historyPanel} ${showHistory ? styles.show : ""}`}>
      <div className={styles.historyHeader}>
        <HistoryOutlined className={styles.icon} />
        <span className={styles.title}>历史观看记录</span>
        {historyList.length > 0 && (
          <DeleteOutlined
            className={styles.clear}
            onClick={handleClearHistory}
          />
        )}
      </div>
      <div className={styles.historyList}>
        {historyList.length > 0 ? (
          historyList.map((item, idx) => (
            <div
              key={idx}
              className={styles.historyItem}
              onClick={() => {
                router.push(item.link);
                setShowHistory(false);
              }}
              style={{ cursor: "pointer" }}
            >
              <span className={styles.filmTitle}>{item.name}</span>
              <span className={styles.episode}>{item.episode}</span>
            </div>
          ))
        ) : (
          <div style={{ padding: '20px 0' }}>
            <Empty
              image={Empty.PRESENTED_IMAGE_SIMPLE}
              description="暂无观看记录"
            />
          </div>
        )}
      </div>
    </div>
  );

  const navigateToCategory = (id: string) => {
    setDesktopCatalogOpen(false);
    setMobileMenuVisible(false);
    router.push(`/filmClassify?Pid=${id}`);
  };

  return (
    <header className={`${styles.headerWrap} ${scrolled ? styles.scrolled : ""}`}>
      <div className={styles.headerInner}>
        {/* LOGO Area */}
        <div className={styles.logoArea}>
          <div className={styles.mobileMenuTrigger} onClick={() => setMobileMenuVisible(true)}>
            <MenuOutlined />
          </div>
          
          {siteInfo?.siteName && (
            <div className={styles.siteName} onClick={() => router.push("/")}>
              {/* 站点 logo 由后台配置提供，当前保持原生 img 避免额外远程域名配置 */}
              {/* eslint-disable-next-line @next/next/no-img-element */}
              {siteInfo.logo && <img src={siteInfo.logo} alt="logo" className={styles.logoImg} />}
              <span className={styles.logoText}>{siteInfo.siteName}</span>
            </div>
          )}
        </div>

        {/* Navigation Area - Dynamic & Flexible */}
        <div className={styles.navArea} ref={desktopCatalogRef}>
          <nav className={styles.navLinks}>
            <a onClick={() => router.push("/")} className={styles.navHomeItem}>
              首页
            </a>

            <div className={styles.navScroller}>
              {quickNavs.map((nav) => (
                <a
                  key={nav.id}
                  onClick={() => navigateToCategory(nav.id)}
                  className={styles.navItem}
                >
                  {nav.name}
                </a>
              ))}
            </div>

            <button
              type="button"
              className={`${styles.navCatalogBtn} ${desktopCatalogOpen ? styles.navCatalogBtnActive : ""}`}
              onClick={() => setDesktopCatalogOpen((open) => !open)}
            >
              分类全览 <DownOutlined className={styles.navCatalogIcon} />
            </button>
          </nav>

          <div className={`${styles.navCatalogPanel} ${desktopCatalogOpen ? styles.navCatalogPanelOpen : ""}`}>
            <div className={styles.navCatalogHeader}>
              <div>
                <span className={styles.navCatalogEyebrow}>Category Atlas</span>
                <strong className={styles.navCatalogTitle}>全部分类</strong>
              </div>
              <span className={styles.navCatalogCount}>{navList.length} 个分类</span>
            </div>
            <div className={styles.navCatalogGrid}>
              {navList.map((nav) => (
                <button
                  key={nav.id}
                  type="button"
                  className={styles.navCatalogItem}
                  onClick={() => navigateToCategory(nav.id)}
                >
                  <span className={styles.navCatalogName}>{nav.name}</span>
                </button>
              ))}
            </div>
          </div>
        </div>

        {/* Action Area - Search & Actions */}
        <div className={styles.actionArea}>
          <div className={styles.searchGroup}>
            <Input
              placeholder="搜索影片、动漫..."
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleSearch()}
              variant="borderless"
            />
            <Button 
              type="primary" 
              icon={<SearchOutlined />} 
              className={styles.searchBtn}
              onClick={handleSearch}
            />
          </div>

          <div className={styles.actions}>
            <div className={styles.historyWrapper} ref={historyRef}>
              <div 
                className={`${styles.actionBtn} ${showHistory ? styles.active : ""}`} 
                onClick={toggleHistory}
              >
                <HistoryOutlined />
              </div>
              {historyContent}
            </div>
            
            <div className={styles.mobileSearchBtn} onClick={() => router.push("/search")}>
              <SearchOutlined />
            </div>
          </div>
        </div>
      </div>

      <Drawer
        title={<div className={styles.drawerTitle}>{siteInfo?.siteName || "Menu"}</div>}
        placement="left"
        onClose={() => setMobileMenuVisible(false)}
        open={mobileMenuVisible}
        size={280}
        className={styles.mobileDrawer}
      >
        <div className={styles.mobileNav}>
          <div className={styles.mobileNavItem} onClick={() => { router.push("/"); setMobileMenuVisible(false); }}>
            <HomeOutlined /> <span>首页</span>
          </div>
          {navList.map((nav) => (
            <div 
              key={nav.id} 
              className={styles.mobileNavItem} 
              onClick={() => navigateToCategory(nav.id)}
            >
              <FireOutlined /> <span>{nav.name}</span>
            </div>
          ))}
        </div>
      </Drawer>
    </header>
  );
}
