"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import { Alert, Avatar, Button, Card, Flex, Input, List, Modal, Space, Spin, Switch, Tag, Typography } from "antd";
import {
  DeleteOutlined,
  EditOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

interface SiteConfigValues {
  siteName: string;
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
}

const DEFAULT_CONFIG: SiteConfigValues = {
  siteName: "",
  keyword: "",
  logo: "",
  state: false,
  describe: "",
  hint: "",
};

const CONFIG_ITEMS: ConfigItem[] = [
  { field: "siteName", label: "网站名称", type: "text" },
  { field: "logo", label: "网站 Logo", type: "image" },
  { field: "keyword", label: "搜索关键字", type: "text" },
  { field: "describe", label: "网站描述", type: "textarea" },
  { field: "state", label: "网站状态", type: "switch" },
  { field: "hint", label: "维护提示", type: "textarea" },
];

function normalizeConfig(data: Partial<SiteConfigValues> | undefined): SiteConfigValues {
  return {
    siteName: String(data?.siteName ?? ""),
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
      <Flex align="center" gap={10}>
        <Avatar src={src} shape="square" size={34} className={styles.logoPreview} />
        <Typography.Text ellipsis>{src}</Typography.Text>
      </Flex>
    );
  }
  const text = String(value || "").trim();
  return text ? <Typography.Text ellipsis>{text}</Typography.Text> : <Typography.Text type="secondary">未设置</Typography.Text>;
}

export default function SiteConfigPageView() {
  const [config, setConfig] = useState<SiteConfigValues>(DEFAULT_CONFIG);
  const [fetching, setFetching] = useState(false);
  const [editingItem, setEditingItem] = useState<ConfigItem | null>(null);
  const [editingValue, setEditingValue] = useState<string | boolean>("");
  const [saving, setSaving] = useState(false);
  const [clearOpen, setClearOpen] = useState(false);
  const [clearPassword, setClearPassword] = useState("");
  const [clearing, setClearing] = useState(false);
  const { message } = useAppMessage();

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
        message.success(resp.msg);
        await getBasicInfo();
      } else {
        message.error(resp.msg);
      }
    } finally {
      setFetching(false);
    }
  };

  const clearFilms = async () => {
    if (!clearPassword) {
      message.error("请输入管理密码");
      return;
    }
    setClearing(true);
    try {
      const resp = await ApiPost("/manage/spider/clear", {
        password: clearPassword,
      });
      if (resp.code === 0) {
        message.success(resp.msg);
        setClearOpen(false);
        setClearPassword("");
        return;
      }
      message.error(resp.msg || "清空全站影视数据失败");
    } finally {
      setClearing(false);
    }
  };

  const editorTitle = useMemo(() => (editingItem ? `编辑${editingItem.label}` : "编辑配置"), [editingItem]);

  useEffect(() => {
    void getBasicInfo();
  }, [getBasicInfo]);

  return (
    <div className={styles.formPanel}>
      <ManagePageHeader
        title="网站配置"
        description="集中维护站点名称、描述、Logo 与站点可用状态等基础信息。"
        actions={
          <Button icon={<ReloadOutlined />} loading={fetching} onClick={handleReset}>
            重置配置
          </Button>
        }
      />

      <Spin spinning={fetching} description="正在加载网站配置...">
        <Card size="small">
          <List
            dataSource={CONFIG_ITEMS}
            renderItem={(item) => (
              <List.Item
                actions={[
                  <Button
                    key="edit"
                    type="text"
                    icon={<EditOutlined />}
                    onClick={() => openEditor(item)}
                  >
                    编辑
                  </Button>,
                ]}
              >
                <List.Item.Meta
                  title={item.label}
                  description={renderPreviewValue(item, config[item.field])}
                />
              </List.Item>
            )}
          />
        </Card>
      </Spin>

      <Card size="small" title="危险操作" className={styles.dangerCard}>
        <Flex justify="space-between" align="center" gap={16} wrap="wrap">
          <Space direction="vertical" size={4} className={styles.dangerText}>
            <Typography.Text type="danger" strong>清空全站影视数据</Typography.Text>
            <Typography.Text type="secondary">
              清空主站影片、附属站播放源、分类映射、搜索标签、轮播与采集失败记录，并重建当前启用主站分类。
            </Typography.Text>
          </Space>
          <Button danger icon={<DeleteOutlined />} onClick={() => setClearOpen(true)}>
            清空全站影视数据
          </Button>
        </Flex>
      </Card>

      <Modal
        title={editorTitle}
        open={Boolean(editingItem)}
        onCancel={closeEditor}
        onOk={() => void saveEditingItem()}
        okText="保存"
        confirmLoading={saving}
        destroyOnHidden
      >
        {editingItem?.type === "switch" ? (
          <Flex align="center" justify="space-between" className={styles.switchEditor}>
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
        ) : (
          <Input
            value={String(editingValue ?? "")}
            onChange={(event) => setEditingValue(event.target.value)}
          />
        )}
      </Modal>

      <Modal
        title="清空全站影视数据"
        open={clearOpen}
        onCancel={() => {
          setClearOpen(false);
          setClearPassword("");
        }}
        onOk={() => void clearFilms()}
        okText="确认清空"
        confirmLoading={clearing}
        okButtonProps={{ danger: true }}
        destroyOnHidden
      >
        <Flex vertical gap={12}>
          <Alert
            showIcon
            type="error"
            message="该操作不可逆"
            description="会清空全站影视数据并重建主站分类框架。执行期间会停止采集任务，请确认当前没有误操作风险。"
          />
          <Input.Password
            placeholder="请输入管理密码"
            value={clearPassword}
            onChange={(event) => setClearPassword(event.target.value)}
          />
        </Flex>
      </Modal>
    </div>
  );
}
