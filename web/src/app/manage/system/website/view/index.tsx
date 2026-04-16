"use client";

import React, { useState, useEffect, useCallback } from "react";
import { Form, Input, Switch, Button, Typography, Spin, Space, Card, Row, Col } from "antd";
import { SettingOutlined, ReloadOutlined, SaveOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./index.module.less";

const { Title } = Typography;

export default function SiteConfigPageView() {
  const [form] = Form.useForm();
  const [submitting, setSubmitting] = useState(false);
  const [fetching, setFetching] = useState(false);
  const { message } = useAppMessage();

  const getBasicInfo = useCallback(async () => {
    setFetching(true);
    try {
      const resp = await ApiGet("/manage/config/basic");
      if (resp.code === 0) form.setFieldsValue(resp.data);
      else message.error(resp.msg);
    } finally {
      setFetching(false);
    }
  }, [form, message]);

  const handleUpdate = async () => {
    setSubmitting(true);
    try {
      const values = await form.validateFields();
      const resp = await ApiPost("/manage/config/basic/update", values);
      if (resp.code === 0) {
        message.success(resp.msg);
        await getBasicInfo();
      } else message.error(resp.msg);
    } finally {
      setSubmitting(false);
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

  useEffect(() => {
    getBasicInfo();
  }, [getBasicInfo]);

  return (
    <div className={styles.container}>
      <div className={styles.header}>
        <div className={styles.headerLeft}>
          <Title level={4} className={styles.title}>
            网站基础参数配置
          </Title>
        </div>
        <Space size="middle">
          <Button icon={<ReloadOutlined />} loading={fetching} onClick={handleReset}>
            重置
          </Button>
          <Button type="primary" icon={<SaveOutlined />} loading={submitting} onClick={handleUpdate}>
            更新配置
          </Button>
        </Space>
      </div>

      <Spin spinning={fetching} description="正在加载网站配置...">
        <Form form={form} layout="vertical" className={`${styles.form} ${styles.formCompact}`}>
          <Card
            title={
              <Space>
                <SettingOutlined style={{ color: "var(--ant-color-primary)" }} />
                基础配置
              </Space>
            }
            className={styles.sectionCard}
            styles={{
              header: {
                background: "rgba(255, 255, 255, 0.02)",
                borderBottom: "1px solid var(--ant-color-border-secondary)",
              },
            }}
          >
            <Row gutter={[40, 0]} className={styles.formRow}>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item name="siteName" label="网站名称">
                  <Input placeholder="请输入网站名称" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item name="keyword" label="搜索关键字">
                  <Input placeholder="请输入搜索关键字" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item name="state" label="网站状态" valuePropName="checked">
                  <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item name="isVideoProxy" label="视频播放代理" valuePropName="checked">
                  <Switch checkedChildren="开启" unCheckedChildren="关闭" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={16}>
                <Form.Item name="logo" label="网站Logo">
                  <Input placeholder="请输入完整的 Logo 图片 URL 地址" />
                </Form.Item>
              </Col>
              <Col xs={24}>
                <Form.Item name="describe" label="网站描述">
                  <Input.TextArea autoSize={{ minRows: 4, maxRows: 8 }} placeholder="多维度描述本站特色..." />
                </Form.Item>
              </Col>
              <Col xs={24}>
                <Form.Item name="hint" label="维护提示">
                  <Input.TextArea autoSize={{ minRows: 4, maxRows: 8 }} placeholder="当网站处于维护状态时，展示给用户的提示语..." />
                </Form.Item>
              </Col>
            </Row>
          </Card>
        </Form>
      </Spin>
    </div>
  );
}
