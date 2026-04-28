"use client";

import React, { useState, useEffect, useCallback } from "react";
import { Form, Input, Switch, Button, Spin } from "antd";
import {
  ReloadOutlined,
  SaveOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

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
    <div className={styles.formPanel}>
      <ManagePageHeader
        title="网站配置"
        description="集中维护站点名称、描述、Logo 与站点可用状态等基础信息。"
        actions={
          <>
            <Button icon={<ReloadOutlined />} loading={fetching} onClick={handleReset}>
              重置
            </Button>
            <Button
              type="primary"
              icon={<SaveOutlined />}
              loading={submitting}
              onClick={handleUpdate}
            >
              更新配置
            </Button>
          </>
        }
      />

      <Spin spinning={fetching} description="正在加载网站配置...">
        <Form
          form={form}
          layout="vertical"
          className={`${styles.form} ${styles.formCompact}`}
          requiredMark="optional"
        >
          <div className={styles.formBody}>
            <div className={styles.formGrid3}>
              <Form.Item name="siteName" label="网站名称">
                <Input placeholder="请输入网站名称" />
              </Form.Item>
              <Form.Item name="keyword" label="搜索关键字">
                <Input placeholder="请输入搜索关键字" />
              </Form.Item>
              <Form.Item name="logo" label="网站 Logo">
                <Input placeholder="请输入完整的 Logo 图片 URL 地址" />
              </Form.Item>
            </div>

            <div className={styles.formGrid2}>
              <Form.Item
                name="state"
                label="网站状态"
                valuePropName="checked"
              >
                <Switch checkedChildren="开启" unCheckedChildren="关闭" />
              </Form.Item>
              <Form.Item
                name="isVideoProxy"
                label="视频播放代理"
                valuePropName="checked"
              >
                <Switch checkedChildren="开启" unCheckedChildren="关闭" />
              </Form.Item>
            </div>

            <Form.Item name="describe" label="网站描述">
              <Input.TextArea
                autoSize={{ minRows: 4, maxRows: 6 }}
                placeholder="多维度描述本站特色..."
              />
            </Form.Item>

            <Form.Item name="hint" label="维护提示">
              <Input.TextArea
                autoSize={{ minRows: 4, maxRows: 6 }}
                placeholder="当网站处于维护状态时，展示给用户的提示语..."
              />
            </Form.Item>
          </div>
        </Form>
      </Spin>
    </div>
  );
}
