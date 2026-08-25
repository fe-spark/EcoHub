"use client";

import React, {
  Suspense,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { Button, Drawer, Empty, Input } from "antd";
import {
  SearchOutlined,
  HistoryOutlined,
  DeleteOutlined,
  MenuOutlined,
  HomeOutlined,
  FireOutlined,
  DownOutlined,
  GithubOutlined,
} from "@ant-design/icons";
import styles from "./index.module.less";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { clearHistoryMap, readHistoryMap } from "@/lib/historyStorage";
import { usePublicContentLoading } from "@/components/public/PublicContentLoading";
import SiteLogo from "@/components/public/SiteLogo";
import { PROJECT_GITHUB_URL, DEFAULT_SITE_NAME } from "@/lib/project";

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

/**
 * 仅这一层调用 useSearchParams，并放在内层 Suspense 里。
 * 避免整棵 Header（含 logo img）因 query 变化被卸载重挂载而重新拉图。
 */
function SearchParamsBridge({
  onChange,
}: {
  onChange: (next: { search: string; pid: string | null }) => void;
}) {
  const searchParams = useSearchParams();
  const search = searchParams.get("search") || "";
  const pid = searchParams.get("Pid");

  useEffect(() => {
    onChange({ search, pid });
  }, [search, pid, onChange]);

  return null;
}

export default function Header({ navList }: { navList: NavItem[] }) {
  const [keyword, setKeyword] = useState("");
  const { config: siteInfo } = useSiteConfig();
  const [historyList, setHistoryList] = useState<HistoryItem[]>([]);
  const [scrolled, setScrolled] = useState(false);
  const [mobileMenuVisible, setMobileMenuVisible] = useState(false);
  const [desktopCatalogOpen, setDesktopCatalogOpen] = useState(false);
  const [activeCategoryId, setActiveCategoryId] = useState("");
  const [urlPid, setUrlPid] = useState<string | null>(null);

  const [isNavigating, setIsNavigating] = useState(false);
  const [pendingCategoryId, setPendingCategoryId] = useState<string | null>(null);

  const pathname = usePathname();
  const { message } = useAppMessage();
  const desktopCatalogRef = useRef<HTMLDivElement>(null);
  const {
    navigate: layoutNavigate,
    active: contentLoadingActive,
  } = usePublicContentLoading();

  const [keywordFromUrl, setKeywordFromUrl] = useState("");

  const onSearchParamsChange = useCallback(
    (next: { search: string; pid: string | null }) => {
      setKeyword(next.search);
      setKeywordFromUrl(next.search);
      setUrlPid(next.pid);
    },
    [],
  );

  useEffect(() => {
    const handleScroll = () => {
      const scrollY = window.scrollY || document.documentElement.scrollTop;
      setScrolled(scrollY > 20);
    };
    window.addEventListener("scroll", handleScroll);
    return () => window.removeEventListener("scroll", handleScroll);
  }, []);

  // 布局 content loading 结束后，同步清掉 Header 导航中状态
  if (isNavigating && !contentLoadingActive) {
    setIsNavigating(false);
    setPendingCategoryId(null);
  }

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
    const q = keyword.trim();
    if (!q) {
      message.error("请输入搜索关键词");
      return;
    }
    const href = `/search?search=${encodeURIComponent(q)}`;
    // 同 keyword 且已在搜索页：不重复导航
    if (pathname === "/search" && keywordFromUrl === q) {
      return;
    }
    beginHeaderNavigation("search", "搜索加载中...", href);
  };

  const [showHistory, setShowHistory] = useState(false);
  const historyRef = useRef<HTMLDivElement>(null);
  const quickNavs = navList.slice(0, QUICK_NAV_LIMIT);
  const activePid = pathname.startsWith("/filmClassify") ? urlPid : null;
  const isHomeActive = pathname === "/";
  const isCategoryActive = (id: string | number) => activeCategoryId === String(id);

  // 导航进行中保持乐观高亮，避免 URL 尚未更新时被 activePid 冲掉
  if (!isNavigating) {
    const nextActiveCategoryId = activePid ? String(activePid) : "";
    if (nextActiveCategoryId !== activeCategoryId) {
      setActiveCategoryId(nextActiveCategoryId);
    }
  }

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

  const beginHeaderNavigation = (pendingId: string, label: string, href: string) => {
    setPendingCategoryId(pendingId);
    setDesktopCatalogOpen(false);
    setMobileMenuVisible(false);
    setIsNavigating(true);
    // 走布局级 navigate：transition 不挂在会被卸载的页面组件上
    layoutNavigate(href, label);
  };

  const navigateToCategory = (id: string | number) => {
    const nextId = String(id);

    // 已在该分类页：不重复导航（避免整页内容 loading 卡死）
    if (
      pathname.startsWith("/filmClassify") &&
      activePid !== null &&
      String(activePid) === nextId
    ) {
      setDesktopCatalogOpen(false);
      setMobileMenuVisible(false);
      return;
    }

    if (isNavigating && pendingCategoryId === nextId) {
      return;
    }

    setActiveCategoryId(nextId);
    beginHeaderNavigation(
      nextId,
      "分类加载中...",
      `/filmClassify?Pid=${encodeURIComponent(nextId)}`,
    );
  };

  const navigateToHome = () => {
    if (pathname === "/") {
      return;
    }
    if (isNavigating && pendingCategoryId === "home") {
      return;
    }
    setActiveCategoryId("");
    beginHeaderNavigation("home", "首页加载中...", "/");
  };

  /** Logo 点击：优先跳转网站配置的 siteUrl，否则回前台首页 */
  const navigateFromLogo = () => {
    const siteUrl = String(siteInfo?.siteUrl || "").trim();
    if (siteUrl) {
      try {
        const target = new URL(siteUrl, window.location.origin);
        if (target.origin === window.location.origin) {
          const path = `${target.pathname}${target.search}${target.hash}` || "/";
          if (path === "/" || path === pathname) {
            if (pathname === "/") return;
            setActiveCategoryId("");
            beginHeaderNavigation("home", "首页加载中...", "/");
            return;
          }
          window.location.assign(target.href);
          return;
        }
        window.open(target.href, "_blank", "noopener,noreferrer");
        return;
      } catch {
        window.open(siteUrl, "_blank", "noopener,noreferrer");
        return;
      }
    }
    navigateToHome();
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
                setShowHistory(false);
                beginHeaderNavigation(`history:${item.link}`, "页面加载中...", item.link);
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

  return (
    <>
      {/* useSearchParams 仅挂在空桥接组件上，fallback 也为 null，不会拆掉 logo DOM */}
      <Suspense fallback={null}>
        <SearchParamsBridge onChange={onSearchParamsChange} />
      </Suspense>
      <header className={`${styles.headerWrap} ${scrolled ? styles.scrolled : ""}`}>
        <div className={styles.headerInner}>
          {/* LOGO Area：稳定挂载，导航时不 remount */}
          <div className={styles.logoArea}>
            <div className={styles.mobileMenuTrigger} onClick={() => setMobileMenuVisible(true)}>
              <MenuOutlined />
            </div>
            
            <div
              className={styles.siteName}
              onClick={navigateFromLogo}
              title={siteInfo?.siteUrl ? `打开 ${siteInfo.siteUrl}` : "回首页"}
            >
              <SiteLogo
                src={siteInfo?.logo}
                className={styles.logoImg}
                fetchPriority="high"
              />
              <span className={styles.logoText}>{siteInfo?.siteName || DEFAULT_SITE_NAME}</span>
            </div>
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
              <a
                href={PROJECT_GITHUB_URL}
                target="_blank"
                rel="noopener noreferrer"
                className={styles.actionBtn}
                title="打开 GitHub 项目地址"
                aria-label="打开 GitHub 项目地址"
              >
                <GithubOutlined />
              </a>
              <div className={styles.historyWrapper} ref={historyRef}>
                <div 
                  className={`${styles.actionBtn} ${showHistory ? styles.active : ""}`} 
                  onClick={toggleHistory}
                >
                  <HistoryOutlined />
                </div>
                {historyContent}
              </div>
              
              <div
                className={styles.mobileSearchBtn}
                onClick={() => {
                  beginHeaderNavigation("search", "搜索加载中...", "/search");
                }}
              >
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
            <a
              href={PROJECT_GITHUB_URL}
              target="_blank"
              rel="noopener noreferrer"
              className={styles.mobileNavItem}
              title="打开 GitHub 项目地址"
            >
              <GithubOutlined /> <span>GitHub 项目地址</span>
            </a>
          </div>
        </Drawer>
      </header>
    </>
  );
}
