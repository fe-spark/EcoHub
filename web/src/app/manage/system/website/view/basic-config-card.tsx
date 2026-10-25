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
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { isFormValuesEqual } from "@/lib/deep-equal";
import styles from "./basic-config-card.module.less";

export interface DanmakuPayload {
  enabled: boolean;
  autoSniff: boolean;
  apiUrl: string;
  appId?: string;
  appSecret?: string;
}

export interface BasicInfoPayload {
  siteName: string;
  siteUrl: string;
  keyword: string;
  logo: string;
  describe: string;
  danmaku: DanmakuPayload;
}

const DEFAULT_DANMAKU: DanmakuPayload = {
  enabled: true,
  autoSniff: true,
  apiUrl: "",
  appId: "",
  appSecret: "",
};

const DEFAULT_BASIC_INFO: BasicInfoPayload = {
  siteName: "EcoHub",
  siteUrl: "",
  keyword: "",
  logo: "",
  describe: "",
  danmaku: DEFAULT_DANMAKU,
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
    danmaku: {
      enabled: raw?.danmaku?.enabled !== false,
      autoSniff: raw?.danmaku?.autoSniff !== false,
      apiUrl: String(raw?.danmaku?.apiUrl ?? "").trim(),
      appId: String(raw?.danmaku?.appId ?? "").trim(),
      appSecret: String(raw?.danmaku?.appSecret ?? "").trim(),
    },
  };
}

interface BasicConfigCardProps {
  canWrite: boolean;
}

export default function BasicConfigCard({ canWrite }: BasicConfigCardProps) {
  const [data, setData] = useState<BasicInfoPayload>(DEFAULT_BASIC_INFO);
  const [draft, setDraft] = useState<BasicInfoPayload>(DEFAULT_BASIC_INFO);
  const [isTouched, setIsTouched] = useState(false);
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
    if (!draft.siteName.trim()) {
      message.error("网站名称不能为空");
      return;
    }
    setSaving(true);
    try {
      const normalized = normalizeBasicInfo(draft);
      const resp = await ApiPost("/manage/config/basic", normalized);
      if (resp.code === 0) {
        message.success(resp.msg || "基本信息已保存");
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
          <SettingOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>网站基本信息</span>
        </Space>
      }
    >
      <Spin spinning={fetching} description="正在加载基本信息...">
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
              disabled={!canWrite}
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
              disabled={!canWrite}
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
                  disabled={!canWrite}
                  placeholder="输入 Logo 地址，或从素材中心选择"
                  value={currentValues.logo}
                  onChange={(e) => {
                    setIsTouched(true);
                    setDraft((prev) => ({ ...prev, logo: e.target.value }));
                  }}
                />
                <Button
                  icon={<PictureOutlined />}
                  disabled={!canWrite}
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
              disabled={!canWrite}
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
              disabled={!canWrite}
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

          {/* 弹幕功能配置 */}
          <div className={styles.field}>
            <Flex justify="space-between" align="center">
              <div>
                <Typography.Text strong>启用弹幕系统</Typography.Text>
                <div>
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    开启后，播放器将挂载弹幕插件并展示弹幕图层
                  </Typography.Text>
                </div>
              </div>
              <Switch
                disabled={!canWrite}
                checked={currentValues.danmaku?.enabled !== false}
                onChange={(checked) => {
                  setIsTouched(true);
                  setDraft((prev) => ({
                    ...prev,
                    danmaku: { ...(prev.danmaku || DEFAULT_DANMAKU), enabled: checked },
                  }));
                }}
              />
            </Flex>
          </div>

          {currentValues.danmaku?.enabled !== false && (
            <>
              <div className={styles.field}>
                <Flex justify="space-between" align="center">
                  <div>
                    <Typography.Text strong>自动嗅探全网弹幕</Typography.Text>
                    <div>
                      <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                        开启后，播放视频时自动根据片名和集数联网嗅探全网弹幕（弹弹play协议），关闭后仅保留本站自发弹幕
                      </Typography.Text>
                    </div>
                  </div>
                  <Switch
                    disabled={!canWrite}
                    checked={currentValues.danmaku?.autoSniff !== false}
                    onChange={(checked) => {
                      setIsTouched(true);
                      setDraft((prev) => ({
                        ...prev,
                        danmaku: { ...(prev.danmaku || DEFAULT_DANMAKU), autoSniff: checked },
                      }));
                    }}
                  />
                </Flex>
              </div>

              <div className={styles.field}>
                <Typography.Text strong>自定义弹幕 API 地址 (可选)</Typography.Text>
                <Input
                  disabled={!canWrite}
                  placeholder="留空默认走官方接口；自建节点可填如 http://192.168.1.100:9321/api/v2"
                  value={currentValues.danmaku?.apiUrl || ""}
                  onChange={(e) => {
                    setIsTouched(true);
                    setDraft((prev) => ({
                      ...prev,
                      danmaku: { ...(prev.danmaku || DEFAULT_DANMAKU), apiUrl: e.target.value },
                    }));
                  }}
                  style={{ marginTop: 6 }}
                />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  支持兼容弹弹play协议的自建 danmu-api 容器；留空则直接使用官方开放接口（需配置下方 AppId 与 Secret）
                </Typography.Text>
              </div>

              <div className={styles.field}>
                <Typography.Text strong>弹弹play AppId (官方接口必填)</Typography.Text>
                <Input
                  disabled={!canWrite}
                  placeholder="填写开放平台分配的 AppId（例如 jedqb3k6s7）"
                  value={currentValues.danmaku?.appId || ""}
                  onChange={(e) => {
                    setIsTouched(true);
                    setDraft((prev) => ({
                      ...prev,
                      danmaku: { ...(prev.danmaku || DEFAULT_DANMAKU), appId: e.target.value },
                    }));
                  }}
                  style={{ marginTop: 6 }}
                />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  从 dev.dandanplay.com 应用详情页获取
                </Typography.Text>
              </div>

              <div className={styles.field}>
                <Typography.Text strong>弹弹play AppSecret (官方接口必填)</Typography.Text>
                <Input.Password
                  disabled={!canWrite}
                  placeholder="填写 dev.dandanplay.com 分配的 AppSecret 密钥"
                  value={currentValues.danmaku?.appSecret || ""}
                  onChange={(e) => {
                    setIsTouched(true);
                    setDraft((prev) => ({
                      ...prev,
                      danmaku: { ...(prev.danmaku || DEFAULT_DANMAKU), appSecret: e.target.value },
                    }));
                  }}
                  style={{ marginTop: 6 }}
                />
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  在开放平台应用详情页中复制 AppSecret 1 或 AppSecret 2 即可
                </Typography.Text>
              </div>
            </>
          )}
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

      <UnsavedChangesBar
        visible={hasDirty}
        saving={saving}
        onDiscard={handleCancel}
        onSave={() => void handleSave()}
      />
    </Card>
  );
}
