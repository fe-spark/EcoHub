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
  Tooltip,
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
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { useFormDirtyTracker } from "@/lib/use-form-dirty";
import type { ProxyConfigValues } from "./types";
import ProxyModulesScope from "./ProxyModulesScope";
import styles from "./index.module.less";

const DEFAULT_CONFIG: ProxyConfigValues = {
  enabled: false,
  proxyUrl: "",
  modules: {
    spider: true,
    tmdb: true,
    notify: true,
    upgrade: true,
  },
};

interface ProxyConfigPageViewProps {
  embedded?: boolean;
}

export default function ProxyConfigPageView({ embedded = false }: ProxyConfigPageViewProps) {
  const [form] = Form.useForm<ProxyConfigValues>();
  const [fetching, setFetching] = useState(true);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testResult, setTestResult] = useState<string | null>(null);
  const [serverData, setServerData] = useState<ProxyConfigValues>(DEFAULT_CONFIG);
  const isGlobalEnabled = Form.useWatch("enabled", form);
  const proxyUrlTouched = useRef(false);

  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const canOperate = canWrite && isAdmin;

  const { isDirty, onValuesChange: trackFormChange, setBaseline, resetToBaseline } =
    useFormDirtyTracker(form, serverData);

  const fetchConfig = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/proxy/config");
      if (resp.code === 0 && resp.data) {
        const normalized: ProxyConfigValues = {
          enabled: Boolean(resp.data.enabled),
          proxyUrl: resp.data.proxyUrl || "",
          modules: {
            spider: resp.data.modules?.spider ?? true,
            tmdb: resp.data.modules?.tmdb ?? true,
            notify: resp.data.modules?.notify ?? true,
            upgrade: resp.data.modules?.upgrade ?? true,
          },
        };
        proxyUrlTouched.current = false;
        setServerData(normalized);
        form.setFieldsValue(normalized);
        setBaseline(normalized);
      }
    } finally {
      setFetching(false);
    }
  }, [form, setBaseline]);

  useEffect(() => {
    void fetchConfig();
  }, [fetchConfig]);

  const handleCancel = () => {
    resetToBaseline();
    proxyUrlTouched.current = false;
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
        setBaseline(values);
        proxyUrlTouched.current = false;
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
    if (!form.getFieldValue("enabled")) {
      message.warning("请先开启全局网络代理后再测试连接");
      return;
    }
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

  return (
    <div className={styles.page}>
      {embedded ? null : (
        <ManagePageHeader
          title="网络代理"
          description="管理系统 HTTP/SOCKS5 网络代理出口及各功能模块（采集、刮削、通知、更新）生效范围。"
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
            <span>网络代理配置</span>
          </Space>
        }
        extra={!isAdmin ? <Tag color="default">仅超级管理员可操作</Tag> : null}
      >
        <Spin spinning={fetching} description="正在加载代理配置...">
          <Form<ProxyConfigValues>
            form={form}
            layout="vertical"
            initialValues={DEFAULT_CONFIG}
            disabled={!canOperate}
            onValuesChange={(changed, allValues) => {
              trackFormChange(changed, allValues);
              if (Object.prototype.hasOwnProperty.call(changed, "proxyUrl")) {
                proxyUrlTouched.current = true;
              }
              if (changed.enabled === false) {
                setTestResult(null);
              }
            }}
          >
            <Flex vertical gap={0} className={styles.contentStack}>
              {/* 最上层：启用/禁用网络代理总开关 */}
              <div className={styles.masterSwitchBlock}>
                <Flex align="center" justify="space-between" gap={20}>
                  <Flex vertical gap={4}>
                    <span className={styles.masterSwitchTitle}>启用全局网络代理</span>
                    <span className={styles.masterSwitchDesc}>
                      开启后各勾选模块的外部网络请求将统一经由此代理转发，关闭后所有外部请求直连
                    </span>
                  </Flex>
                  <Form.Item name="enabled" valuePropName="checked" noStyle>
                    <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                  </Form.Item>
                </Flex>
              </div>

              <Divider style={{ margin: "24px 0" }} />

              {/* 模块 1：代理服务器地址 */}
              <div className={styles.sectionBlock}>
                <div className={styles.sectionHeader}>
                  <ApiOutlined style={{ color: "var(--ant-color-primary)" }} />
                  <span>代理服务器</span>
                </div>

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
                          disabled={!isEnabled || !canOperate}
                        />
                      </Form.Item>
                    );
                  }}
                </Form.Item>
              </div>

              <Divider style={{ margin: "24px 0" }} />

              <ProxyModulesScope
                form={form}
                canOperate={canOperate}
              />


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
                    <Tooltip
                      title={
                        !Boolean(isGlobalEnabled)
                          ? "开启全局网络代理后方可测试连接"
                          : undefined
                      }
                    >
                      <span>
                        <Button
                          icon={<SendOutlined />}
                          loading={testing}
                          disabled={!canOperate || !Boolean(isGlobalEnabled)}
                          onClick={() => void handleTest()}
                        >
                          测试连接
                        </Button>
                      </span>
                    </Tooltip>
                  </Space>
                </div>
              </div>
            </Flex>
          </Form>
        </Spin>
      </Card>

      <UnsavedChangesBar
        visible={isDirty}
        saving={saving}
        onDiscard={handleCancel}
        onSave={() => void handleSave()}
      />
    </div>
  );
}
