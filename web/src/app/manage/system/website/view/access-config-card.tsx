"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Card,
  Flex,
  Input,
  Space,
  Spin,
  Switch,
  Typography,
} from "antd";
import {
  EditOutlined,
  ReloadOutlined,
  SafetyCertificateOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import styles from "./access-config-card.module.less";

function generateRandomKey(length: number = 16): string {
  const chars = "abcdefghijklmnopqrstuvwxyz0123456789";
  let res = "";
  if (typeof crypto !== "undefined" && crypto.getRandomValues) {
    const bytes = new Uint8Array(length);
    crypto.getRandomValues(bytes);
    for (let i = 0; i < length; i++) {
      res += chars[bytes[i] % chars.length];
    }
  } else {
    for (let i = 0; i < length; i++) {
      res += chars[Math.floor(Math.random() * chars.length)];
    }
  }
  return res;
}

export interface AccessConfigPayload {
  state: boolean;
  hint: string;
  privateAccess: boolean;
  provideKey: string;
}

const DEFAULT_ACCESS_CONFIG: AccessConfigPayload = {
  state: true,
  hint: "网站升级中, 暂时无法访问 !!!",
  privateAccess: false,
  provideKey: "",
};

const MAX_HINT_LEN = 256;
const MAX_PROVIDE_KEY_LEN = 64;

function normalizeAccessConfig(
  raw?: Partial<AccessConfigPayload> | null,
): AccessConfigPayload {
  return {
    state: raw?.state === undefined ? true : Boolean(raw.state),
    hint: String(raw?.hint ?? "").trim() || DEFAULT_ACCESS_CONFIG.hint,
    privateAccess: Boolean(raw?.privateAccess),
    provideKey: String(raw?.provideKey ?? "").trim(),
  };
}

interface AccessConfigCardProps {
  canWrite: boolean;
}

export default function AccessConfigCard({ canWrite }: AccessConfigCardProps) {
  const [data, setData] = useState<AccessConfigPayload>(DEFAULT_ACCESS_CONFIG);
  const [draft, setDraft] = useState<AccessConfigPayload>(DEFAULT_ACCESS_CONFIG);
  const [isEditing, setIsEditing] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const loadData = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/access");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeAccessConfig(resp.data);
        setData(normalized);
        setDraft(normalized);
      }
    } finally {
      setFetching(false);
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  const hasDirty = useMemo(() => {
    return JSON.stringify(draft) !== JSON.stringify(data);
  }, [draft, data]);

  const handleCancel = () => {
    setDraft(data);
    setIsEditing(false);
  };

  const handleSave = async () => {
    if (!canWrite) return;
    if (draft.privateAccess && !draft.provideKey.trim()) {
      message.error("开启私有化访问时，订阅密钥不能为空");
      return;
    }
    setSaving(true);
    try {
      const normalized = normalizeAccessConfig(draft);
      const resp = await ApiPost("/manage/config/access/update", normalized);
      if (resp.code === 0) {
        message.success(resp.msg || "访问控制配置已保存");
        setData(normalized);
        setDraft(normalized);
        setIsEditing(false);
        await refreshSiteConfig();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } finally {
      setSaving(false);
    }
  };

  const currentValues = isEditing ? draft : data;

  return (
    <Card
      className={styles.card}
      title={
        <Space size={8} align="center">
          <SafetyCertificateOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>运行与访问控制</span>
        </Space>
      }
      extra={
        <Space size={8} align="center">
          {isEditing ? (
            <>
              <Button size="small" disabled={saving} onClick={handleCancel}>
                取消
              </Button>
              <Button
                size="small"
                type="primary"
                icon={<SaveOutlined />}
                disabled={!canWrite || !hasDirty}
                loading={saving}
                onClick={handleSave}
              >
                保存配置
              </Button>
            </>
          ) : (
            <Button
              size="small"
              type="primary"
              icon={<EditOutlined />}
              disabled={!canWrite}
              onClick={() => {
                setDraft(data);
                setIsEditing(true);
              }}
            >
              编辑
            </Button>
          )}
        </Space>
      }
    >
      <Spin spinning={fetching} description="正在加载访问控制配置...">
        <Flex vertical gap={16}>
          {/* 网站运行状态 */}
          <Flex align="center" justify="space-between">
            <Flex vertical gap={4}>
              <Typography.Text strong>网站运行状态</Typography.Text>
              <Typography.Text type="secondary">
                开启后网站正常对外开放；关闭后前台将显示维护提示页面
              </Typography.Text>
            </Flex>
            <Switch
              disabled={!isEditing || !canWrite}
              checked={currentValues.state}
              checkedChildren="开启"
              unCheckedChildren="关闭"
              onChange={(state) => setDraft((prev) => ({ ...prev, state }))}
            />
          </Flex>

          {/* 系统维护提示 */}
          <div className={styles.field}>
            <Flex justify="space-between" align="baseline">
              <Typography.Text strong>系统维护提示</Typography.Text>
              <Typography.Text type="secondary">
                {currentValues.hint.length}/{MAX_HINT_LEN}
              </Typography.Text>
            </Flex>
            <Input.TextArea
              disabled={!isEditing || !canWrite}
              maxLength={MAX_HINT_LEN}
              rows={2}
              placeholder="网站维护中，请稍后再试..."
              value={currentValues.hint}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, hint: e.target.value }))
              }
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              网站处于关闭维护状态时，向前台访问用户呈现的友好提示语
            </Typography.Text>
          </div>

          {/* 私有化访问控制 */}
          <Flex align="center" justify="space-between">
            <Flex vertical gap={4}>
              <Typography.Text strong>私有化访问</Typography.Text>
              <Typography.Text type="secondary">
                开启后，未登录访客将强制重定向至登录页
              </Typography.Text>
            </Flex>
            <Switch
              disabled={!isEditing || !canWrite}
              checked={currentValues.privateAccess}
              checkedChildren="开启"
              unCheckedChildren="关闭"
              onChange={(privateAccess) =>
                setDraft((prev) => {
                  let provideKey = prev.provideKey;
                  if (privateAccess && !provideKey.trim()) {
                    provideKey = generateRandomKey(16);
                  }
                  return { ...prev, privateAccess, provideKey };
                })
              }
            />
          </Flex>

          {/* 订阅密钥 */}
          {currentValues.privateAccess && (
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>
                  订阅密钥{" "}
                  <span style={{ color: "var(--ant-color-error)" }}>*</span>
                </Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.provideKey.length}/{MAX_PROVIDE_KEY_LEN}
                </Typography.Text>
              </Flex>
              <Space.Compact style={{ width: "100%" }}>
                <Input
                  disabled={!isEditing || !canWrite}
                  maxLength={MAX_PROVIDE_KEY_LEN}
                  placeholder="必填，请输入订阅密钥，例如 mysecret888"
                  status={
                    isEditing && !currentValues.provideKey.trim()
                      ? "error"
                      : undefined
                  }
                  value={currentValues.provideKey}
                  onChange={(e) =>
                    setDraft((prev) => ({
                      ...prev,
                      provideKey: e.target.value,
                    }))
                  }
                />
                <Button
                  icon={<ReloadOutlined />}
                  disabled={!isEditing || !canWrite}
                  onClick={() =>
                    setDraft((prev) => ({
                      ...prev,
                      provideKey: generateRandomKey(16),
                    }))
                  }
                >
                  随机生成
                </Button>
              </Space.Compact>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                开启私有化后必填。客户端及 TVBox 订阅地址均需在末尾追加 ?key=您的密钥（可在右上角订阅地址直接复制）。
              </Typography.Text>
            </div>
          )}
        </Flex>
      </Spin>
    </Card>
  );
}
