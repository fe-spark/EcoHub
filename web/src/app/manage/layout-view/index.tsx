"use client";

import React, { useState, useEffect } from "react";
import { useRouter, usePathname } from "next/navigation";
import { Layout, Menu, Avatar, Button, Space, Dropdown, Tag } from "antd";
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
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import styles from "./index.module.less";

const { Sider, Header, Content } = Layout;

type MenuItem = Required<MenuProps>["items"][number];

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
      { key: "/manage/film/class", label: "分类管理" },
    ],
  },
  {
    key: "sub-collect",
    icon: <ThunderboltOutlined />,
    label: "采集中心",
    children: [
      { key: "/manage/collect", label: "采集站点" },
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
      { key: "/manage/system/mapping-rules", label: "映射规则" },
      { key: "/manage/system/users", label: "账号管理" },
    ],
  },
];

function resolveMenuKey(pathname: string) {
  if (pathname.startsWith("/manage/film/add")) {
    return "/manage/film";
  }
  if (pathname.startsWith("/manage/film/class")) {
    return "/manage/film/class";
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
  if (pathname.startsWith("/manage/system/mapping-rules")) {
    return "/manage/system/mapping-rules";
  }
  if (pathname.startsWith("/manage/system/users")) {
    return "/manage/system/users";
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
  const { config: siteInfo } = useSiteConfig();
  const [userInfo, setUserInfo] = useState<any>(null);

  const router = useRouter();
  const pathname = usePathname();
  const selectedKey = resolveMenuKey(pathname);

  useEffect(() => {
    ApiGet("/manage/user/info").then((resp) => {
      if (resp.code === 0) {
        setUserInfo(resp.data);
      }
    });
  }, []);

  const onMenuClick: MenuProps["onClick"] = ({ key }) => {
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

  return (
    <Layout className={styles.layout} hasSider>
      <Sider
        trigger={null}
        collapsible
        collapsed={collapsed}
        className={styles.sider}
        theme="light"
      >
        <div
          style={{ display: "flex", flexDirection: "column", height: "100vh" }}
        >
          <div
            className={styles.logoWrap}
            onClick={() => window.open("/", "_blank")}
          >
            {siteInfo?.logo && <Avatar src={siteInfo.logo} size={30} />}
            {!collapsed && siteInfo?.siteName && (
              <span className={styles.siteName}>{siteInfo.siteName}</span>
            )}
          </div>
          <Menu
            mode="inline"
            style={{ flex: 1, overflow: "auto" }}
            selectedKeys={[selectedKey]}
            defaultOpenKeys={openKeys}
            items={menuItems}
            onClick={onMenuClick}
          />
        </div>
      </Sider>
      <Layout
        style={{ display: "flex", flexDirection: "column", height: "100vh" }}
      >
        <Header className={styles.header}>
          <Space size="middle">
            <Button
              type="text"
              icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => setCollapsed(!collapsed)}
              className={styles.headerIconBtn}
            />
            <span className={styles.headerTitle}>管理后台</span>
          </Space>

          <Space size="middle">
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
                <div style={{ cursor: "pointer" }}>
                  <Space size="small">
                    <Avatar
                      src={userInfo.avatar === "empty" ? null : userInfo.avatar}
                      icon={<UserOutlined />}
                      style={{ backgroundColor: "#1890ff" }}
                    />
                    <span style={{ fontWeight: 500 }}>
                      {userInfo.nickName || userInfo.userName}
                    </span>
                    {userInfo.canWrite === false && (
                      <Tag color="blue">访客只读</Tag>
                    )}
                  </Space>
                </div>
              </Dropdown>
            )}
          </Space>
        </Header>
        <Content
          className={styles.content}
          style={{ flex: 1, overflow: "auto" }}
        >
          {children}
        </Content>
      </Layout>
    </Layout>
  );
}
