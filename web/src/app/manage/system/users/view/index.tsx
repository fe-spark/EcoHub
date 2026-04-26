"use client";

import React, { useState, useEffect } from "react";
import {
  Table,
  Button,
  Space,
  Tooltip,
  Modal,
  Form,
  Input,
  Select,
  message,
  Card,
  Tag,
  Popconfirm,
  Avatar,
} from "antd";
import {
  UserOutlined,
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  LockOutlined,
  MailOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import ManagePageShell from "../../../components/page-shell";

const { Option } = Select;

export default function UsersPageView() {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState([]);
  const [total, setTotal] = useState(0);
  const [current, setCurrent] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [searchText, setSearchText] = useState("");
  const [currentUser, setCurrentUser] = useState<any>(null);

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<any>(null);
  const [form] = Form.useForm();

  const fetchCurrentUser = React.useCallback(async () => {
    try {
      const resp = await ApiGet("/manage/user/info");
      if (resp.code === 0) {
        setCurrentUser(resp.data);
      }
    } catch (error) {
      console.error("Fetch current user info error:", error);
    }
  }, []);

  const fetchData = React.useCallback(
    async (page = current, size = pageSize, name = searchText) => {
      setLoading(true);
      try {
        const resp = await ApiGet("/manage/user/list", {
          current: page,
          pageSize: size,
          userName: name,
        });
        if (resp.code === 0) {
          setData(resp.data.list || []);
          setTotal(resp.data.total || 0);
        }
      } catch (error) {
        console.error("Fetch users error:", error);
      } finally {
        setLoading(false);
      }
    },
    [current, pageSize, searchText],
  );

  useEffect(() => {
    fetchCurrentUser();
    fetchData();
  }, [fetchCurrentUser, fetchData]);

  const handleSearch = (value: string) => {
    setSearchText(value);
    setCurrent(1);
    fetchData(1, pageSize, value);
  };

  const handleAdd = () => {
    setEditingUser(null);
    form.resetFields();
    setIsModalOpen(true);
  };

  const handleEdit = (record: any) => {
    setEditingUser(record);
    form.setFieldsValue({
      ...record,
      password: "",
    });
    setIsModalOpen(true);
  };

  const handleDelete = async (id: number) => {
    try {
      const resp = await ApiPost("/manage/user/del", { id: String(id) });
      if (resp.code === 0) {
        message.success("删除成功");
        fetchData();
      } else {
        message.error(resp.msg || "删除失败");
      }
    } catch (error) {
      console.error("Delete user error:", error);
    }
  };

  const handleModalOk = async () => {
    try {
      const values = await form.validateFields();
      setLoading(true);

      const url = editingUser ? "/manage/user/update" : "/manage/user/add";
      const payload = editingUser ? { ...values, id: editingUser.id } : values;

      const resp = await ApiPost(url, payload);
      if (resp.code === 0) {
        message.success(editingUser ? "更新成功" : "添加成功");
        setIsModalOpen(false);
        fetchData();
      } else {
        message.error(resp.msg || "操作失败");
      }
    } catch (error) {
      console.error("Save user error:", error);
    } finally {
      setLoading(false);
    }
  };

  const columns = [
    {
      title: "用户名",
      dataIndex: "userName",
      key: "userName",
      render: (text: string, record: any) => (
        <Space>
          <Avatar
            src={record.avatar === "empty" ? null : record.avatar}
            icon={<UserOutlined />}
          />
          <span style={{ fontWeight: 500 }}>{text}</span>
          {record.isAdmin && <Tag color="gold">超级管理员</Tag>}
          {record.isVisitor && <Tag color="blue">访客只读</Tag>}
        </Space>
      ),
    },
    {
      title: "昵称",
      dataIndex: "nickName",
      key: "nickName",
    },
    {
      title: "邮箱",
      dataIndex: "email",
      key: "email",
    },
    {
      title: "状态",
      dataIndex: "status",
      key: "status",
      render: (status: number) => (
        <Tag color={status === 0 ? "success" : "error"}>
          {status === 0 ? "正常" : "禁用"}
        </Tag>
      ),
    },
    {
      title: "操作",
      key: "action",
      render: (_: any, record: any) => (
        <Space size={8}>
          <Tooltip
            title={
              !currentUser?.canWrite
                ? "访客账号仅允许查看"
                : record.isAdmin && !currentUser?.isAdmin
                  ? "权限不足，仅超级管理员可修改超级管理员信息"
                  : "编辑用户"
            }
          >
            <Button
              type="primary"
              shape="circle"
              size="small"
              icon={<EditOutlined />}
              disabled={
                !currentUser?.canWrite ||
                (record.isAdmin && !currentUser?.isAdmin)
              }
              onClick={() => handleEdit(record)}
            />
          </Tooltip>
          {currentUser?.isAdmin && !record.isAdmin && !record.isVisitor && (
            <Popconfirm
              title="确定要删除这个用户吗？"
              onConfirm={() => handleDelete(record.id)}
              okText="确定"
              cancelText="取消"
            >
              <Tooltip title="删除用户">
                <Button
                  type="primary"
                  danger
                  shape="circle"
                  size="small"
                  icon={<DeleteOutlined />}
                />
              </Tooltip>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <ManagePageShell
      eyebrow="系统设置"
      title="账号管理"
      description="统一维护后台账号、权限身份和基础状态，支持快速搜索与编辑。"
      actions={
        <Space>
          <Input.Search
            placeholder="搜索用户名"
            onSearch={handleSearch}
            style={{ width: 250 }}
            allowClear
          />
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={handleAdd}
            disabled={!currentUser?.canWrite}
          >
            新增用户
          </Button>
        </Space>
      }
      panelClassName=""
      panelless
    >
      <Table
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        pagination={{
          current,
          pageSize,
          total,
          onChange: (page, size) => {
            setCurrent(page);
            setPageSize(size);
            fetchData(page, size);
          },
          showSizeChanger: true,
          showTotal: (total) => `共 ${total} 条记录`,
        }}
      />

      <Modal
        title={editingUser ? "编辑用户" : "新增用户"}
        open={isModalOpen}
        onOk={handleModalOk}
        onCancel={() => setIsModalOpen(false)}
        confirmLoading={loading}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" preserve={false}>
          <Form.Item
            name="userName"
            label="用户名"
            rules={[{ required: true, message: "请输入用户名" }]}
          >
            <Input
              prefix={<UserOutlined />}
              placeholder="用于登录的账号"
              disabled={!!editingUser}
            />
          </Form.Item>

          <Form.Item
            name="password"
            label={editingUser ? "新密码 (留空则不修改)" : "密码"}
            rules={[{ required: !editingUser, message: "请输入密码" }]}
          >
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="请输入密码"
            />
          </Form.Item>

          <Form.Item name="nickName" label="昵称">
            <Input placeholder="用户显示的名称" />
          </Form.Item>

          <Form.Item
            name="email"
            label="邮箱"
            rules={[{ type: "email", message: "请输入有效的邮箱地址" }]}
          >
            <Input prefix={<MailOutlined />} placeholder="用户邮箱" />
          </Form.Item>

          <Form.Item name="gender" label="性别" initialValue={0}>
            <Select>
              <Option value={0}>保密</Option>
              <Option value={1}>男</Option>
              <Option value={2}>女</Option>
            </Select>
          </Form.Item>

          <Form.Item name="status" label="状态" initialValue={0}>
            <Select disabled={editingUser?.isAdmin || editingUser?.isVisitor}>
              <Option value={0}>正常</Option>
              <Option value={1}>禁用</Option>
            </Select>
          </Form.Item>
        </Form>
      </Modal>
    </ManagePageShell>
  );
}
