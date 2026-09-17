"use client";

import React, { useState, useEffect, useMemo } from "react";
import {
  Button,
  Input,
  Modal,
  Space,
  Tag,
  Typography,
} from "antd";
import {
  CopyOutlined,
  LinkOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { useSiteConfig } from "@/components/common/SiteGuard";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./index.module.less";

interface SubscribeItem {
  key: string;
  title: string;
  tag: string;
  path: string;
  withKey?: boolean;
}

export interface SubscribeModalProps {
  open: boolean;
  onClose: () => void;
}

export default function SubscribeModal(props: SubscribeModalProps) {
  const { open, onClose } = props;
  const { config: publicConfig } = useSiteConfig();
  const { message } = useAppMessage();
  const [adminConfig, setAdminConfig] = useState<{
    siteUrl?: string;
    provideKey?: string;
    privateAccess?: boolean;
  } | null>(null);

  // 后台工作台弹窗拉取具备管理员鉴权的 /manage/config/basic，以获取完整 provideKey
  useEffect(() => {
    if (!open) return;
    let active = true;
    ApiGet("/manage/config/basic")
      .then((resp) => {
        if (active && resp.code === 0 && resp.data) {
          setAdminConfig(resp.data);
        }
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [open]);

  const activeSiteUrl = adminConfig?.siteUrl || publicConfig?.siteUrl;
  const currentKey = (adminConfig?.provideKey || publicConfig?.provideKey)?.trim();
  const isPrivate = Boolean(
    adminConfig ? adminConfig.privateAccess : publicConfig?.privateAccess,
  );

  // 基础 Origin：优先使用配置的 siteUrl，兜底使用当前浏览器 location.origin
  const origin = useMemo(() => {
    if (activeSiteUrl && activeSiteUrl.trim()) {
      return activeSiteUrl.trim().replace(/\/+$/, "");
    }
    if (typeof window !== "undefined") {
      return window.location.origin;
    }
    return "";
  }, [activeSiteUrl]);

  const getUrl = (item: SubscribeItem) => {
    if (!origin) return item.path;
    if (item.withKey && isPrivate && currentKey) {
      const delimiter = item.path.includes("?") ? "&" : "?";
      return `${origin}${item.path}${delimiter}key=${encodeURIComponent(currentKey)}`;
    }
    return `${origin}${item.path}`;
  };

  const handleCopy = async (text: string, label: string) => {
    if (!text) return;
    try {
      if (navigator?.clipboard?.writeText) {
        await navigator.clipboard.writeText(text);
        message.success(`${label}已复制`);
      } else {
        const textarea = document.createElement("textarea");
        textarea.value = text;
        textarea.style.position = "fixed";
        textarea.style.opacity = "0";
        document.body.appendChild(textarea);
        textarea.select();
        document.execCommand("copy");
        document.body.removeChild(textarea);
        message.success(`${label}已复制`);
      }
    } catch {
      message.error("复制失败，请手动选中文本复制");
    }
  };

  const items: SubscribeItem[] = [
    {
      key: "tvbox",
      title: "TVBox / 影视仓配置",
      tag: "推荐",
      path: "/api/provide/tvbox",
      withKey: true,
    },
    {
      key: "vod",
      title: "MacCMS V10 接口",
      tag: "API",
      path: "/api/provide/vod",
      withKey: true,
    },
    {
      key: "client",
      title: "EcoHub 客户端软件源",
      tag: "App",
      path: "/api/provide/app",
      withKey: true,
    },
  ];

  return (
    <Modal
      open={open}
      onCancel={onClose}
      width={560}
      footer={null}
      title={
        <Space size={8}>
          <LinkOutlined style={{ color: "var(--ant-color-primary)" }} />
          <Typography.Text strong style={{ fontSize: 15 }}>
            订阅地址
          </Typography.Text>
        </Space>
      }
    >
      <div className={styles.modalContent}>
        {items.map((item) => {
          const fullUrl = getUrl(item);

          return (
            <div key={item.key} className={styles.itemRow}>
              <div className={styles.itemLabel}>
                <span>{item.title}</span>
                <Tag bordered={false} style={{ margin: 0 }}>
                  {item.tag}
                </Tag>
              </div>

              <Space.Compact style={{ width: "100%" }}>
                <Input
                  readOnly
                  value={fullUrl}
                  className={styles.urlInput}
                />
                <Button
                  type="primary"
                  icon={<CopyOutlined />}
                  onClick={() => handleCopy(fullUrl, item.title)}
                >
                  复制
                </Button>
              </Space.Compact>
            </div>
          );
        })}
      </div>
    </Modal>
  );
}
