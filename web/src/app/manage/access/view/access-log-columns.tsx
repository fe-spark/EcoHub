"use client";

import React from "react";
import Link from "next/link";
import { Space, Tag, Typography } from "antd";
import {
  CompassOutlined,
  PlayCircleOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import type { LogRow } from "./types";
import ResourceCell from "./resource-cell";
import styles from "./index.module.less";

export const ACTION_MAP: Record<string, { label: string; icon: React.ReactNode; color: string }> = {
  play: { label: "影视点播", icon: <PlayCircleOutlined />, color: "var(--ant-color-primary, #fa8c16)" },
  search: { label: "寻片搜索", icon: <SearchOutlined />, color: "#fa541c" },
  browse: { label: "页面浏览", icon: <CompassOutlined />, color: "#52c41a" },
  classify: { label: "分类筛选", icon: <CompassOutlined />, color: "#722ed1" },
};

export const BROWSER_MAP: Record<string, { label: string; color: string }> = {
  chrome: { label: "Chrome", color: "var(--ant-color-info, #1677ff)" },
  safari: { label: "Safari", color: "var(--ant-color-success, #52c41a)" },
  edge: { label: "Edge", color: "var(--ant-color-cyan, #13c2c2)" },
  firefox: { label: "Firefox", color: "var(--ant-color-error, #fa541c)" },
  other: { label: "其他浏览器", color: "var(--ant-color-text-tertiary, #8c8c8c)" },
};

export const OS_MAP: Record<string, { label: string; color: string }> = {
  windows: { label: "Windows", color: "var(--ant-color-info, #1677ff)" },
  macos: { label: "macOS", color: "var(--ant-color-text, #8c8c8c)" },
  android: { label: "Android", color: "var(--ant-color-success, #52c41a)" },
  ios: { label: "iOS", color: "var(--ant-color-text, #000000)" },
  harmonyos: { label: "HarmonyOS", color: "var(--ant-color-info, #1677ff)" },
  linux: { label: "Linux", color: "var(--ant-color-warning, #fa8c16)" },
  other: { label: "其他系统", color: "var(--ant-color-text-tertiary, #8c8c8c)" },
};

export function isSafeInternalHref(v: string): boolean {
  const s = String(v ?? "").trim();
  if (!s || s.startsWith("//")) return false;
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(s)) return false;
  return s.startsWith("/");
}

export function buildAccessLogColumns(): ColumnsType<LogRow> {
  return [
    {
      title: "访问时间",
      dataIndex: "ts",
      key: "ts",
      width: 150,
      render: (ts: number) => (
        <span style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>
          {dayjs(ts).format("YYYY-MM-DD HH:mm:ss")}
        </span>
      ),
    },
    {
      title: "行为动作",
      dataIndex: "action",
      key: "action",
      width: 120,
      render: (act: string) => {
        const item = ACTION_MAP[act] || { label: act || "访问", color: "default", icon: null };
        return (
          <Tag color={item.color} icon={item.icon}>
            {item.label}
          </Tag>
        );
      },
    },
    {
      title: "访问页面 (URL)",
      dataIndex: "path",
      key: "path",
      render: (path: string, record) => {
        const target = record.page || path || "/";
        return (
          <Space direction="vertical" size={2}>
            {isSafeInternalHref(target) ? (
              <Link href={target} target="_blank" className={styles.topPageLink}>
                {target}
              </Link>
            ) : (
              <span className={styles.topPageLink}>{target}</span>
            )}
            {record.pageTitle ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {record.pageTitle}
              </Typography.Text>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: "关联资源",
      dataIndex: "resource",
      key: "resource",
      width: 240,
      render: (_, record) => <ResourceCell record={record} />,
    },
    {
      title: "设备 ID",
      dataIndex: "deviceId",
      key: "deviceId",
      width: 220,
      render: (did?: string) =>
        did ? (
          <Typography.Text
            copyable={{ text: did }}
            code
            style={{ fontSize: 12, whiteSpace: "nowrap" }}
          >
            {did}
          </Typography.Text>
        ) : (
          <span style={{ color: "#bbb" }}>-</span>
        ),
    },
    {
      title: "访客 IP",
      dataIndex: "ipPreview",
      key: "ipPreview",
      width: 140,
      render: (ip: string) => <Typography.Text code>{ip || "local"}</Typography.Text>,
    },
    {
      title: "客户端环境",
      dataIndex: "uaFamily",
      key: "uaFamily",
      width: 130,
      render: (ua: string) => {
        const info = BROWSER_MAP[(ua || "").toLowerCase()];
        return <Tag color={info ? "blue" : undefined}>{info ? info.label : ua || "Web"}</Tag>;
      },
    },
  ];
}
