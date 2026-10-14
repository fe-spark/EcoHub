"use client";

import React, { useState, useEffect } from "react";
import {
  Modal,
  Form,
  Select,
  InputNumber,
  Radio,
  Space,
} from "antd";
import { CompassOutlined, FireOutlined, StarOutlined, ClockCircleOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { BannerRecord, MAX_BANNER_COUNT } from "./types";

interface BannerQuickModalProps {
  open: boolean;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onSuccess: (banners: BannerRecord[]) => void;
}

interface FormValues {
  strategy: "hot" | "score" | "latest";
  categories?: number[];
  count: number;
}

export default function BannerQuickModal({
  open,
  tmdbEnabled = false,
  onClose,
  onSuccess,
}: BannerQuickModalProps) {
  const [form] = Form.useForm<FormValues>();
  const [submitting, setSubmitting] = useState(false);
  const [categoryOptions, setCategoryOptions] = useState<{ label: string; value: number }[]>([]);
  const { message } = useAppMessage();

  useEffect(() => {
    if (!open) {
      return;
    }
    // 拉取当前首选站的所有可用一级分类
    ApiGet("/categories").then((resp) => {
      if (resp.code === 0 && Array.isArray(resp.data)) {
        setCategoryOptions(
          resp.data.map((c: { id?: number; name?: string }) => ({
            label: c.name || "",
            value: Number(c.id),
          })),
        );
      }
    }).catch(() => {});

    form.setFieldsValue({
      strategy: "hot",
      categories: [],
      count: 6,
    });
  }, [open, form]);

  const handleSubmit = async () => {
    try {
      const values = await form.validateFields();
      setSubmitting(true);
      const resp = await ApiPost("/manage/banner/quick-generate", {
        count: values.count,
        strategy: values.strategy,
        categories: values.categories,
      });
      if (resp.code === 0) {
        message.success("一键排片完成并已实时生效");
        onSuccess(resp.data || []);
        onClose();
      } else {
        message.error(resp.msg || "一键排片失败");
      }
    } catch (err: any) {
      if (err?.errorFields) {
        return;
      }
      message.error(err?.message || "网络异常，一键排片失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Modal
      title={
        <Space size={8}>
          <CompassOutlined style={{ color: "var(--ant-color-primary)" }} />
          <span>一键排片</span>
        </Space>
      }
      open={open}
      onCancel={onClose}
      onOk={handleSubmit}
      confirmLoading={submitting}
      okText="确定"
      cancelText="取消"
      width={480}
      destroyOnHidden
    >
      <Form form={form} layout="vertical" initialValues={{ strategy: "hot", count: 6 }}>
        <Form.Item
          name="strategy"
          label="排序策略"
          rules={[{ required: true, message: "请选择排序策略" }]}
        >
          <Radio.Group buttonStyle="solid" style={{ width: "100%" }}>
            <Radio.Button value="hot" style={{ width: "33.33%", textAlign: "center" }}>
              <FireOutlined /> 最热优先
            </Radio.Button>
            <Radio.Button value="score" style={{ width: "33.33%", textAlign: "center" }}>
              <StarOutlined /> 评分优先
            </Radio.Button>
            <Radio.Button value="latest" style={{ width: "33.33%", textAlign: "center" }}>
              <ClockCircleOutlined /> 最新更新
            </Radio.Button>
          </Radio.Group>
        </Form.Item>

        <Form.Item
          name="categories"
          label="限定分类"
        >
          <Select
            mode="multiple"
            allowClear
            placeholder="全部分类"
            options={categoryOptions}
            maxTagCount="responsive"
          />
        </Form.Item>

        <Form.Item
          name="count"
          label="排片数量"
          rules={[{ required: true, message: "请输入排片数量" }]}
        >
          <InputNumber min={1} max={MAX_BANNER_COUNT} style={{ width: "100%" }} />
        </Form.Item>
      </Form>
    </Modal>
  );
}
