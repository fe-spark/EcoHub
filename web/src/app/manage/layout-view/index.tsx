"use client";

import React, { useState, useEffect } from "react";
import { useRouter, usePathname } from "next/navigation";
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
} from "antd";
import {
  HomeOutlined,
  ThunderboltOutlined,
  ClockCircleOutlined,
  VideoCameraOutlined,
  FolderOpenOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  LogoutOutlined,
  UserOutlined,
  BgColorsOutlined,
  SunOutlined,
  MoonOutlined,
  DesktopOutlined,
  FileTextOutlined,
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { useThemeMode } from "@/components/theme/GlobalThemeProvider";
import type { ThemeMode } from "@/components/theme/ThemeDock";
import styles from "./index.module.less";

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
    key: "sub-film",
    icon: <VideoCameraOutlined />,
    label: "内容管理",
    children: [
      { key: "/manage/film", label: "影片列表" },
    ],
  },
  {
    key: "sub-collect",
    icon: <ThunderboltOutlined />,
    label: "采集中心",
    children: [
      { key: "/manage/collect", label: "采集站点" },
      { key: "/manage/collect/category", label: "分类管理" },
      { key: "/manage/collect/category/rules", label: "分类规则" },
      { key: "/manage/collect/record", label: "失败记录" },
      { key: "/manage/cron", label: "计划任务" },
    ],
  },
  {
    key: "/manage/file",
    icon: <FolderOpenOutlined />,
    label: "图片素材",
  },
  {
    key: "sub-system",
    icon: <ClockCircleOutlined />,
    label: "系统设置",
    children: [
      { key: "/manage/system/website", label: "网站配置" },
      { key: "/manage/system/banners", label: "首页封面" },
      { key: "/manage/system/users", label: "账号管理" },
    ],
  },
  {
    key: "/manage/system/logs",
    icon: <FileTextOutlined />,
    label: "系统日志",
  },
];

function resolveMenuKey(pathname: string) {
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
  if (pathname.startsWith("/manage/system/website")) {
    return "/manage/system/website";
  }
  if (pathname.startsWith("/manage/system/banners")) {
    return "/manage/system/banners";
  }
  if (pathname.startsWith("/manage/system/users")) {
    return "/manage/system/users";
  }
  if (pathname.startsWith("/manage/system/logs")) {
    return "/manage/system/logs";
  }
  if (pathname.startsWith("/manage/file")) {
    return "/manage/file";
  }
  return "/manage";
}

function collectOpenKeys(items: MenuItem[], selectedKey: string) {
  const openKeys: string[] = [];
  for (const item of items) {
    if (
      !item ||
      typeof item !== "object" ||
      !("children" in item) ||
      !item.children
    ) {
      continue;
    }
    const hasMatch = item.children.some(
      (child) =>
        child &&
        typeof child === "object" &&
        "key" in child &&
        child.key === selectedKey,
    );
    if (hasMatch && "key" in item && typeof item.key === "string") {
      openKeys.push(item.key);
    }
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
  const screens = useBreakpoint();
  const isMobile = !screens.lg;

  const router = useRouter();
  const pathname = usePathname();
  const selectedKey = resolveMenuKey(pathname);
  const isLogPage = pathname.startsWith("/manage/system/logs");

  useEffect(() => {
    ApiGet("/manage/user/info").then((resp) => {
      if (resp.code === 0) {
        setUserInfo(resp.data);
      }
    });
  }, []);

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

  const openKeys = collectOpenKeys(menuItems, selectedKey);
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
      <div className={styles.logoWrap} onClick={() => window.open("/", "_blank")}>
        {siteInfo?.logo && (
          <Avatar
            src={siteInfo.logo}
            size={34}
            shape="square"
            className={styles.logoIcon}
          />
        )}
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
          className={`${styles.content} ${isLogPage ? styles.contentFixed : ""}`}
          style={{ flex: 1, overflow: isLogPage ? "hidden" : "auto" }}
        >
          {children}
        </Content>
      </Layout>
      <Drawer
        title="后台菜单"
        placement="left"
        width={280}
        open={isMobile && drawerOpen}
        onClose={() => setDrawerOpen(false)}
        className={styles.menuDrawer}
        bodyStyle={{ padding: 0 }}
      >
        <div className={styles.drawerInner}>{menuNode}</div>
      </Drawer>
    </Layout>
  );
}
