"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Avatar,
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
  PictureOutlined,
  SaveOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useSiteConfig } from "@/components/common/SiteGuard";
import ImagePicker from "@/app/manage/components/image-picker";
import styles from "./basic-config-card.module.less";

export interface BasicInfoPayload {
  siteName: string;
  siteUrl: string;
  keyword: string;
  logo: string;
  state: boolean;
  describe: string;
  hint: string;
}

const DEFAULT_BASIC_INFO: BasicInfoPayload = {
  siteName: "EcoHub",
  siteUrl: "",
  keyword: "",
  logo: "",
  state: true,
  describe: "",
  hint: "网站升级中, 暂时无法访问 !!!",
};

const MAX_SITE_NAME_LEN = 64;
const MAX_KEYWORD_LEN = 128;
const MAX_DESCRIBE_LEN = 512;
const MAX_HINT_LEN = 256;

function normalizeBasicInfo(raw?: Partial<BasicInfoPayload> | null): BasicInfoPayload {
  return {
    siteName: String(raw?.siteName ?? "").trim() || DEFAULT_BASIC_INFO.siteName,
    siteUrl: String(raw?.siteUrl ?? "").trim(),
    keyword: String(raw?.keyword ?? "").trim(),
    logo: String(raw?.logo ?? "").trim(),
    state: raw?.state === undefined ? true : Boolean(raw.state),
    describe: String(raw?.describe ?? "").trim(),
    hint: String(raw?.hint ?? "").trim() || DEFAULT_BASIC_INFO.hint,
  };
}

interface BasicConfigCardProps {
  canWrite: boolean;
}

export default function BasicConfigCard({ canWrite }: BasicConfigCardProps) {
  const [data, setData] = useState<BasicInfoPayload>(DEFAULT_BASIC_INFO);
  const [draft, setDraft] = useState<BasicInfoPayload>(DEFAULT_BASIC_INFO);
  const [isEditing, setIsEditing] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [saving, setSaving] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);

  const { message } = useAppMessage();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const loadData = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/basic");
      if (resp.code === 0 && resp.data) {
        const normalized = normalizeBasicInfo(resp.data);
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
    if (!draft.siteName.trim()) {
      message.error("网站名称不能为空");
      return;
    }
    setSaving(true);
    try {
      const normalized = normalizeBasicInfo(draft);
      const resp = await ApiPost("/manage/config/basic/update", normalized);
      if (resp.code === 0) {
        message.success(resp.msg || "基本信息已保存");
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
          <SettingOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>基本信息配置</span>
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
                保存信息
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
      <Spin spinning={fetching} description="正在加载基本信息...">
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

          {/* 网站名称 */}
          <div className={styles.field}>
            <Flex justify="space-between" align="baseline">
              <Typography.Text strong>网站名称</Typography.Text>
              <Typography.Text type="secondary">
                {currentValues.siteName.length}/{MAX_SITE_NAME_LEN}
              </Typography.Text>
            </Flex>
            <Input
              disabled={!isEditing || !canWrite}
              maxLength={MAX_SITE_NAME_LEN}
              placeholder="请输入网站名称，例如 EcoHub"
              value={currentValues.siteName}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, siteName: e.target.value }))
              }
            />
            <Typography.Text type="secondary" style={{ fontSize: 12 }}>
              展示在前台顶部导航、浏览器标题及页面元数据中
            </Typography.Text>
          </div>

          {/* 网站访问地址 */}
          <div className={styles.field}>
            <Typography.Text strong>网站访问地址</Typography.Text>
            <Input
              disabled={!isEditing || !canWrite}
              placeholder="https://example.com"
              value={currentValues.siteUrl}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, siteUrl: e.target.value }))
              }
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
                  disabled={!isEditing || !canWrite}
                  placeholder="输入 Logo 地址，或从素材中心选择"
                  value={currentValues.logo}
                  onChange={(e) =>
                    setDraft((prev) => ({ ...prev, logo: e.target.value }))
                  }
                />
                <Button
                  icon={<PictureOutlined />}
                  disabled={!isEditing || !canWrite}
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
              disabled={!isEditing || !canWrite}
              maxLength={MAX_KEYWORD_LEN}
              placeholder="在线视频, 免费观影, 高清电影"
              value={currentValues.keyword}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, keyword: e.target.value }))
              }
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
              disabled={!isEditing || !canWrite}
              maxLength={MAX_DESCRIBE_LEN}
              rows={3}
              placeholder="请输入网站描述信息..."
              value={currentValues.describe}
              onChange={(e) =>
                setDraft((prev) => ({ ...prev, describe: e.target.value }))
              }
            />
          </div>

          {/* 维护提示 */}
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
        </Flex>
      </Spin>

      <ImagePicker
        open={pickerOpen}
        title="从素材中心选择 Logo"
        onCancel={() => setPickerOpen(false)}
        onSelect={(link) => {
          setDraft((prev) => ({ ...prev, logo: link }));
          setPickerOpen(false);
        }}
      />
    </Card>
  );
}
