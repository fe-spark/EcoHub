"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Avatar,
  Button,
  Card,
  Flex,
  Input,
  List,
  Modal,
  Space,
  Spin,
  Switch,
  Tag,
  Typography,
} from "antd";

import {
  EditOutlined,
  PictureOutlined,
  ReloadOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useSiteConfig } from "@/components/common/SiteGuard";
import ManagePageHeader from "@/app/manage/components/page-header";
import ImagePicker from "@/app/manage/components/image-picker";
import styles from "./index.module.less";

interface SiteConfigValues {
  siteName: string;
  siteUrl: string;
  keyword: string;
  logo: string;
  state: boolean;
  describe: string;
  hint: string;
}

type EditableField = keyof SiteConfigValues;

interface ConfigItem {
  field: EditableField;
  label: string;
  type: "text" | "textarea" | "switch" | "image";
  hint?: string;
}

const DEFAULT_CONFIG: SiteConfigValues = {
  siteName: "",
  siteUrl: "",
  keyword: "",
  logo: "",
  state: false,
  describe: "",
  hint: "",
};

const CONFIG_ITEMS: ConfigItem[] = [
  { field: "siteName", label: "网站名称", type: "text" },
  {
    field: "siteUrl",
    label: "网站地址",
    type: "text",
    hint: "公网访问根地址，如 https://example.com。用于点击 Logo 跳转，以及 Telegram 通知中的播放链接。",
  },
  {
    field: "logo",
    label: "网站 Logo",
    type: "image",
    hint: "可粘贴图片地址，或从素材中心选择。建议使用方形小图（32×32 或 64×64 PNG）。",
  },
  { field: "keyword", label: "搜索关键字", type: "text" },
  { field: "describe", label: "网站描述", type: "textarea" },
  { field: "state", label: "网站状态", type: "switch" },
  { field: "hint", label: "维护提示", type: "textarea" },
];

function normalizeConfig(data: Partial<SiteConfigValues> | undefined): SiteConfigValues {
  return {
    siteName: String(data?.siteName ?? ""),
    siteUrl: String(data?.siteUrl ?? "").trim(),
    keyword: String(data?.keyword ?? ""),
    logo: String(data?.logo ?? ""),
    state: Boolean(data?.state),
    describe: String(data?.describe ?? ""),
    hint: String(data?.hint ?? ""),
  };
}

function renderPreviewValue(item: ConfigItem, value: SiteConfigValues[EditableField]) {
  if (item.type === "switch") {
    return value ? <Tag color="success">开启</Tag> : <Tag color="default">关闭</Tag>;
  }
  if (item.type === "image") {
    const src = String(value || "").trim();
    if (!src) return <Typography.Text type="secondary">未设置</Typography.Text>;
    return (
      <Space size={8} align="center">
        <Avatar src={src} shape="square" size={32} style={{ borderRadius: 8 }} />
        <Typography.Text ellipsis style={{ maxWidth: 360 }}>
          {src}
        </Typography.Text>
      </Space>
    );
  }
  const text = String(value || "").trim();
  return text ? (
    <Typography.Text ellipsis style={{ maxWidth: 520 }}>
      {text}
    </Typography.Text>
  ) : (
    <Typography.Text type="secondary">未设置</Typography.Text>
  );
}

interface SiteConfigPageViewProps {
  /** 嵌入系统设置 Tabs 时隐藏独立页头 */
  embedded?: boolean;
}

export default function SiteConfigPageView({ embedded = false }: SiteConfigPageViewProps) {
  const [config, setConfig] = useState<SiteConfigValues>(DEFAULT_CONFIG);
  const [fetching, setFetching] = useState(false);
  const [editingItem, setEditingItem] = useState<ConfigItem | null>(null);
  const [editingValue, setEditingValue] = useState<string | boolean>("");
  const [pickerOpen, setPickerOpen] = useState(false);
  const [saving, setSaving] = useState(false);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const { refresh: refreshSiteConfig } = useSiteConfig();

  const getBasicInfo = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/basic");
      if (resp.code === 0) {
        setConfig(normalizeConfig(resp.data));
        return;
      }
      message.error(resp.msg);
    } finally {
      setFetching(false);
    }
  }, [message]);

  const openEditor = (item: ConfigItem) => {
    setEditingItem(item);
    setEditingValue(config[item.field]);
  };

  const closeEditor = () => {
    setEditingItem(null);
    setEditingValue("");
    setPickerOpen(false);
  };

  const saveEditingItem = async () => {
    if (!editingItem) return;
    const nextConfig = { ...config, [editingItem.field]: editingValue };
    setSaving(true);
    try {
      const resp = await ApiPost("/manage/config/basic/update", nextConfig);
      if (resp.code === 0) {
        message.success(resp.msg);
        setConfig(normalizeConfig(nextConfig));
        closeEditor();
        await getBasicInfo();
        await refreshSiteConfig();
        return;
      }
      message.error(resp.msg);
    } finally {
      setSaving(false);
    }
  };

  const handleReset = async () => {
    setFetching(true);
    try {
      const resp = await ApiPost("/manage/config/basic/reset");
      if (resp.code === 0) {
        message.success(resp.msg || "已还原默认基本信息");
        await getBasicInfo();
        await refreshSiteConfig();
      } else {
        message.error(resp.msg);
      }
    } finally {
      setFetching(false);
    }
  };

  const editorTitle = useMemo(
    () => (editingItem ? `编辑${editingItem.label}` : "编辑配置"),
    [editingItem],
  );

  useEffect(() => {
    void getBasicInfo();
  }, [getBasicInfo]);

  const resetAction = (
    <Button icon={<ReloadOutlined />} loading={fetching} onClick={() => void handleReset()}>
      还原默认
    </Button>
  );

  return (
    <div className={styles.formPanel}>
      {embedded ? null : (
        <ManagePageHeader
          title="网站配置"
          description="维护站点基本信息；还原将恢复默认基本信息。"
          actions={resetAction}
        />
      )}

      <Spin spinning={fetching} description="正在加载网站配置...">
        <Card
          title={
            <Space size={8}>
              <SettingOutlined style={{ color: "#1677ff" }} />
              <span>基本信息</span>
            </Space>
          }
          extra={embedded ? resetAction : null}
          className={styles.card}
          styles={{ body: { padding: "8px 16px" } }}
        >
          <List
            itemLayout="horizontal"
            dataSource={CONFIG_ITEMS}
            split
            renderItem={(item) => (
              <List.Item
                style={{ padding: "16px 0" }}
                actions={[
                  <Button
                    key="edit"
                    type="link"
                    icon={<EditOutlined />}
                    disabled={!canWrite}
                    onClick={() => openEditor(item)}
                  >
                    编辑
                  </Button>,
                ]}
              >
                <Space direction="vertical" size={4} className={styles.configMeta}>
                  <Typography.Text strong>{item.label}</Typography.Text>
                  <div className={styles.configValue}>{renderPreviewValue(item, config[item.field])}</div>
                  {item.hint ? (
                    <Typography.Text type="secondary" className={styles.configHint}>
                      {item.hint}
                    </Typography.Text>
                  ) : null}
                </Space>
              </List.Item>
            )}
          />
        </Card>
      </Spin>

      <Modal
        title={editorTitle}
        open={Boolean(editingItem)}
        onCancel={closeEditor}
        okButtonProps={{ disabled: !canWrite }}
        onOk={() => void saveEditingItem()}
        okText="保存"
        confirmLoading={saving}
        destroyOnHidden
      >
        {editingItem?.type === "switch" ? (
          <Flex align="center" justify="space-between" style={{ minHeight: 48 }}>
            <Typography.Text>{editingItem.label}</Typography.Text>
            <Switch
              checked={Boolean(editingValue)}
              checkedChildren="开启"
              unCheckedChildren="关闭"
              onChange={setEditingValue}
            />
          </Flex>
        ) : editingItem?.type === "textarea" ? (
          <Input.TextArea
            autoSize={{ minRows: 4, maxRows: 8 }}
            value={String(editingValue ?? "")}
            onChange={(event) => setEditingValue(event.target.value)}
          />
        ) : editingItem?.type === "image" ? (
          <Space direction="vertical" size={12} style={{ width: "100%" }}>
            {String(editingValue || "").trim() ? (
              <Avatar
                src={String(editingValue)}
                shape="square"
                size={64}
                style={{ borderRadius: 8 }}
              />
            ) : null}
            <Space.Compact style={{ width: "100%" }}>
              <Input
                value={String(editingValue ?? "")}
                onChange={(event) => setEditingValue(event.target.value)}
                placeholder="输入 Logo 地址，或从素材中心选择"
                disabled={!canWrite}
              />
              <Button
                icon={<PictureOutlined />}
                disabled={!canWrite}
                onClick={() => setPickerOpen(true)}
              >
                选图
              </Button>
            </Space.Compact>
          </Space>
        ) : (
          <Input
            value={String(editingValue ?? "")}
            onChange={(event) => setEditingValue(event.target.value)}
          />
        )}
        <ImagePicker
          open={pickerOpen}
          title="从素材中心选择 Logo"
          onCancel={() => setPickerOpen(false)}
          onSelect={(link) => {
            setEditingValue(link);
            setPickerOpen(false);
          }}
        />
      </Modal>
    </div>
  );
}
