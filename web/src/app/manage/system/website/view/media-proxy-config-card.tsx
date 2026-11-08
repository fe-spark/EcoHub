"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Card,
  Flex,
  Input,
  Space,
  Spin,
  Switch,
  Typography,
} from "antd";
import {
  CloudSyncOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { isFormValuesEqual } from "@/lib/deep-equal";
import styles from "./media-proxy-config-card.module.less";

export interface MediaProxyConfigPayload {
  enabled: boolean;
  template: string;
}

const DEFAULT_MEDIA_PROXY_CONFIG: MediaProxyConfigPayload = {
  enabled: true,
  template: "",
};

const MAX_TEMPLATE_LEN = 256;

function normalizeMediaProxyConfig(
  raw?: Partial<MediaProxyConfigPayload> | null,
): MediaProxyConfigPayload {
  return {
    enabled: raw?.enabled === undefined ? true : Boolean(raw.enabled),
    template: String(raw?.template ?? "").trim(),
  };
}

interface MediaProxyConfigCardProps {
  canWrite: boolean;
}

export default function MediaProxyConfigCard({ canWrite }: MediaProxyConfigCardProps) {
  const [data, setData] = useState<MediaProxyConfigPayload>(DEFAULT_MEDIA_PROXY_CONFIG);
  const [draft, setDraft] = useState<MediaProxyConfigPayload>(DEFAULT_MEDIA_PROXY_CONFIG);
  const [isTouched, setIsTouched] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const loadData = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/media-proxy");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeMediaProxyConfig(resp.data);
        setData(normalized);
        setDraft(normalized);
        setIsTouched(false);
      }
    } finally {
      setFetching(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const hasDirty = useMemo(() => {
    return isTouched && !isFormValuesEqual(draft, data);
  }, [isTouched, draft, data]);

  const handleCancel = () => {
    setDraft(data);
    setIsTouched(false);
  };

  const handleSave = async () => {
    if (!canWrite) return;
    setSaving(true);
    try {
      const payload: MediaProxyConfigPayload = {
        enabled: draft.enabled,
        template: draft.template.trim(),
      };
      const resp = await ApiPost("/manage/config/media-proxy", payload);
      if (resp.code === 0) {
        message.success("媒体图片反代配置已保存");
        setData(payload);
        setDraft(payload);
        setIsTouched(false);
        refreshSiteConfig();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const currentValues = hasDirty ? draft : data;

  return (
    <Card
      className={styles.card}
      title={
        <Space size={8} align="center">
          <CloudSyncOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>媒体图片反代（边缘加速）</span>
        </Space>
      }
    >
      <Spin spinning={fetching} description="正在加载媒体反代配置...">
        <Flex vertical gap={16}>
          {/* 边缘反代开启开关 */}
          <Flex align="center" justify="space-between">
            <Flex vertical gap={4}>
              <Typography.Text strong>启用边缘反代加速</Typography.Text>
              <Typography.Text type="secondary">
                开启后客户端与前台加载海外影视海报（如采集站、TMDB 等）时自动走公共边缘 CDN 节点反代，国内免代理直开，不消耗自建服务端带宽
              </Typography.Text>
            </Flex>
            <Switch
              disabled={!canWrite}
              checked={currentValues.enabled}
              checkedChildren="开启"
              unCheckedChildren="关闭"
              onChange={(enabled) => {
                setIsTouched(true);
                setDraft((prev) => ({ ...prev, enabled }));
              }}
            />
          </Flex>

          {/* 自定义反代模板 */}
          {currentValues.enabled && (
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>自定义反代模板（可选）</Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.template.length}/{MAX_TEMPLATE_LEN}
                </Typography.Text>
              </Flex>
              <Input
                disabled={!canWrite}
                maxLength={MAX_TEMPLATE_LEN}
                placeholder="留空默认使用 WordPress Photon 节点 (https://i{hash}.wp.com/{url})"
                value={currentValues.template}
                onChange={(e) => {
                  setIsTouched(true);
                  setDraft((prev) => ({
                    ...prev,
                    template: e.target.value,
                  }));
                }}
              />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                支持占位符：{`{url}`} 为去掉协议的纯图片地址，{`{hash}`} 为 0~2 分片负载均衡序号。示例：https://my-worker.workers.dev/{`{url}`}
              </Typography.Text>
            </div>
          )}
        </Flex>
      </Spin>

      <UnsavedChangesBar
        visible={hasDirty}
        saving={saving}
        onDiscard={handleCancel}
        onSave={() => void handleSave()}
      />
    </Card>
  );
}
