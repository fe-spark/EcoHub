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
} from "@ant-design/icons";
import type { MenuProps } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import styles from "./index.module.less";

const { Sider, Header, Content } = Layout;

type MenuItem = Required<MenuProps>["items"][number];

const menuItems: MenuItem[] = [
  {
    key: "sub-system",
    icon: <HomeOutlined />,
    label: "网站管理",
    children: [
      { key: "/manage/system/website", label: "站点管理" },
      { key: "/manage/system/banners", label: "海报管理" },
      { key: "/manage/system/users", label: "用户管理" },
    ],
  },
  {
    key: "sub-collect",
    icon: <ThunderboltOutlined />,
    label: "采集管理",
    children: [
      { key: "/manage/collect", label: "影视采集" },
      { key: "/manage/collect/record", label: "失效记录" },
    ],
  },
  {
    key: "/manage/cron",
    icon: <ClockCircleOutlined />,
    label: "定时任务",
  },
  {
    key: "sub-film",
    icon: <VideoCameraOutlined />,
    label: "影片管理",
    children: [
      { key: "/manage/film/class", label: "影视分类" },
      { key: "/manage/film", label: "影视信息" },
    ],
  },
  {
    key: "/manage/file",
    icon: <FolderOpenOutlined />,
    label: "图库管理",
  },
];

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

  const openKeys = menuItems
    .filter((item: any) =>
      item.children?.some((child: any) => pathname.startsWith(child.key)),
    )
    .map((item: any) => item.key);

  return (
    <Layout className={styles.layout} hasSider>
      <Sider
        trigger={null}
        collapsible
        collapsed={collapsed}
        className={styles.sider}
        theme="light"
      >
        <div style={{ display: "flex", flexDirection: "column", height: "100vh" }}>
          <div className={styles.logoWrap} onClick={() => window.open("/", "_blank")}>
            {siteInfo?.logo && <Avatar src={siteInfo.logo} size={30} />}
            {!collapsed && siteInfo?.siteName && (
              <span className={styles.siteName}>{siteInfo.siteName}</span>
            )}
          </div>
          <Menu
            mode="inline"
            style={{ flex: 1, overflow: "auto" }}
            selectedKeys={[pathname]}
            defaultOpenKeys={openKeys}
            items={menuItems}
            onClick={onMenuClick}
          />
        </div>
      </Sider>
      <Layout style={{ display: "flex", flexDirection: "column", height: "100vh" }}>
        <Header className={styles.header}>
          <Space size="middle">
            <Button
              type="text"
              icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => setCollapsed(!collapsed)}
              className={styles.headerIconBtn}
            />
            <span className={styles.headerTitle}>后台管理中心</span>
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
                    <span style={{ fontWeight: 500 }}>{userInfo.nickName || userInfo.userName}</span>
                    {userInfo.canWrite === false && <Tag color="blue">访客只读</Tag>}
                  </Space>
                </div>
              </Dropdown>
            )}
          </Space>
        </Header>
        <Content className={styles.content} style={{ flex: 1, overflow: "auto" }}>
          {children}
        </Content>
      </Layout>
    </Layout>
  );
}
