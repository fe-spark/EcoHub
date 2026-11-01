"use client";

import React, { useEffect } from "react";
import { Drawer, Form, Input, Select, Space, Button, Flex } from "antd";
import {
  UserOutlined,
  LockOutlined,
  MailOutlined,
  UserAddOutlined,
} from "@ant-design/icons";

const { Option } = Select;

interface UserDrawerProps {
  open: boolean;
  editingUser: any;
  currentUser: any;
  loading: boolean;
  onOk: (values: any) => Promise<void>;
  onCancel: () => void;
}

export function UserDrawer({
  open,
  editingUser,
  currentUser,
  loading,
  onOk,
  onCancel,
}: UserDrawerProps) {
  const [form] = Form.useForm();

  useEffect(() => {
    if (open) {
      form.resetFields();
      if (editingUser) {
        const userNameVal =
          editingUser.userName ??
          editingUser.username ??
          editingUser.user_name ??
          "";
        form.setFieldsValue({
          userName: userNameVal,
          nickName: editingUser.nickName ?? editingUser.nickname ?? "",
          email: editingUser.email || "",
          gender: editingUser.gender ?? 0,
          role: editingUser.role ?? (editingUser.isAdmin ? 1 : editingUser.isVisitor ? 2 : 0),
          status: editingUser.status ?? 0,
          password: "",
        });
      } else {
        form.setFieldsValue({
          userName: "",
          password: "",
          nickName: "",
          email: "",
          gender: 0,
          role: 0,
          status: 0,
        });
      }
    }
  }, [open, editingUser, form]);

  const handleOk = async () => {
    try {
      const values = await form.validateFields();
      await onOk(values);
    } catch {
      // 验证未通过
    }
  };

  return (
    <Drawer
      title={
        <Space>
          <UserAddOutlined style={{ color: "#1677ff" }} />
          <span>{editingUser ? "编辑账号" : "新增账号"}</span>
        </Space>
      }
      open={open}
      onClose={onCancel}
      size={520}
      destroyOnClose
      extra={
        <Space>
          <Button onClick={onCancel} disabled={loading}>
            取消
          </Button>
          <Button type="primary" onClick={handleOk} loading={loading}>
            保存
          </Button>
        </Space>
      }
    >
      <Form form={form} layout="vertical">
        <Form.Item
          name="userName"
          label="用户名"
          rules={[{ required: !editingUser, message: "请输入用户名" }]}
        >
          <Input
            prefix={<UserOutlined />}
            placeholder="登录账号"
            disabled={!!editingUser}
          />
        </Form.Item>

        <Form.Item
          name="password"
          label={editingUser ? "新密码（留空维持原密码）" : "登录密码"}
          rules={[{ required: !editingUser, message: "请输入登录密码" }]}
        >
          <Input.Password prefix={<LockOutlined />} placeholder="请输入密码" />
        </Form.Item>

        <Form.Item name="nickName" label="显示昵称">
          <Input placeholder="用户昵称" />
        </Form.Item>

        <Form.Item
          name="email"
          label="电子邮箱"
          initialValue=""
          rules={[
            {
              validator: (_, value) => {
                const text = String(value ?? "").trim();
                if (!text || /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(text)) {
                  return Promise.resolve();
                }
                return Promise.reject(new Error("请输入有效的电子邮箱地址"));
              },
            },
          ]}
        >
          <Input prefix={<MailOutlined />} placeholder="选填" />
        </Form.Item>

        <Form.Item name="gender" label="性别" initialValue={0}>
          <Select>
            <Option value={0}>保密</Option>
            <Option value={1}>男</Option>
            <Option value={2}>女</Option>
          </Select>
        </Form.Item>

        <Form.Item
          name="role"
          label="身份角色"
          initialValue={0}
        >
          <Select disabled={Boolean(editingUser?.builtin) || !currentUser?.isAdmin}>
            <Option value={1}>超级用户</Option>
            <Option value={0}>普通用户</Option>
            <Option value={2}>访客</Option>
          </Select>
        </Form.Item>

        <Form.Item
          name="status"
          label="账号状态"
          initialValue={0}
        >
          <Select disabled={!currentUser?.isAdmin}>
            <Option value={0}>启用</Option>
            <Option value={1} disabled={editingUser?.id === currentUser?.id}>
              禁用
            </Option>
          </Select>
        </Form.Item>
      </Form>
    </Drawer>
  );
}
