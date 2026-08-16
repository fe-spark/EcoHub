"use client";

import React, { useState, useEffect } from "react";
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
  HomeOutlined,
  ThunderboltOutlined,
  VideoCameraOutlined,
  FolderOpenOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  LogoutOutlined,
  UserOutlined,
  TeamOutlined,
  SettingOutlined,
  BgColorsOutlined,
  SunOutlined,
  MoonOutlined,
  DesktopOutlined,
  GithubOutlined,
} from "@ant-design/icons";
import { PROJECT_GITHUB_URL } from "@/lib/project";

import type { MenuProps } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { useThemeMode } from "@/components/theme/GlobalThemeProvider";
import type { ThemeMode } from "@/components/theme/ThemeDock";
import { resolveSiteLogoSrc } from "@/components/public/SiteLogo";
import { ManagePermissionProvider } from "@/lib/manage-permission";
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

type MenuItem = Required<MenuProps>["items"][number];

const themeModeLabels: Record<ThemeMode, string> = {
  light: "浅色",
  dark: "深色",
  system: "跟随系统",
};

const menuItems: MenuItem[] = [
  {
    key: "/manage",
    icon: <HomeOutlined />,
    label: "工作台",
  },
  {
    key: "sub-collect",
    icon: <ThunderboltOutlined />,
    label: "采集管理",
    children: [
      { key: "/manage/collect", label: "采集中心" },
      { key: "/manage/collect/record", label: "失败记录" },
      { key: "/manage/cron", label: "计划任务" },
    ],
  },
  {
    key: "sub-film",
    icon: <VideoCameraOutlined />,
    label: "内容管理",
    children: [
      { key: "/manage/film", label: "影片列表" },
      { key: "/manage/banners", label: "首页轮播" },
      { key: "/manage/collect/category", label: "分类管理" },
      { key: "/manage/collect/category/rules", label: "分类规则" },
    ],
  },
  {
    key: "/manage/file",
    icon: <FolderOpenOutlined />,
    label: "素材中心",
  },
  {
    key: "/manage/system/users",
    icon: <TeamOutlined />,
    label: "账号管理",
  },
  {
    key: "/manage/system",
    icon: <SettingOutlined />,
    label: "系统设置",
  },
];

function resolveMenuKey(pathname: string) {
  // 旧数据重置入口并入系统设置 · 数据安全
  if (pathname.startsWith("/manage/reset")) {
    return "/manage/system";
  }
  if (pathname.startsWith("/manage/banners")) {
    return "/manage/banners";
  }
  if (pathname.startsWith("/manage/film/add")) {
    return "/manage/film";
  }
  if (pathname.startsWith("/manage/collect/category/rules")) {
    return "/manage/collect/category/rules";
  }
  if (pathname.startsWith("/manage/collect/category")) {
    return "/manage/collect/category";
  }
  if (pathname.startsWith("/manage/film")) {
    return "/manage/film";
  }
  if (pathname.startsWith("/manage/collect/record")) {
    return "/manage/collect/record";
  }
  if (pathname.startsWith("/manage/collect")) {
    return "/manage/collect";
  }
  if (pathname.startsWith("/manage/cron")) {
    return "/manage/cron";
  }
  if (pathname.startsWith("/manage/system/users")) {
    return "/manage/system/users";
  }
  if (pathname.startsWith("/manage/system")) {
    return "/manage/system";
  }
  if (pathname.startsWith("/manage/file")) {
    return "/manage/file";
  }
  return "/manage";
}


function collectAllOpenKeys(items: MenuItem[]) {
  const openKeys: string[] = [];
  for (const item of items) {
    if (
      !item ||
      typeof item !== "object" ||
      !("children" in item) ||
      !item.children ||
      !("key" in item) ||
      typeof item.key !== "string"
    ) {
      continue;
    }
    openKeys.push(item.key);
  }
  return openKeys;
}

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
  const isMobile = !screens.lg;

  const router = useRouter();
  const pathname = usePathname();
  const isSystemPage = pathname.startsWith("/manage/system");
  const selectedKey = resolveMenuKey(pathname);






  useEffect(() => {
    ApiGet("/manage/user/info").then((resp) => {
      if (resp.code === 0) {
        setUserInfo(resp.data);
      }
    });
  }, []);

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

  const openKeys = collectAllOpenKeys(menuItems);
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
        {(!collapsed || isMobile) && siteInfo?.siteName && (
          <span className={styles.siteName}>{siteInfo.siteName}</span>
        )}
      </div>
      <Menu
        mode="inline"
        className={styles.menu}
        style={{ borderInlineEnd: 0 }}
        selectedKeys={[selectedKey]}
        defaultOpenKeys={openKeys}
        items={menuItems}
        onClick={onMenuClick}
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
          className={`${styles.content} ${isSystemPage ? styles.contentFixed : ""}`}
          style={{ flex: 1, overflow: isSystemPage ? "hidden" : "auto" }}
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
          <ManagePermissionProvider canWrite={userInfo?.canWrite !== false}>
            {children}
          </ManagePermissionProvider>
        </Content>
      </Layout>
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
