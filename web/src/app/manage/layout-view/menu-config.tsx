import React from "react";
import type { MenuProps } from "antd";
import {
  HomeOutlined,
  ThunderboltOutlined,
  VideoCameraOutlined,
  FolderOpenOutlined,
  SettingOutlined,
  LineChartOutlined,
  ApiOutlined,
  CloudServerOutlined,
} from "@ant-design/icons";
import type { ThemeMode } from "@/components/theme/ThemeDock";

export type MenuItem = Required<MenuProps>["items"][number];

export const themeModeLabels: Record<ThemeMode, string> = {
  light: "浅色",
  dark: "深色",
  system: "跟随系统",
};

export const menuItems: MenuItem[] = [
  { key: "/manage", icon: <HomeOutlined />, label: "工作台" },
  {
    key: "sub-collect",
    icon: <ThunderboltOutlined />,
    label: "采集管理",
    children: [
      { key: "/manage/collect", label: <span data-tour="menu-collect">采集中心</span> },
      { key: "/manage/collect/record", label: <span data-tour="menu-record">失败记录</span> },
      { key: "/manage/cron", label: <span data-tour="menu-cron">计划任务</span> },
    ],
  },
  {
    key: "sub-film",
    icon: <VideoCameraOutlined />,
    label: "内容管理",
    children: [
      { key: "/manage/film", label: <span data-tour="menu-film">影片列表</span> },
      { key: "/manage/banners", label: "首页轮播" },
      { key: "/manage/collect/category", label: <span data-tour="menu-category">分类管理</span> },
      { key: "/manage/collect/category/rules", label: <span data-tour="menu-rules">分类规则</span> },
    ],
  },
  { key: "/manage/file", icon: <FolderOpenOutlined />, label: "素材中心" },
  { key: "/manage/access", icon: <LineChartOutlined />, label: "数据分析" },
  { key: "/manage/system/proxy", icon: <ApiOutlined />, label: "网络代理" },
  {
    key: "sub-integrations",
    icon: <CloudServerOutlined />,
    label: "服务集成",
    children: [
      { key: "/manage/system/tmdb", label: "TMDB 刮削" },
      { key: "/manage/system/notify", label: "通知推送" },
    ],
  },
  {
    key: "sub-system",
    icon: <SettingOutlined />,
    label: "系统运维",
    children: [
      { key: "/manage/system/website", label: "网站配置" },
      { key: "/manage/system/users", label: "账号管理" },
      { key: "/manage/system/security", label: "数据安全" },
      { key: "/manage/system/logs", label: "运行日志" },
    ],
  },
];

export function resolveMenuKey(pathname: string): string {
  if (pathname.startsWith("/manage/banners")) return "/manage/banners";
  if (pathname.startsWith("/manage/collect/category/rules")) return "/manage/collect/category/rules";
  if (pathname.startsWith("/manage/collect/category")) return "/manage/collect/category";
  if (pathname.startsWith("/manage/film")) return "/manage/film";
  if (pathname.startsWith("/manage/collect/record")) return "/manage/collect/record";
  if (pathname.startsWith("/manage/collect")) return "/manage/collect";
  if (pathname.startsWith("/manage/cron")) return "/manage/cron";
  if (pathname.startsWith("/manage/system/proxy")) return "/manage/system/proxy";
  if (pathname.startsWith("/manage/system/tmdb")) return "/manage/system/tmdb";
  if (pathname.startsWith("/manage/system/notify")) return "/manage/system/notify";
  if (pathname.startsWith("/manage/system/users")) return "/manage/system/users";
  if (pathname.startsWith("/manage/system/website")) return "/manage/system/website";
  if (pathname.startsWith("/manage/system/security")) return "/manage/system/security";
  if (pathname.startsWith("/manage/system/logs")) return "/manage/system/logs";
  if (pathname.startsWith("/manage/access")) return "/manage/access";
  if (pathname.startsWith("/manage/file")) return "/manage/file";
  return "/manage";
}

export function resolveParentMenuKeys(items: MenuItem[], targetKey: string): string[] {
  for (const item of items) {
    if (
      item &&
      typeof item === "object" &&
      "children" in item &&
      Array.isArray(item.children) &&
      "key" in item &&
      typeof item.key === "string"
    ) {
      if (item.children.some((child) => child && typeof child === "object" && "key" in child && child.key === targetKey)) {
        return [item.key];
      }
    }
  }
  return [];
}
