"use client";

import React, { useState, useEffect, useCallback, useRef } from "react";
import { useRouter, useSearchParams } from "next/navigation";
import { Input, Button, Empty, Drawer, Flex, Dropdown } from "antd";
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
  const router = useRouter();
  const searchParams = useSearchParams();
  const { message } = useAppMessage();

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

  useEffect(() => {
    const handleClickOutside = (event: MouseEvent) => {
      if (historyRef.current && !historyRef.current.contains(event.target as Node)) {
        setShowHistory(false);
      }
    };
    document.addEventListener("mousedown", handleClickOutside);
    return () => document.removeEventListener("mousedown", handleClickOutside);
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

  const [visibleCount, setVisibleCount] = useState(navList.length);
  const containerRef = useRef<HTMLDivElement>(null);
  const itemsRef = useRef<(HTMLAnchorElement | null)[]>([]);
  const homeRef = useRef<HTMLAnchorElement>(null);

  // Calculate visible items based on container width
  useEffect(() => {
    if (!containerRef.current || navList.length === 0) return;

    const observer = new ResizeObserver((entries) => {
      const entry = entries[0];
      const containerWidth = entry.contentRect.width;
      
      if (containerWidth <= 0) return;

      const homeWidth = homeRef.current?.getBoundingClientRect().width || 64;
      // Get the actual gap from computed style if possible, or use a reasonable estimate
      const computedGap = window.getComputedStyle(containerRef.current).gap;
      const gap = parseInt(computedGap) || 24; 
      const moreBtnBuffer = 100; // More button + icon + padding
      
      let currentWidth = homeWidth;
      let count = 0;

      for (let i = 0; i < navList.length; i++) {
        const itemWidth = itemsRef.current[i]?.getBoundingClientRect().width || 0;
        if (itemWidth === 0) continue;

        const isLast = i === navList.length - 1;
        const spaceNeeded = gap + itemWidth + (isLast ? 0 : moreBtnBuffer);

        if (currentWidth + spaceNeeded > containerWidth) {
          break;
        }
        
        currentWidth += itemWidth + gap;
        count++;
      }
      
      setVisibleCount(count);
    });

    observer.observe(containerRef.current);
    return () => observer.disconnect();
  }, [navList.length]);

  const visibleNavs = navList.slice(0, visibleCount);
  const overflowNavs = navList.slice(visibleCount);

  const moreMenu = {
    items: overflowNavs.map((nav) => ({
      key: nav.id,
      label: nav.name,
      onClick: () => router.push(`/filmClassify?Pid=${nav.id}`),
    })),
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
        <div className={styles.navArea} ref={containerRef}>
          <nav className={styles.navLinks}>
            <a onClick={() => router.push("/")} className={styles.navItem} ref={homeRef}>
              首页
            </a>
            
            {/* Hidden items for width measurement */}
            <div style={{ position: 'absolute', visibility: 'hidden', pointerEvents: 'none', display: 'flex', gap: '24px', opacity: 0 }}>
              {navList.map((nav, i) => (
                <a 
                  key={`measure-${nav.id}`} 
                  ref={el => { itemsRef.current[i] = el; }} 
                  className={styles.navItem}
                >
                  {nav.name}
                </a>
              ))}
            </div>

            {visibleNavs.map((nav) => (
              <a
                key={nav.id}
                onClick={() => router.push(`/filmClassify?Pid=${nav.id}`)}
                className={styles.navItem}
              >
                {nav.name}
              </a>
            ))}

            {overflowNavs.length > 0 && (
              <Dropdown menu={moreMenu} placement="bottomRight" trigger={['hover']} overlayClassName={styles.navMoreOverlay}>
                <a className={styles.navItem}>
                  更多 <DownOutlined style={{ fontSize: 12, marginLeft: 4 }} />
                </a>
              </Dropdown>
            )}
          </nav>
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
              onClick={() => { router.push(`/filmClassify?Pid=${nav.id}`); setMobileMenuVisible(false); }}
            >
              <FireOutlined /> <span>{nav.name}</span>
            </div>
          ))}
        </div>
      </Drawer>
    </header>
  );
}
