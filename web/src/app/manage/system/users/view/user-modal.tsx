"use client";

import React, { useEffect } from "react";
import { Modal, Form, Input, Select, Space } from "antd";
import {
  UserOutlined,
  LockOutlined,
  MailOutlined,
  UserAddOutlined,
} from "@ant-design/icons";

const { Option } = Select;

interface UserModalProps {
  open: boolean;
  editingUser: any;
  currentUser: any;
  loading: boolean;
  onOk: (values: any) => Promise<void>;
  onCancel: () => void;
}

export function UserModal({
  open,
  editingUser,
  currentUser,
  loading,
  onOk,
  onCancel,
}: UserModalProps) {
  const [form] = Form.useForm();

  useEffect(() => {
    if (open) {
      form.resetFields();
      if (editingUser) {
        form.setFieldsValue({
          userName: editingUser.userName ?? "",
          nickName: editingUser.nickName ?? "",
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
    } catch (error) {
      // Form validation failed
    }
  };

  return (
    <Modal
      title={
        <Space>
          <UserAddOutlined style={{ color: "#1677ff" }} />
          <span>{editingUser ? "编辑账号" : "新增账号"}</span>
        </Space>
      }
      open={open}
      onOk={handleOk}
      onCancel={onCancel}
      confirmLoading={loading}
      destroyOnHidden
      width={540}
    >
      <Form form={form} layout="vertical" preserve={false}>
        <Form.Item
          name="userName"
          label="用户名"
          rules={[{ required: true, message: "请输入用户名" }]}
        >
          <Input
            prefix={<UserOutlined />}
            placeholder="用于登录系统的账号"
            disabled={!!editingUser}
          />
        </Form.Item>

        <Form.Item
          name="password"
          label={editingUser ? "新密码 (留空表示维持原密码)" : "登录密码"}
          rules={[{ required: !editingUser, message: "请输入登录密码" }]}
          extra={editingUser ? "不修改密码请直接留空" : "请设置不少于6位的登录密码"}
        >
          <Input.Password prefix={<LockOutlined />} placeholder="请输入密码" />
        </Form.Item>

        <Form.Item name="nickName" label="显示昵称">
          <Input placeholder="用户显示的别名或昵称" />
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
          extra={
            editingUser?.builtin
              ? "默认超级用户不可修改角色"
              : currentUser?.isAdmin
                ? "可指定超级用户、普通用户或访客"
                : "仅超级用户可指定角色"
          }
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
          extra={
            !currentUser?.isAdmin
              ? "仅超级用户可修改账号状态"
              : editingUser?.id === currentUser?.id
                ? "不能禁用当前登录账号"
                : undefined
          }
        >
          <Select disabled={!currentUser?.isAdmin}>
            <Option value={0}>启用</Option>
            <Option value={1} disabled={editingUser?.id === currentUser?.id}>
              禁用
            </Option>
          </Select>
        </Form.Item>
      </Form>
    </Modal>
  );
}
