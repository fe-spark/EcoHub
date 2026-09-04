import type { ReactNode } from "react";
import { Space, Tag, Typography } from "antd";
import {
  AndroidOutlined,
  AppleOutlined,
  CompassOutlined,
  MobileOutlined,
  PlayCircleOutlined,
  SearchOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import type { LogRow } from "./types";
import ResourceCell from "./resource-cell";

export const PLATFORM_MAP: Record<string, { label: string; icon: ReactNode; color: string }> = {
  android: { label: "Android 安卓", icon: <AndroidOutlined />, color: "var(--ant-color-success, #52c41a)" },
  harmony: { label: "HarmonyOS 鸿蒙", icon: <MobileOutlined />, color: "var(--ant-color-info, #1677ff)" },
  ios: { label: "iOS", icon: <AppleOutlined />, color: "var(--ant-color-text, #000000)" },
};

export const ACTION_MAP: Record<string, { label: string; icon: ReactNode; color: string }> = {
  play: { label: "影视点播", icon: <PlayCircleOutlined />, color: "var(--ant-color-primary, #fa8c16)" },
  search: { label: "寻片搜索", icon: <SearchOutlined />, color: "var(--ant-color-error, #fa541c)" },
  browse: { label: "页面浏览", icon: <CompassOutlined />, color: "var(--ant-color-success, #52c41a)" },
  classify: { label: "分类筛选", icon: <CompassOutlined />, color: "var(--ant-color-purple, #722ed1)" },
};

export function buildAppLogColumns(): ColumnsType<LogRow> {
  return [
    {
      title: "访问时间",
      dataIndex: "ts",
      key: "ts",
      width: 170,
      render: (ts: string) => dayjs(ts).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "客户端平台",
      dataIndex: "clientType",
      key: "clientType",
      width: 140,
      render: (client: string) => {
        const info = PLATFORM_MAP[client] || { label: client || "App", icon: <MobileOutlined />, color: "var(--ant-color-success, #52c41a)" };
        return (
          <Tag color={info.color} icon={info.icon}>
            {info.label}
          </Tag>
        );
      },
    },
    {
      title: "原生页面标识 (Screen)",
      dataIndex: "page",
      key: "page",
      render: (page: string, record) => {
        const target = record.page || record.path || "HomePage";
        return (
          <Space orientation="vertical" size={2}>
            <Typography.Text strong>{target}</Typography.Text>
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
      title: "用户动作",
      dataIndex: "action",
      key: "action",
      width: 120,
      render: (action: string) => {
        const item = ACTION_MAP[action] || { label: action || "浏览", icon: <CompassOutlined />, color: "var(--ant-color-success, #52c41a)" };
        return (
          <Tag color={item.color} icon={item.icon}>
            {item.label}
          </Tag>
        );
      },
    },
    {
      title: "版本与设备机型",
      key: "device",
      width: 200,
      render: (_, record) => (
        <Space size={4} wrap>
          {record.appVersion ? <Tag color="blue">{record.appVersion}</Tag> : null}
          {record.deviceModel ? <Tag>{record.deviceModel}</Tag> : <span style={{ color: "var(--ant-color-text-quaternary, #bbb)" }}>-</span>}
        </Space>
      ),
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
          <Typography.Text copyable={{ text: did }} code style={{ fontSize: 12, whiteSpace: "nowrap" }}>
            {did}
          </Typography.Text>
        ) : (
          <span style={{ color: "var(--ant-color-text-quaternary, #bbb)" }}>-</span>
        ),
    },
    {
      title: "访客 IP",
      dataIndex: "ipPreview",
      key: "ipPreview",
      width: 130,
      render: (ip: string) => <Typography.Text code>{ip || "local"}</Typography.Text>,
    },
  ];
}
