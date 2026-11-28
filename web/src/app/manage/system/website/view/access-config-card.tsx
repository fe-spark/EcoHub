"use client";

import React, { forwardRef, useImperativeHandle } from "react";
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
  ReloadOutlined,
  SafetyCertificateOutlined,
} from "@ant-design/icons";
import type { ConfigCardHandle } from "./types";
import { ConfigLoadErrorAlert, useSiteConfigCard } from "./use-config-card";
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
const MIN_PROVIDE_KEY_LEN = 8;
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
  onDirtyChange?: (dirty: boolean) => void;
}

const AccessConfigCard = forwardRef<ConfigCardHandle, AccessConfigCardProps>(
  function AccessConfigCard({ canWrite, onDirtyChange }, ref) {
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
      message,
    } = useSiteConfigCard<AccessConfigPayload>({
      loadUrl: "/manage/config/access",
      saveUrl: "/manage/config/access",
      defaultValue: DEFAULT_ACCESS_CONFIG,
      normalize: normalizeAccessConfig,
      loadErrorText: "加载访问控制配置失败",
      saveErrorText: "保存访问控制配置失败",
      canWrite,
      onDirtyChange,
    });

    useImperativeHandle(
      ref,
      () => ({
        isDirty: () => hasDirty,
        validate: () => {
          const trimmedKey = draft.provideKey.trim();
          if (draft.privateAccess && !trimmedKey) {
            message.error("开启私有化访问时，订阅密钥不能为空");
            return false;
          }
          if (draft.privateAccess && trimmedKey.length < MIN_PROVIDE_KEY_LEN) {
            message.error(`开启私有化访问时，订阅密钥长度不能少于 ${MIN_PROVIDE_KEY_LEN} 位`);
            return false;
          }
          return true;
        },
        save: () => save(),
        discard,
      }),
      [hasDirty, draft.privateAccess, draft.provideKey, save, discard, message],
    );

    const currentValues = draft;

    return (
      <Card
        className={styles.card}
        title={
          <Space size={8} align="center">
            <SafetyCertificateOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>运行与访问控制</span>
          </Space>
        }
      >
        <Spin spinning={fetching} description="正在加载访问控制配置...">
          <ConfigLoadErrorAlert
            visible={loadError}
            text="加载访问控制配置失败"
            loading={fetching}
            onRetry={() => void reload()}
          />
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
                disabled={!editable}
                checked={currentValues.state}
                checkedChildren="开启"
                unCheckedChildren="关闭"
                onChange={(state) => {
                  setIsTouched(true);
                  setDraft((prev) => ({ ...prev, state }));
                }}
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
                disabled={!editable}
                maxLength={MAX_HINT_LEN}
                rows={2}
                placeholder="网站维护中，请稍后再试..."
                value={currentValues.hint}
                onChange={(e) => {
                  setIsTouched(true);
                  setDraft((prev) => ({ ...prev, hint: e.target.value }));
                }}
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
                disabled={!editable}
                checked={currentValues.privateAccess}
                checkedChildren="开启"
                unCheckedChildren="关闭"
                onChange={(privateAccess) => {
                  setIsTouched(true);
                  setDraft((prev) => {
                    let provideKey = prev.provideKey;
                    if (privateAccess && !provideKey.trim()) {
                      provideKey = generateRandomKey(16);
                    }
                    return { ...prev, privateAccess, provideKey };
                  });
                }}
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
                    disabled={!editable}
                    maxLength={MAX_PROVIDE_KEY_LEN}
                    placeholder="必填，至少 8 位，例如 mysecret888"
                    status={
                      !currentValues.provideKey.trim() ||
                      currentValues.provideKey.trim().length < MIN_PROVIDE_KEY_LEN
                        ? "error"
                        : undefined
                    }
                    value={currentValues.provideKey}
                    onChange={(e) => {
                      setIsTouched(true);
                      setDraft((prev) => ({
                        ...prev,
                        provideKey: e.target.value,
                      }));
                    }}
                  />
                  <Button
                    icon={<ReloadOutlined />}
                    disabled={!editable}
                    onClick={() => {
                      setIsTouched(true);
                      setDraft((prev) => ({
                        ...prev,
                        provideKey: generateRandomKey(16),
                      }));
                    }}
                  >
                    随机生成
                  </Button>
                </Space.Compact>
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  开启私有化后必填，至少 8 位。客户端及 TVBox 订阅地址需在末尾追加 ?key=您的密钥。
                </Typography.Text>
              </div>
            )}
          </Flex>
        </Spin>
      </Card>
    );
  },
);

export default AccessConfigCard;
