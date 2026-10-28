"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import { useRouter, usePathname } from "next/navigation";
import Link from "next/link";
import {
  Layout,
  Menu,
  Avatar,
  Button,
  Space,
  Dropdown,
  Tag,
  Drawer,
  Grid,
  Alert,
} from "antd";
import {
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  LogoutOutlined,
  UserOutlined,
  BgColorsOutlined,
  SunOutlined,
  MoonOutlined,
  DesktopOutlined,
  GithubOutlined,
  QuestionCircleOutlined,
} from "@ant-design/icons";
import { PROJECT_GITHUB_URL, DEFAULT_SITE_NAME } from "@/lib/project";

import type { MenuProps } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { useThemeMode } from "@/components/theme/GlobalThemeProvider";
import type { ThemeMode } from "@/components/theme/ThemeDock";
import { resolveSiteLogoSrc } from "@/components/public/SiteLogo";
import { ManagePermissionProvider } from "@/lib/manage-permission";
import ManageTour, { replayManageTour } from "@/app/manage/components/manage-tour";
import SiderVersion from "@/app/manage/components/sider-version";
import {
  menuItems,
  resolveMenuKey,
  resolveParentMenuKeys,
  themeModeLabels,
} from "./menu-config";
import styles from "./index.module.less";

type AdminNotice = {
  level?: string;
  code?: string;
  message: string;
  actionPath?: string;
  actionText?: string;
};

const { Sider, Header, Content } = Layout;
const { useBreakpoint } = Grid;

export default function ManageLayoutView({
  children,
}: {
  children: React.ReactNode;
}) {
  const [collapsed, setCollapsed] = useState(false);
  const [drawerOpen, setDrawerOpen] = useState(false);
  const { config: siteInfo } = useSiteConfig();
  const { mode, setMode } = useThemeMode();
  const [userInfo, setUserInfo] = useState<any>(null);
  const [notices, setNotices] = useState<AdminNotice[]>([]);
  const screens = useBreakpoint();
  const [mounted, setMounted] = useState(false);

  useEffect(() => {
    setMounted(true);
  }, []);

  const isMobile = mounted && screens.lg === false;

  const router = useRouter();
  const pathname = usePathname();
  const isFixedTabbedPage = pathname.startsWith("/manage/system/website");
  const selectedKey = resolveMenuKey(pathname);
  const [openKeys, setOpenKeys] = useState<string[]>(() => resolveParentMenuKeys(menuItems, resolveMenuKey(pathname)));

  useEffect(() => {
    const parentKeys = resolveParentMenuKeys(menuItems, selectedKey);
    if (parentKeys.length > 0) {
      setOpenKeys((prev) => Array.from(new Set([...prev, ...parentKeys])));
    }
  }, [selectedKey]);
  const [accessVisible, setAccessVisible] = useState<boolean | null>(null);

  const fetchAccessVisible = useCallback(() => {
    ApiGet<{ enabled: boolean }>("/manage/access/status")
      .then((resp) => {
        if (resp.code === 0 && resp.data) {
          setAccessVisible(Boolean(resp.data.enabled));
        } else {
          setAccessVisible(false);
        }
      })
      .catch(() => {
        setAccessVisible(false);
      });
  }, []);

  useEffect(() => {
    fetchAccessVisible();
    const handleConfigChange = (e: Event) => {
      const customEvent = e as CustomEvent<{ enabled: boolean }>;
      if (customEvent?.detail?.enabled !== undefined) {
        setAccessVisible(Boolean(customEvent.detail.enabled));
      } else {
        fetchAccessVisible();
      }
    };
    window.addEventListener("ecohub:access-config-changed", handleConfigChange);
    return () => {
      window.removeEventListener("ecohub:access-config-changed", handleConfigChange);
    };
  }, [fetchAccessVisible]);

  useEffect(() => {
    ApiGet("/manage/user/info").then((resp) => {
      if (resp.code === 0) {
        setUserInfo(resp.data);
      }
    });
  }, []);

  useEffect(() => {
    if (
      pathname !== "/manage/system" &&
      pathname.startsWith("/manage/system") &&
      !pathname.startsWith("/manage/system/website")
    ) {
      if (userInfo && !userInfo.isAdmin) {
        router.replace("/manage");
      }
    }
  }, [userInfo, pathname, router]);
  // 进入后台及路由切换时刷新公告（数据重置后应消失）
  useEffect(() => {
    ApiGet("/manage/index").then((resp) => {
      if (resp.code === 0 && Array.isArray(resp.data?.notices)) {
        setNotices(resp.data.notices as AdminNotice[]);
      }
    });
  }, [pathname]);

  const onMenuClick: MenuProps["onClick"] = ({ key }) => {
    if (isMobile) {
      setDrawerOpen(false);
    }
    router.push(key);
  };

  const handleLogout = async () => {
    try {
      await ApiPost("/logout");
    } catch {
    } finally {
      router.replace("/login");
    }
  };

  const onNeedMenu = useCallback((need: boolean) => {
    if (isMobile) {
      setDrawerOpen(need);
      return;
    }
    if (need) {
      setCollapsed(false);
    }
  }, [isMobile]);

  const visibleMenuItems = useMemo(() => {
    const isAdmin = Boolean(userInfo?.isAdmin);
    return menuItems
      .filter((item) => {
        if (item?.key === "/manage/access") return Boolean(accessVisible);
        if (item?.key === "/manage/system/proxy" || item?.key === "sub-integrations") return isAdmin;
        return true;
      })
      .map((item) => {
        if (!item || !("children" in item) || !item.children) return item;
        const filteredChildren = item.children.filter((child) => {
          if (
            child &&
            "key" in child &&
            (child.key === "/manage/system/security" ||
              child.key === "/manage/system/logs" ||
              child.key === "/manage/system/users")
          ) {
            return isAdmin;
          }
          return true;
        });
        return { ...item, children: filteredChildren };
      });
  }, [userInfo?.isAdmin, accessVisible]);
  const themeMenuItems: MenuProps["items"] = [
    {
      key: "light",
      icon: <SunOutlined />,
      label: themeModeLabels.light,
    },
    {
      key: "dark",
      icon: <MoonOutlined />,
      label: themeModeLabels.dark,
    },
    {
      key: "system",
      icon: <DesktopOutlined />,
      label: themeModeLabels.system,
    },
  ];

  const menuNode = (
    <>
      <div
        className={styles.logoWrap}
        onClick={() => {
          const url = String(siteInfo?.siteUrl || "").trim() || "/";
          window.open(url, "_blank", "noopener,noreferrer");
        }}
        title={siteInfo?.siteUrl ? `打开 ${siteInfo.siteUrl}` : "打开前台首页"}
      >
        <Avatar
          src={resolveSiteLogoSrc(siteInfo?.logo)}
          size={34}
          shape="square"
          className={styles.logoIcon}
        />
        {(!collapsed || isMobile) && (
          <span className={styles.siteName}>{siteInfo?.siteName || DEFAULT_SITE_NAME}</span>
        )}
      </div>
      <div className={styles.menuTourTarget}>
        <Menu
          mode="inline"
          className={styles.menu}
          style={{ borderInlineEnd: 0 }}
          selectedKeys={[selectedKey]}
          openKeys={collapsed ? [] : openKeys}
          onOpenChange={(keys) => {
            if (!collapsed) {
              setOpenKeys(keys);
            }
          }}
          items={visibleMenuItems}
          onClick={onMenuClick}
        />
      </div>
      <SiderVersion
        collapsed={collapsed && !isMobile}
        isAdmin={Boolean(userInfo?.isAdmin)}
      />
    </>
  );

  return (
    <Layout className={styles.layout} hasSider={!isMobile}>
      {!isMobile ? (
        <Sider
          trigger={null}
          collapsible
          collapsed={collapsed}
          className={styles.sider}
          theme="light"
          width={240}
          collapsedWidth={80}
        >
          <div className={styles.siderInner}>{menuNode}</div>
        </Sider>
      ) : null}
      <Layout className={styles.mainLayout}>
        <Header className={styles.header}>
          <Space size="middle">
            <Button
              type="text"
              icon={
                isMobile ? (
                  <MenuUnfoldOutlined />
                ) : collapsed ? (
                  <MenuUnfoldOutlined />
                ) : (
                  <MenuFoldOutlined />
                )
              }
              onClick={() => {
                if (isMobile) {
                  setDrawerOpen(true);
                  return;
                }
                setCollapsed(!collapsed);
              }}
              className={styles.headerIconBtn}
             />
             <span className={styles.headerTitle}>管理后台</span>
           </Space>

          <Space size="small" className={styles.userArea}>
            <Button
              type="text"
              icon={<GithubOutlined />}
              className={styles.headerIconBtn}
              href={PROJECT_GITHUB_URL}
              target="_blank"
              rel="noopener noreferrer"
              title="打开 GitHub 项目地址"
              aria-label="打开 GitHub 项目地址"
            />
            <Dropdown
              menu={{
                selectedKeys: [mode],
                items: themeMenuItems,
                onClick: ({ key }) => setMode(key as ThemeMode),
              }}
              placement="bottomRight"
              arrow
            >
              <Button
                type="text"
                icon={<BgColorsOutlined />}
                className={`${styles.headerIconBtn} ${styles.themeButton}`}
              >
                {!isMobile ? themeModeLabels[mode] : null}
              </Button>
            </Dropdown>
            {userInfo && (
              <Dropdown
                menu={{
                  items: [
                    {
                      key: "tour",
                      icon: <QuestionCircleOutlined />,
                      label: "新手引导",
                      onClick: replayManageTour,
                    },
                    {
                      key: "logout",
                      icon: <LogoutOutlined />,
                      label: "退出登录",
                      onClick: handleLogout,
                    },
                  ],
                }}
                placement="bottomRight"
                arrow
              >
                <div className={styles.userTrigger}>
                  <Space size="small">
                    <Avatar
                      src={userInfo.avatar === "empty" ? null : userInfo.avatar}
                      icon={<UserOutlined />}
                      style={{ backgroundColor: "#1890ff" }}
                    />
                    <span className={styles.userName}>
                      {userInfo.nickName || userInfo.userName}
                    </span>
                    {!isMobile && userInfo.canWrite === false && (
                      <Tag color="blue">访客只读</Tag>
                    )}
                  </Space>
                </div>
              </Dropdown>
            )}
          </Space>
        </Header>
        <Content
          className={`${styles.content} ${isFixedTabbedPage ? styles.contentFixed : ""}`}
          style={{ flex: 1, overflow: isFixedTabbedPage ? "hidden" : "auto" }}
        >
          {notices.length > 0 && (
            <div className={styles.noticeStack}>
              {notices.map((n) => (
                <Alert
                  key={n.code || n.message}
                  type={
                    n.level === "warning"
                      ? "warning"
                      : n.level === "info"
                        ? "info"
                        : "error"
                  }
                  showIcon
                  title={n.message}
                  action={
                    n.actionPath ? (
                      <Link href={n.actionPath}>
                        <Button size="small" type="primary" danger={n.level !== "info" && n.level !== "warning"}>
                          {n.actionText || "查看"}
                        </Button>
                      </Link>
                    ) : undefined
                  }
                  className={styles.noticeAlert}
                />
              ))}
            </div>
          )}
          <ManagePermissionProvider
            canWrite={userInfo?.canWrite !== false}
            isAdmin={Boolean(userInfo?.isAdmin)}
          >
            {children}
          </ManagePermissionProvider>
        </Content>
      </Layout>
      <ManageTour
        isMobile={isMobile}
        canWrite={userInfo?.canWrite !== false}
        permissionReady={userInfo != null}
        onNeedMenu={onNeedMenu}
      />
      <Drawer
        title="后台菜单"
        placement="left"
        size={280}
        open={isMobile && drawerOpen}
        onClose={() => setDrawerOpen(false)}
        className={styles.menuDrawer}
        styles={{ body: { padding: 0 } }}
      >
        <div className={styles.drawerInner}>{menuNode}</div>
      </Drawer>
    </Layout>
  );
}
