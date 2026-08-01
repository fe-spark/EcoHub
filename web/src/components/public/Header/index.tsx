"use client";

import React, { useCallback, useEffect, useRef, useState, useTransition } from "react";
import { createPortal } from "react-dom";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { Button, Drawer, Empty, Input } from "antd";
import {
  SearchOutlined,
  HistoryOutlined,
  DeleteOutlined,
  MenuOutlined,
  HomeOutlined,
  FireOutlined,
  DownOutlined,
  LoadingOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { clearHistoryMap, readHistoryMap } from "@/lib/historyStorage";
import {
  startNavigationLoading,
  finishNavigationLoading,
  forceFinishNavigationLoading,
} from "@/components/public/TopLoadingBar";

interface NavItem {
  id: string | number;
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
  const [activeCategoryId, setActiveCategoryId] = useState("");
  
  const [mounted, setMounted] = useState(false);
  const [isNavigating, setIsNavigating] = useState(false);
  const [isPending, startTransition] = useTransition();
  const [pendingCategoryId, setPendingCategoryId] = useState<string | null>(null);

  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const { message } = useAppMessage();
  const desktopCatalogRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    setMounted(true);
  }, []);

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

  // 监听路由变化完成，关闭遮罩与 Loading 状态
  useEffect(() => {
    setIsNavigating(false);
    setPendingCategoryId(null);
    forceFinishNavigationLoading();
  }, [pathname, searchParams]);

  // 移动端/桌面端加载遮罩开启时强制锁定背景滚动
  useEffect(() => {
    if (isNavigating && !pathname.startsWith("/search")) {
      const originalOverflow = document.body.style.overflow;
      const originalTouchAction = document.body.style.touchAction;
      document.body.style.overflow = "hidden";
      document.body.style.touchAction = "none";

      const preventTouch = (e: TouchEvent) => {
        e.preventDefault();
      };

      document.addEventListener("touchmove", preventTouch, { passive: false });

      return () => {
        document.body.style.overflow = originalOverflow;
        document.body.style.touchAction = originalTouchAction;
        document.removeEventListener("touchmove", preventTouch);
      };
    }
  }, [isNavigating, pathname]);

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
    setPendingCategoryId("search");
    startNavigationLoading("搜索加载中...");
    startTransition(() => {
      router.push(`/search?search=${encodeURIComponent(keyword)}`);
    });
  };

  const [showHistory, setShowHistory] = useState(false);
  const historyRef = useRef<HTMLDivElement>(null);
  const quickNavs = navList.slice(0, QUICK_NAV_LIMIT);
  const activePid = pathname.startsWith("/filmClassify") ? searchParams.get("Pid") : null;
  const isHomeActive = pathname === "/";
  const isCategoryActive = (id: string | number) => activeCategoryId === String(id);

  useEffect(() => {
    setActiveCategoryId(activePid ? String(activePid) : "");
  }, [activePid]);

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

  const navigateToCategory = (id: string | number) => {
    const nextId = String(id);

    if (isNavigating && pendingCategoryId === nextId) {
      return;
    }

    setPendingCategoryId(nextId);
    setActiveCategoryId(nextId);
    setDesktopCatalogOpen(false);
    setMobileMenuVisible(false);
    setIsNavigating(true);

    startNavigationLoading("分类加载中...");

    startTransition(() => {
      router.push(`/filmClassify?Pid=${encodeURIComponent(nextId)}`);
    });
  };

  const navigateToHome = () => {
    if (isNavigating) return;
    setPendingCategoryId("home");
    setActiveCategoryId("");
    setMobileMenuVisible(false);
    setIsNavigating(true);
    startNavigationLoading("首页加载中...");
    startTransition(() => {
      router.push("/");
    });
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
                setIsNavigating(true);
                startNavigationLoading("页面加载中...");
                startTransition(() => {
                  router.push(item.link);
                });
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

  const headerHeight = scrolled ? 64 : 80;
  const isSearchPage = pathname.startsWith("/search");
  const showMask = mounted && isNavigating && !isSearchPage;

  return (
    <>
      {/* 内容区域遮罩（搜索页不展示暗色遮罩，确保移动端搜索框 100% 亮起不被挡） */}
      {showMask && createPortal(
        <div
          onTouchMove={(e) => e.preventDefault()}
          style={{
            position: "fixed",
            top: `${headerHeight}px`,
            left: 0,
            right: 0,
            bottom: 0,
            width: "100vw",
            height: `calc(100vh - ${headerHeight}px)`,
            zIndex: 999,
            display: "flex",
            flexDirection: "column",
            alignItems: "center",
            justifyContent: "center",
            backgroundColor: "rgba(10, 12, 16, 0.75)",
            backdropFilter: "blur(16px)",
            WebkitBackdropFilter: "blur(16px)",
            pointerEvents: "all",
            userSelect: "none",
            margin: 0,
            padding: 0,
            transition: "top 0.3s ease, height 0.3s ease",
          }}
        >
          <div
            style={{
              display: "flex",
              flexDirection: "column",
              alignItems: "center",
              justifyContent: "center",
              padding: "32px 48px",
              borderRadius: "20px",
              backgroundColor: "rgba(24, 28, 36, 0.95)",
              border: "1px solid rgba(250, 173, 20, 0.35)",
              boxShadow: "0 20px 50px rgba(0, 0, 0, 0.6), 0 0 35px rgba(250, 173, 20, 0.2)",
            }}
          >
            <LoadingOutlined style={{ fontSize: 44, color: "#faad14", marginBottom: 16 }} />
            <span style={{ color: "#ffffff", fontSize: 16, fontWeight: 700, letterSpacing: "0.5px" }}>
              正在加载...
            </span>
          </div>
        </div>,
        document.body
      )}

      <header className={`${styles.headerWrap} ${scrolled ? styles.scrolled : ""}`}>
        <div className={styles.headerInner}>
          {/* LOGO Area */}
          <div className={styles.logoArea}>
            <div className={styles.mobileMenuTrigger} onClick={() => setMobileMenuVisible(true)}>
              <MenuOutlined />
            </div>
            
            {siteInfo?.siteName && (
              <div className={styles.siteName} onClick={navigateToHome}>
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
              <a
                onClick={navigateToHome}
                className={`${styles.navHomeItem} ${isHomeActive ? styles.navHomeItemActive : ""}`}
              >
                首页
              </a>

              <div className={styles.navScroller}>
                {quickNavs.map((nav) => {
                  const isActive = isCategoryActive(nav.id);
                  return (
                    <a
                      key={nav.id}
                      onClick={() => navigateToCategory(nav.id)}
                      className={`${styles.navItem} ${isActive ? styles.navItemActive : ""}`}
                    >
                      {nav.name}
                    </a>
                  );
                })}
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
                {navList.map((nav) => {
                  const isActive = isCategoryActive(nav.id);
                  return (
                    <button
                      key={nav.id}
                      type="button"
                      className={`${styles.navCatalogItem} ${isActive ? styles.navCatalogItemActive : ""}`}
                      onClick={() => navigateToCategory(nav.id)}
                      disabled={isNavigating}
                    >
                      <span className={styles.navCatalogName}>{nav.name}</span>
                    </button>
                  );
                })}
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
              
              <div className={styles.mobileSearchBtn} onClick={() => {
                startNavigationLoading("搜索加载中...");
                startTransition(() => {
                  router.push("/search");
                });
              }}>
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
            <div
              className={`${styles.mobileNavItem} ${isHomeActive ? styles.mobileNavItemActive : ""}`}
              onClick={navigateToHome}
            >
              <HomeOutlined /> <span>首页</span>
            </div>
            {navList.map((nav) => {
              const isActive = isCategoryActive(nav.id);
              return (
                <div 
                  key={nav.id} 
                  className={`${styles.mobileNavItem} ${isActive ? styles.mobileNavItemActive : ""}`}
                  onClick={() => navigateToCategory(nav.id)}
                >
                  <FireOutlined /> <span>{nav.name}</span>
                </div>
              );
            })}
          </div>
        </Drawer>
      </header>
    </>
  );
}
