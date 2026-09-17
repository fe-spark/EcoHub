"use client";

import React, { useEffect, useState, useCallback } from "react";
import {
  Card,
  Form,
  Input,
  Switch,
  Select,
  Button,
  Space,
  Spin,
  Tag,
  Divider,
  Flex,
  Row,
  Col,
  Typography,
} from "antd";
import {
  SaveOutlined,
  EditOutlined,
  ApiOutlined,
  CompassOutlined,
  GlobalOutlined,
  SendOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

interface TMDBConfigValues {
  enabled: boolean;
  apiKey: string;
  proxy: string;
  imageDomain: string;
  language: string;
}

const DEFAULT_CONFIG: TMDBConfigValues = {
  enabled: false,
  apiKey: "",
  proxy: "",
  imageDomain: "https://image.tmdb.org/t/p",
  language: "zh-CN",
};

interface TMDBConfigPageViewProps {
  embedded?: boolean;
}

export default function TMDBConfigPageView({ embedded = false }: TMDBConfigPageViewProps) {
  const [form] = Form.useForm<TMDBConfigValues>();
  const [fetching, setFetching] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [serverData, setServerData] = useState<TMDBConfigValues>(DEFAULT_CONFIG);

  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const canOperate = canWrite && isAdmin;

  const fetchConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/tmdb/config");
      if (resp.code === 0 && resp.data) {
        const normalized: TMDBConfigValues = {
          enabled: Boolean(resp.data.enabled),
          apiKey: resp.data.apiKey || "",
          proxy: resp.data.proxy || "",
          imageDomain: resp.data.imageDomain || "https://image.tmdb.org/t/p",
          language: resp.data.language || "zh-CN",
        };
        setServerData(normalized);
        form.setFieldsValue(normalized);
      }
    } finally {
      setFetching(false);
    }
  }, [form]);

  useEffect(() => {
    void fetchConfig();
  }, [fetchConfig]);

  const handleCancel = () => {
    form.setFieldsValue(serverData);
    setIsEditing(false);
  };

  const handleSave = async () => {
    if (!canOperate) return;
    try {
      const values = await form.validateFields();
      setSaving(true);
      const payload: TMDBConfigValues = {
        ...serverData,
        ...values,
      };
      const resp = await ApiPost("/manage/tmdb/config/update", payload);
      if (resp.code === 0) {
        message.success(resp.msg || "TMDB 配置已保存");
        setServerData(payload);
        setIsEditing(false);
        void fetchConfig();
        return;
      }
      message.error(resp.msg || "保存失败");
    } catch {
      // validation error
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    if (!canOperate) return;
    try {
      const apiKeyVal = form.getFieldValue("apiKey");
      if (!apiKeyVal || !apiKeyVal.trim()) {
        message.error("请先填写有效的 API Key");
        return;
      }
      const values = await form.validateFields(["apiKey", "proxy"]);
      setTesting(true);
      const resp = await ApiPost("/manage/tmdb/config/test", {
        ...form.getFieldsValue(),
        apiKey: values.apiKey,
        proxy: values.proxy,
      });
      if (resp.code === 0) {
        message.success(resp.msg || "TMDB 连接测试成功！");
      } else {
        message.error(resp.msg || "连接测试失败");
      }
    } catch (err: any) {
      if (err?.errorFields) {
        message.error("请先填写有效的 API Key");
      }
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          title="刮削设置"
          description="管理 The Movie Database (TMDB) 官方元数据接入、图片反代与精准刮削策略。"
        />
      )}

      <Card
        className={styles.card}
        styles={{
          header: {
            borderBottom: "1px solid var(--ant-color-border-secondary, rgba(0, 0, 0, 0.06))",
            padding: "0 24px",
            minHeight: 54,
          },
          body: {
            padding: "26px 30px",
          },
        }}
        title={
          <Space size={8} align="center">
            <CompassOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>TMDB 影视元数据刮削配置</span>
          </Space>
        }
        extra={
          <Space size={8} align="center">
            {!isAdmin && <Tag color="default">仅超级管理员可操作</Tag>}
            {isEditing ? (
              <>
                <Button size="small" disabled={saving} onClick={handleCancel}>
                  取消
                </Button>
                <Button
                  size="small"
                  type="primary"
                  icon={<SaveOutlined />}
                  loading={saving}
                  disabled={!canOperate}
                  onClick={() => void handleSave()}
                >
                  保存配置
                </Button>
              </>
            ) : (
              <Button
                size="small"
                type="primary"
                icon={<EditOutlined />}
                disabled={!canOperate}
                onClick={() => setIsEditing(true)}
              >
                编辑
              </Button>
            )}
          </Space>
        }
      >
        <Spin spinning={fetching} description="正在加载刮削配置...">
          <Form
            form={form}
            layout="vertical"
            className={styles.form}
            initialValues={DEFAULT_CONFIG}
            disabled={!isEditing || !canOperate}
          >
            <Flex vertical gap={0} className={styles.contentStack}>
              {/* 最上层：启用/禁用刮削总开关卡片 */}
              <div className={styles.masterSwitchBlock}>
                <Flex align="center" justify="space-between" gap={20}>
                  <Flex vertical gap={4}>
                    <span className={styles.masterSwitchTitle}>启用 TMDB 影视元数据刮削</span>
                    <span className={styles.masterSwitchDesc}>
                      开启后支持在影片列表和编辑影片中进行 TMDB 候选检索、原版海报与元数据精准覆盖；关闭后停止所有刮削调用
                    </span>
                  </Flex>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </Flex>
              </div>

              <Divider style={{ margin: "28px 0" }} />

              {/* 模块 1：API 密钥与网络连接 */}
              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <ApiOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>API 密钥与网络连接</span>
                </div>

                <Row gutter={[24, 16]}>
                  <Col xs={24} md={12}>
                    <Form.Item
                      noStyle
                      shouldUpdate={(prev, cur) => prev.enabled !== cur.enabled}
                    >
                      {({ getFieldValue }) => {
                        const isEnabled = Boolean(getFieldValue("enabled"));
                        return (
                          <Form.Item
                            label="TMDB API Key (v3 auth)"
                            name="apiKey"
                            rules={
                              isEnabled
                                ? [{ required: true, message: "启用刮削时请输入 TMDB API Key" }]
                                : []
                            }
                            tooltip={
                              <span>
                                前往 TMDB 账号设置中免费申请 API 密钥 (API Key)
                                <a
                                  href="https://www.themoviedb.org/settings/api"
                                  target="_blank"
                                  rel="noreferrer"
                                  className={styles.helpLink}
                                >
                                  前往申请
                                </a>
                              </span>
                            }
                          >
                            <Input.Password placeholder="API Key" allowClear />
                          </Form.Item>
                        );
                      }}
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="网络代理 (HTTP Proxy)"
                      name="proxy"
                      tooltip="境内服务器访问 TMDB 容易超时，可配置本地或局域网 HTTP 代理 (例如 http://127.0.0.1:7890)。留空则直连。"
                    >
                      <Input placeholder="例如 http://127.0.0.1:7890" allowClear />
                    </Form.Item>
                  </Col>
                </Row>

                <div className={styles.testFooter}>
                  <Space size={8} align="center">
                    <SendOutlined style={{ color: "var(--ant-color-primary)" }} />
                    <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                      支持使用当前填写的 API Key 与网络代理测试连通性，无需先保存配置。
                    </Typography.Text>
                  </Space>
                  <Button
                    icon={<ApiOutlined />}
                    loading={testing}
                    disabled={!isEditing || !canOperate}
                    onClick={() => void handleTest()}
                  >
                    测试连接
                  </Button>
                </div>
              </div>

              <Divider style={{ margin: "28px 0" }} />

              {/* 模块 2：展示与本地化偏好 */}
              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <GlobalOutlined style={{ color: "#722ed1" }} />
                  <span>展示与本地化偏好</span>
                </div>

                <Row gutter={[24, 16]}>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="图片域名 / CDN 反代前缀"
                      name="imageDomain"
                      tooltip="默认使用官方 CDN https://image.tmdb.org/t/p，若有自定义 Cloudflare Worker 或反代域名可替换为自定义前缀。"
                    >
                      <Input placeholder="https://image.tmdb.org/t/p" allowClear />
                    </Form.Item>
                  </Col>
                  <Col xs={24} md={12}>
                    <Form.Item
                      label="刮削首选语言"
                      name="language"
                      tooltip="优先获取该语言的中文译名与剧情简介"
                    >
                      <Select
                        options={[
                          { label: "简体中文 (zh-CN)", value: "zh-CN" },
                          { label: "繁体中文 (zh-TW)", value: "zh-TW" },
                          { label: "英语 (en-US)", value: "en-US" },
                        ]}
                      />
                    </Form.Item>
                  </Col>
                </Row>
              </div>
            </Flex>
          </Form>
        </Spin>
      </Card>
    </div>
  );
}
