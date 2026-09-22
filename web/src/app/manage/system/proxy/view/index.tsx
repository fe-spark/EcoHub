"use client";

import React, { useEffect, useState, useCallback, useMemo, useRef } from "react";
import {
  Card,
  Form,
  Input,
  Switch,
  Radio,
  Select,
  Button,
  Space,
  Spin,
  Tag,
  Divider,
  Flex,
  Row,
  Col,
} from "antd";
import {
  SaveOutlined,
  EditOutlined,
  ApiOutlined,
  GlobalOutlined,
  SendOutlined,
  CheckCircleOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import type { ProxyConfigValues, CollectSourceOption } from "./types";
import styles from "./index.module.less";

const DEFAULT_CONFIG: ProxyConfigValues = {
  enabled: false,
  proxyUrl: "",
  scope: "all",
  sourceIds: [],
};

interface ProxyConfigPageViewProps {
  embedded?: boolean;
}

export default function ProxyConfigPageView({ embedded = false }: ProxyConfigPageViewProps) {
  const [form] = Form.useForm<ProxyConfigValues>();
  const [fetching, setFetching] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);
  const [serverData, setServerData] = useState<ProxyConfigValues>(DEFAULT_CONFIG);
  const [sources, setSources] = useState<CollectSourceOption[]>([]);
  const proxyUrlTouched = useRef(false);

  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const canOperate = canWrite && isAdmin;

  const currentScope = Form.useWatch("scope", form);

  const fetchSources = useCallback(async () => {
    try {
      const resp = await ApiGet("/manage/collect/options?all=1");
      if (resp.code === 0 && Array.isArray(resp.data)) {
        setSources(resp.data);
      }
    } catch {
      // 容错处理
    }
  }, []);

  const fetchConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/proxy/config");
      if (resp.code === 0 && resp.data) {
        const normalized: ProxyConfigValues = {
          enabled: Boolean(resp.data.enabled),
          proxyUrl: resp.data.proxyUrl || "",
          scope: resp.data.scope === "custom" ? "custom" : "all",
          sourceIds: Array.isArray(resp.data.sourceIds) ? resp.data.sourceIds : [],
        };
        proxyUrlTouched.current = false;
        setServerData(normalized);
        form.setFieldsValue(normalized);
      }
    } finally {
      setFetching(false);
    }
  }, [form]);

  useEffect(() => {
    void Promise.all([fetchConfig(), fetchSources()]);
  }, [fetchConfig, fetchSources]);

  const handleCancel = () => {
    form.setFieldsValue(serverData);
    setIsEditing(false);
    setTestResult(null);
  };

  const handleSave = async () => {
    if (!canOperate) return;
    try {
      const values = await form.validateFields();
      setSaving(true);
      const resp = await ApiPost("/manage/proxy/config/update", {
        ...values,
        preserveAuth: !proxyUrlTouched.current,
      });
      if (resp.code === 0) {
        message.success(resp.msg || "代理配置保存成功");
        setServerData(values);
        setIsEditing(false);
        void fetchConfig();
        return;
      }
      message.error(resp.msg || "保存失败");
    } catch (err: any) {
      if (err?.errorFields) return;
      message.error(err?.message || "保存失败");
    } finally {
      setSaving(false);
    }
  };

  const handleTest = async () => {
    if (!canOperate) return;
    try {
      const proxyUrlVal = form.getFieldValue("proxyUrl");
      if (!proxyUrlVal || !String(proxyUrlVal).trim()) {
        message.error("请先填写代理服务器地址");
        return;
      }
      const values = await form.validateFields(["proxyUrl"]);
      setTesting(true);
      setTestResult(null);
      const resp = await ApiPost("/manage/proxy/config/test", {
        proxyUrl: values.proxyUrl,
      });
      if (resp.code === 0) {
        const latency = resp.data?.latency ?? 0;
        setTestResult(`连接正常 (${latency}ms)`);
        message.success(resp.msg || "代理连接测试成功！");
      } else {
        setTestResult(null);
        message.error(resp.msg || "代理连通失败");
      }
    } catch (err: any) {
      if (err?.errorFields) {
        message.error("请先填写有效的代理服务器地址");
      }
    } finally {
      setTesting(false);
    }
  };

  const sourceSelectOptions = useMemo(() => {
    return sources.map((s) => ({
      label: s.state === false ? `${s.name}（未启用）` : s.name,
      value: s.id,
      key: s.id,
    }));
  }, [sources]);

  const handleSelectAllSources = () => {
    form.setFieldValue(
      "sourceIds",
      sources.map((s) => s.id),
    );
  };

  const handleClearSources = () => {
    form.setFieldValue("sourceIds", []);
  };

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          title="网络代理"
          description="管理影视采集源 HTTP/SOCKS5 网络代理节点与定向加速范围。"
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
            <ApiOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>影视采集网络代理配置</span>
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
        <Spin spinning={fetching} description="正在加载代理配置...">
          <Form<ProxyConfigValues>
            form={form}
            layout="vertical"
            initialValues={DEFAULT_CONFIG}
            disabled={!isEditing || !canOperate}
            onValuesChange={(changed) => {
              if (Object.prototype.hasOwnProperty.call(changed, "proxyUrl")) {
                proxyUrlTouched.current = true;
              }
            }}
          >
            <Flex vertical gap={0} className={styles.contentStack}>
              {/* 最上层：启用/禁用网络代理总开关 */}
              <div className={styles.masterSwitchBlock}>
                <Flex align="center" justify="space-between" gap={20}>
                  <Flex vertical gap={4}>
                    <span className={styles.masterSwitchTitle}>启用影视采集网络代理</span>
                    <span className={styles.masterSwitchDesc}>
                      开启后支持对指定或全部影视采集源进行网络代理访问，可绕过地域限制或加速远程源站拉取；关闭后所有采集直连
                    </span>
                  </Flex>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </Flex>
              </div>

              <Divider style={{ margin: "28px 0" }} />

              {/* 模块 1：代理服务与生效范围设置 */}
              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <ApiOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>代理服务器设置</span>
                </div>

                <Row gutter={[24, 16]}>
                  <Col xs={24} md={16}>
                    <Form.Item
                      noStyle
                      shouldUpdate={(prev, cur) => prev.enabled !== cur.enabled}
                    >
                      {({ getFieldValue }) => {
                        const isEnabled = Boolean(getFieldValue("enabled"));
                        return (
                          <Form.Item
                            label="代理服务器地址 (Proxy URL)"
                            name="proxyUrl"
                            rules={
                              isEnabled
                                ? [
                                    {
                                      validator: async (_, value: string) => {
                                        const trimmed = String(value || "").trim();
                                        if (!trimmed) {
                                          return Promise.reject(new Error("开启代理时请输入代理服务器地址"));
                                        }
                                        const ok =
                                          trimmed.startsWith("http://") ||
                                          trimmed.startsWith("https://") ||
                                          trimmed.startsWith("socks5://") ||
                                          /^[a-zA-Z0-9.-]+:\d+$/.test(trimmed);
                                        if (!ok) {
                                          return Promise.reject(
                                            new Error("需以 http://、https:// 或 socks5:// 开头"),
                                          );
                                        }
                                      },
                                    },
                                  ]
                                : []
                            }
                            tooltip="支持 HTTP、HTTPS 与 SOCKS5 协议，例如：http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                          >
                            <Input
                              placeholder="http://127.0.0.1:7890 或 socks5://127.0.0.1:1080"
                              allowClear
                            />
                          </Form.Item>
                        );
                      }}
                    </Form.Item>
                  </Col>

                  <Col xs={24} md={8}>
                    <Form.Item
                      label="应用范围 (Scope)"
                      name="scope"
                      tooltip="全部采集源生效：所有源站拉取均走代理；仅指定采集源：仅被勾选的站点生效"
                    >
                      <Radio.Group optionType="button" buttonStyle="solid" style={{ width: "100%" }}>
                        <Radio.Button value="all" style={{ width: "50%", textAlign: "center" }}>
                          全部采集源
                        </Radio.Button>
                        <Radio.Button value="custom" style={{ width: "50%", textAlign: "center" }}>
                          指定采集源
                        </Radio.Button>
                      </Radio.Group>
                    </Form.Item>
                  </Col>
                </Row>

                {currentScope === "custom" && (
                  <Form.Item
                    label={
                      <Flex justify="space-between" align="center" style={{ width: "100%" }}>
                        <span>生效采集源列表</span>
                        <Space size={8}>
                          <Button
                            type="link"
                            size="small"
                            style={{ padding: 0 }}
                            disabled={!isEditing || !canOperate}
                            onClick={handleSelectAllSources}
                          >
                            全选
                          </Button>
                          <Button
                            type="link"
                            size="small"
                            style={{ padding: 0 }}
                            disabled={!isEditing || !canOperate}
                            onClick={handleClearSources}
                          >
                            清空
                          </Button>
                        </Space>
                      </Flex>
                    }
                    name="sourceIds"
                    tooltip="仅在此列表中的采集源在拉取数据、接口测试或实时检索时走代理"
                  >
                    <Select
                      mode="multiple"
                      placeholder="请选择需要走代理的采集源"
                      options={sourceSelectOptions}
                      style={{ width: "100%" }}
                      showSearch
                      filterOption={(input, opt) =>
                        String(opt?.label || "")
                          .toLowerCase()
                          .includes(input.toLowerCase())
                      }
                    />
                  </Form.Item>
                )}
              </div>

              <Divider style={{ margin: "28px 0" }} />

              {/* 模块 2：连通性与可用性测试 */}
              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <GlobalOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>连通性与可用性测试</span>
                </div>

                <div className={styles.testFooter}>
                  <Flex vertical gap={4}>
                    <span style={{ fontWeight: 600, fontSize: 14 }}>测试网络代理连接</span>
                    <span style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>
                      通过当前填写的代理服务器向测试目标发起请求，验证代理节点连通状态与延迟
                    </span>
                  </Flex>

                  <Space align="center" size={12}>
                    {testResult && (
                      <Tag color="success" icon={<CheckCircleOutlined />}>
                        {testResult}
                      </Tag>
                    )}
                    <Button
                      icon={<SendOutlined />}
                      loading={testing}
                      disabled={!canOperate}
                      onClick={() => void handleTest()}
                    >
                      测试连接
                    </Button>
                  </Space>
                </div>
              </div>
            </Flex>
          </Form>
        </Spin>
      </Card>
    </div>
  );
}
