"use client";

import React, { useEffect, useState } from "react";
import { Card, Form, Input, Switch, Button, Space, Tag, Divider, Flex, Alert, Typography } from "antd";
import {
  SaveOutlined,
  ApiOutlined,
  MessageOutlined,
  LinkOutlined,
  InfoCircleOutlined,
} from "@ant-design/icons";
import { useAppMessage } from "@/lib/useAppMessage";
import { getDanmuConfig, setDanmuConfig, probeDanmuService, type DanmuConfig } from "@/lib/danmu";
import styles from "./index.module.less";

/**
 * 弹幕配置（客户端存储）
 *
 * 后端是编译好的 Go 二进制，无弹幕相关接口，且所有配置接口均为强类型白名单
 * （不接受额外字段），因此本页配置写入浏览器 localStorage，由播放页读取生效。
 */

const DEFAULT_CONFIG: DanmuConfig = { enabled: true, apiUrl: "" };

export default function DanmuConfigPageView({ embedded = false }: { embedded?: boolean }) {
  const [form] = Form.useForm<DanmuConfig>();
  const [loaded, setLoaded] = useState(false);
  const [testing, setTesting] = useState(false);
  const [testingOk, setTestingOk] = useState<string | null>(null);
  const { message } = useAppMessage();

  useEffect(() => {
    const cfg = getDanmuConfig();
    form.setFieldsValue(cfg);
    setLoaded(true);
  }, [form]);

  const handleSave = async () => {
    const values = await form.validateFields();
    setDanmuConfig({ enabled: Boolean(values.enabled), apiUrl: values.apiUrl || "" });
    form.setFieldsValue(getDanmuConfig());
    message.success("弹幕配置已保存，刷新播放页后生效");
  };

  const handleTest = async () => {
    const values = await form.validateFields(["apiUrl"]);
    setTesting(true);
    setTestingOk(null);
    try {
      const cfg: DanmuConfig = { enabled: true, apiUrl: values.apiUrl || "" };
      const r = await probeDanmuService(cfg);
      setTestingOk(`连接成功，样例「庆余年」匹配到 episodeId=${r.episodeId}，共 ${r.count} 条弹幕`);
      message.success("弹幕服务连通性测试通过");
    } catch (e: any) {
      message.error(`连接失败：${e?.message || e}`);
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className={styles.page}>
      <Card
        className={styles.card}
        styles={{
          header: {
            borderBottom: "1px solid var(--ant-color-border-secondary, rgba(0, 0, 0, 0.06))",
            padding: "0 24px",
            minHeight: 54,
          },
          body: { padding: "26px 30px" },
        }}
        title={
          <Space size={8} align="center">
            <MessageOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>弹幕系统配置</span>
          </Space>
        }
        extra={
          <Button type="primary" size="small" icon={<SaveOutlined />} onClick={() => void handleSave()}>
            保存配置
          </Button>
        }
      >
        <Alert
          type="info"
          showIcon
          icon={<InfoCircleOutlined />}
          style={{ marginBottom: 24 }}
          message="配置保存在当前浏览器，本机所有播放页生效"
          description="EcoHub 后端为编译产物、未提供弹幕配置接口，因此此处配置写入浏览器本地存储。更换设备或浏览器后需要重新设置（默认值已内置，开箱即可播放弹幕）。"
        />

        <Form form={form} layout="vertical" initialValues={DEFAULT_CONFIG} disabled={!loaded}>
          <div className={styles.masterSwitchBlock}>
            <Flex align="center" justify="space-between" gap={20}>
              <Flex vertical gap={4}>
                <span className={styles.masterSwitchTitle}>启用弹幕系统</span>
                <span className={styles.masterSwitchDesc}>
                  开启后播放页自动按片名匹配并加载弹幕；关闭后所有播放页不再请求弹幕服务
                </span>
              </Flex>
              <Form.Item name="enabled" valuePropName="checked" noStyle>
                <Switch checkedChildren="开启" unCheckedChildren="关闭" />
              </Form.Item>
            </Flex>
          </div>

          <Divider style={{ margin: "28px 0" }} />

          <div className={styles.sectionBlock}>
            <div className={styles.sectionHeader}>
              <LinkOutlined style={{ color: "var(--ant-color-primary)" }} />
              <span>弹幕接口地址</span>
            </div>

            <Form.Item
              label="弹幕服务 API 地址"
              name="apiUrl"
              tooltip="支持 dandanplay 协议的服务（如 logvar/danmu-api）。留空则走 EcoHub 同源代理，最省心。"
              extra={
                <span>
                  留空 = 走 EcoHub 同源代理 <code>/danmu-api/*</code>（推荐：内外网均可访问、无跨域、
                  HTTPS 下不会混合内容拦截）。填写完整地址则浏览器直连，例如{" "}
                  <code>http://192.168.1.100:9321</code>。
                </span>
              }
            >
              <Input placeholder="留空使用同源代理，或填写如 http://192.168.1.100:9321" allowClear />
            </Form.Item>

            <div className={styles.testFooter}>
              <Space size={8} align="center">
                <ApiOutlined style={{ color: "var(--ant-color-primary)" }} />
                <Typography.Text type="secondary" style={{ fontSize: 13 }}>
                  用当前填写的地址实探一次弹幕服务，无需先保存。
                </Typography.Text>
                {testingOk && <Tag color="green">{testingOk}</Tag>}
              </Space>
              <Button icon={<ApiOutlined />} loading={testing} onClick={() => void handleTest()}>
                测试连接
              </Button>
            </div>
          </div>
        </Form>
      </Card>
    </div>
  );
}