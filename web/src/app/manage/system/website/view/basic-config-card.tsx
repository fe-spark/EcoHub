"use client";

import React, { forwardRef, useImperativeHandle, useState } from "react";
import {
  Avatar,
  Button,
  Card,
  Flex,
  Input,
  Space,
  Spin,
  Typography,
} from "antd";
import {
  PictureOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import ImagePicker from "@/app/manage/components/image-picker";
import type { ConfigCardHandle } from "./types";
import { ConfigLoadErrorAlert, useSiteConfigCard } from "./use-config-card";
import styles from "./basic-config-card.module.less";

export interface BasicInfoPayload {
  siteName: string;
  siteUrl: string;
  keyword: string;
  logo: string;
  describe: string;
}

const DEFAULT_BASIC_INFO: BasicInfoPayload = {
  siteName: "EcoHub",
  siteUrl: "",
  keyword: "",
  logo: "",
  describe: "",
};
const MAX_SITE_NAME_LEN = 64;
const MAX_KEYWORD_LEN = 128;
const MAX_DESCRIBE_LEN = 512;

function normalizeBasicInfo(
  raw?: Partial<BasicInfoPayload> | null,
): BasicInfoPayload {
  return {
    siteName: String(raw?.siteName ?? "").trim() || DEFAULT_BASIC_INFO.siteName,
    siteUrl: String(raw?.siteUrl ?? "").trim(),
    keyword: String(raw?.keyword ?? "").trim(),
    logo: String(raw?.logo ?? "").trim(),
    describe: String(raw?.describe ?? "").trim(),
  };
}

interface BasicConfigCardProps {
  canWrite: boolean;
  onDirtyChange?: (dirty: boolean) => void;
}

const BasicConfigCard = forwardRef<ConfigCardHandle, BasicConfigCardProps>(
  function BasicConfigCard({ canWrite, onDirtyChange }, ref) {
    const [pickerOpen, setPickerOpen] = useState(false);
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
    } = useSiteConfigCard<BasicInfoPayload>({
      loadUrl: "/manage/config/basic",
      saveUrl: "/manage/config/basic",
      defaultValue: DEFAULT_BASIC_INFO,
      normalize: normalizeBasicInfo,
      loadErrorText: "加载网站基本信息失败",
      saveErrorText: "保存网站基本信息失败",
      canWrite,
      onDirtyChange,
    });

    useImperativeHandle(
      ref,
      () => ({
        isDirty: () => hasDirty,
        validate: () => {
          if (!draft.siteName.trim()) {
            message.error("网站名称不能为空");
            return false;
          }
          return true;
        },
        save: () => save(),
        discard,
      }),
      [hasDirty, draft.siteName, save, discard, message],
    );

    const currentValues = draft;

    return (
      <Card
        className={styles.card}
        title={
          <Space size={8} align="center">
            <SettingOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>网站基本信息</span>
          </Space>
        }
      >
        <Spin spinning={fetching} description="正在加载基本信息...">
          <ConfigLoadErrorAlert
            visible={loadError}
            text="加载网站基本信息失败"
            loading={fetching}
            onRetry={() => void reload()}
          />
          <Flex vertical gap={16}>
            {/* 网站名称 */}
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>网站名称</Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.siteName.length}/{MAX_SITE_NAME_LEN}
                </Typography.Text>
              </Flex>
              <Input
                disabled={!editable}
                maxLength={MAX_SITE_NAME_LEN}
                placeholder="请输入网站名称，例如 EcoHub"
                value={currentValues.siteName}
                onChange={(e) => {
                  setIsTouched(true);
                  setDraft((prev) => ({ ...prev, siteName: e.target.value }));
                }}
              />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                展示在前台顶部导航、浏览器标题及页面元数据中
              </Typography.Text>
            </div>

            {/* 网站访问地址 */}
            <div className={styles.field}>
              <Typography.Text strong>网站访问地址</Typography.Text>
              <Input
                disabled={!editable}
                placeholder="https://example.com"
                value={currentValues.siteUrl}
                onChange={(e) => {
                  setIsTouched(true);
                  setDraft((prev) => ({ ...prev, siteUrl: e.target.value }));
                }}
              />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                公网访问根地址，用于点击 Logo 跳转以及外部链接复用
              </Typography.Text>
            </div>

            {/* 网站 Logo */}
            <div className={styles.field}>
              <Typography.Text strong>网站 Logo</Typography.Text>
              <div className={styles.logoRow}>
                {currentValues.logo ? (
                  <Avatar
                    src={currentValues.logo}
                    shape="square"
                    size={36}
                    style={{ borderRadius: 8, flexShrink: 0 }}
                  />
                ) : null}
                <Space.Compact style={{ width: "100%" }}>
                  <Input
                    disabled={!editable}
                    placeholder="输入 Logo 地址，或从素材中心选择"
                    value={currentValues.logo}
                    onChange={(e) => {
                      setIsTouched(true);
                      setDraft((prev) => ({ ...prev, logo: e.target.value }));
                    }}
                  />
                  <Button
                    icon={<PictureOutlined />}
                    disabled={!editable}
                    onClick={() => setPickerOpen(true)}
                  >
                    选图
                  </Button>
                </Space.Compact>
              </div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                建议使用方形小图（64×64 PNG/SVG），透明底最佳
              </Typography.Text>
            </div>

            {/* 搜索关键字 */}
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>SEO 搜索关键字</Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.keyword.length}/{MAX_KEYWORD_LEN}
                </Typography.Text>
              </Flex>
              <Input
                disabled={!editable}
                maxLength={MAX_KEYWORD_LEN}
                placeholder="在线视频, 免费观影, 高清电影"
                value={currentValues.keyword}
                onChange={(e) => {
                  setIsTouched(true);
                  setDraft((prev) => ({ ...prev, keyword: e.target.value }));
                }}
              />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                用于搜索引擎 SEO 优化，多个关键词以逗号隔开
              </Typography.Text>
            </div>

            {/* 网站描述 */}
            <div className={styles.field}>
              <Flex justify="space-between" align="baseline">
                <Typography.Text strong>网站描述</Typography.Text>
                <Typography.Text type="secondary">
                  {currentValues.describe.length}/{MAX_DESCRIBE_LEN}
                </Typography.Text>
              </Flex>
              <Input.TextArea
                disabled={!editable}
                maxLength={MAX_DESCRIBE_LEN}
                rows={3}
                placeholder="请输入网站描述信息..."
                value={currentValues.describe}
                onChange={(e) => {
                  setIsTouched(true);
                  setDraft((prev) => ({ ...prev, describe: e.target.value }));
                }}
              />
            </div>
          </Flex>
        </Spin>

        <ImagePicker
          open={pickerOpen}
          title="从素材中心选择 Logo"
          onCancel={() => setPickerOpen(false)}
          onSelect={(link) => {
            setIsTouched(true);
            setDraft((prev) => ({ ...prev, logo: link }));
            setPickerOpen(false);
          }}
        />
      </Card>
    );
  },
);

export default BasicConfigCard;
