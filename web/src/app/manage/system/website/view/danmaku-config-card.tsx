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
  CommentOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { isFormValuesEqual } from "@/lib/deep-equal";
import styles from "./danmaku-config-card.module.less";

export interface DanmakuConfigPayload {
  enabled: boolean;
  autoSniff: boolean;
  apiUrl: string;
  appId?: string;
  appSecret?: string;
}

const DEFAULT_DANMAKU_CONFIG: DanmakuConfigPayload = {
  enabled: false,
  autoSniff: false,
  apiUrl: "",
  appId: "",
  appSecret: "",
};

function normalizeDanmakuConfig(
  raw?: Partial<DanmakuConfigPayload> | null,
): DanmakuConfigPayload {
  return {
    enabled: Boolean(raw?.enabled),
    autoSniff: Boolean(raw?.autoSniff),
    apiUrl: String(raw?.apiUrl ?? "").trim(),
    appId: String(raw?.appId ?? "").trim(),
    appSecret: String(raw?.appSecret ?? "").trim(),
  };
}

interface DanmakuConfigCardProps {
  canWrite: boolean;
}

export default function DanmakuConfigCard({ canWrite }: DanmakuConfigCardProps) {
  const [data, setData] = useState<DanmakuConfigPayload>(DEFAULT_DANMAKU_CONFIG);
  const [draft, setDraft] = useState<DanmakuConfigPayload>(DEFAULT_DANMAKU_CONFIG);
  const [isTouched, setIsTouched] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const loadData = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/danmaku");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeDanmakuConfig(resp.data);
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
    if (draft.enabled && draft.autoSniff && !draft.apiUrl.trim()) {
      if (!draft.appId?.trim()) {
        message.error("使用弹弹play官方接口嗅探时，必须填写 AppId");
        return;
      }
      if (!draft.appSecret?.trim() && !data.appSecret?.trim()) {
        message.error("使用弹弹play官方接口嗅探时，必须填写 AppSecret");
        return;
      }
    }
    setSaving(true);
    try {
      const normalized = normalizeDanmakuConfig(draft);
      const resp = await ApiPost("/manage/config/danmaku", normalized);
      if (resp.code === 0) {
        message.success(resp.msg || "弹幕系统配置已保存");
        setData(normalized);
        setDraft(normalized);
        setIsTouched(false);
        await refreshSiteConfig();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const currentValues = draft;

  return (
    <Card
      className={styles.card}
      title={
        <Space size={8} align="center">
          <CommentOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>弹幕系统配置</span>
        </Space>
      }
    >
      <Spin spinning={fetching} description="正在加载弹幕配置...">
        <Flex vertical gap={16}>
          {/* 启用弹幕系统 */}
          <Flex align="center" justify="space-between">
            <Flex vertical gap={4}>
              <Typography.Text strong>启用弹幕系统</Typography.Text>
              <Typography.Text type="secondary">
                开启后，播放器将挂载弹幕插件并展示弹幕图层
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

          {currentValues.enabled && (
            <>
              {/* 自动嗅探全网弹幕 */}
              <Flex align="center" justify="space-between">
                <Flex vertical gap={4}>
                  <Typography.Text strong>自动嗅探全网弹幕</Typography.Text>
                  <Typography.Text type="secondary">
                    {currentValues.autoSniff
                      ? "开启后，播放视频时自动根据片名和集数联网嗅探全网弹幕（弹弹play协议）"
                      : "关闭时仅保留本站自建弹幕库（自发自收，本地存储），无需任何外部接口与凭据"}
                  </Typography.Text>
                </Flex>
                <Switch
                  disabled={!canWrite}
                  checked={currentValues.autoSniff}
                  checkedChildren="开启"
                  unCheckedChildren="关闭"
                  onChange={(autoSniff) => {
                    setIsTouched(true);
                    setDraft((prev) => ({ ...prev, autoSniff }));
                  }}
                />
              </Flex>

              {/* 仅在开启自动嗅探时才级联展示外部接口配置 */}
              {currentValues.autoSniff && (
                <>
                  {/* 自定义弹幕 API 地址 */}
                  <div className={styles.field}>
                    <Typography.Text strong>自定义弹幕 API 地址 (可选)</Typography.Text>
                    <Input
                      disabled={!canWrite}
                      placeholder="留空默认走官方接口；自建节点可填如 http://192.168.1.100:9321/api/v2"
                      value={currentValues.apiUrl || ""}
                      onChange={(e) => {
                        setIsTouched(true);
                        setDraft((prev) => ({ ...prev, apiUrl: e.target.value }));
                      }}
                      style={{ marginTop: 6 }}
                    />
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      支持兼容弹弹play协议的自建 danmu-api 容器（免密直连）；若配置了自定义节点，下方官方凭据自动变为选填
                    </Typography.Text>
                  </div>

                  {/* 弹弹play AppId */}
                  <div className={styles.field}>
                    <Typography.Text strong>
                      弹弹play AppId {currentValues.apiUrl?.trim() ? "(已配自建节点，选填)" : "(官方接口必填)"}
                    </Typography.Text>
                    <Input
                      disabled={!canWrite}
                      placeholder="填写开放平台分配的 AppId（例如 jedqb3k6s7）"
                      value={currentValues.appId || ""}
                      onChange={(e) => {
                        setIsTouched(true);
                        setDraft((prev) => ({ ...prev, appId: e.target.value }));
                      }}
                      style={{ marginTop: 6 }}
                    />
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      从 dev.dandanplay.com 应用详情页获取
                    </Typography.Text>
                  </div>

                  {/* 弹弹play AppSecret */}
                  <div className={styles.field}>
                    <Typography.Text strong>
                      弹弹play AppSecret {currentValues.apiUrl?.trim() ? "(已配自建节点，选填)" : "(官方接口必填)"}
                    </Typography.Text>
                    <Input.Password
                      disabled={!canWrite}
                      placeholder="填写 dev.dandanplay.com 分配的 AppSecret 密钥"
                      value={currentValues.appSecret || ""}
                      onChange={(e) => {
                        setIsTouched(true);
                        setDraft((prev) => ({ ...prev, appSecret: e.target.value }));
                      }}
                      style={{ marginTop: 6 }}
                    />
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      在开放平台应用详情页中复制 AppSecret 1 或 AppSecret 2 即可（保存后不明文回显）
                    </Typography.Text>
                  </div>
                </>
              )}
            </>
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
