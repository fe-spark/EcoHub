"use client";

import React, { forwardRef, useImperativeHandle } from "react";
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
import type { ConfigCardHandle } from "./types";
import { ConfigLoadErrorAlert, useSiteConfigCard } from "./use-config-card";
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
  onDirtyChange?: (dirty: boolean) => void;
}

const MediaProxyConfigCard = forwardRef<ConfigCardHandle, MediaProxyConfigCardProps>(
  function MediaProxyConfigCard({ canWrite, onDirtyChange }, ref) {
    const {
      draft,
      setDraft,
      setIsTouched,
      fetching,
      loadError,
      reload,
      hasDirty,
      save,
      discard,
      editable,
    } = useSiteConfigCard<MediaProxyConfigPayload>({
      loadUrl: "/manage/config/media-proxy",
      saveUrl: "/manage/config/media-proxy",
      defaultValue: DEFAULT_MEDIA_PROXY_CONFIG,
      normalize: normalizeMediaProxyConfig,
      loadErrorText: "加载媒体图片反代配置失败",
      saveErrorText: "保存媒体图片反代配置失败",
      canWrite,
      onDirtyChange,
    });

    useImperativeHandle(
      ref,
      () => ({
        isDirty: () => hasDirty,
        validate: () => true,
        save: () => save(),
        discard,
      }),
      [hasDirty, save, discard],
    );

    const currentValues = draft;

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
          <ConfigLoadErrorAlert
            visible={loadError}
            text="加载媒体图片反代配置失败"
            loading={fetching}
            onRetry={() => void reload()}
          />
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
                disabled={!editable}
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
                  disabled={!editable}
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
      </Card>
    );
  },
);

export default MediaProxyConfigCard;
