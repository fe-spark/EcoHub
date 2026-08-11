"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Table,
  Button,
  Space,
  Tooltip,
  Pagination,
  Modal,
  Form,
  Input,
  Select,
  Tag,
  Popconfirm,
  Avatar,
  Typography,
} from "antd";
import {
  UserOutlined,
  PlusOutlined,
  EditOutlined,
  DeleteOutlined,
  LockOutlined,
  MailOutlined,
  CrownOutlined,
  EyeOutlined,
  CheckCircleOutlined,
  StopOutlined,
  UserAddOutlined,
  SearchOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";

const { Option } = Select;
const { Text } = Typography;

interface UsersPageViewProps {
  /** 嵌入系统设置 Tabs 时隐藏独立页头 */
  embedded?: boolean;
}

export default function UsersPageView({ embedded = false }: UsersPageViewProps) {
  const [loading, setLoading] = useState(false);
  const [data, setData] = useState<any[]>([]);
  const [total, setTotal] = useState(0);
  const [current, setCurrent] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [inputValue, setInputValue] = useState("");
  const [searchKeyword, setSearchKeyword] = useState("");
  const [roleFilter, setRoleFilter] = useState(-1);
  const [statusFilter, setStatusFilter] = useState(-1);
  const [currentUser, setCurrentUser] = useState<any>(null);

  const [isModalOpen, setIsModalOpen] = useState(false);
  const [editingUser, setEditingUser] = useState<any>(null);
  const [form] = Form.useForm();
  const { message } = useAppMessage();

  const fetchCurrentUser = useCallback(async () => {
    try {
      const resp = await ApiGet("/manage/user/info");
      if (resp.code === 0) {
        setCurrentUser(resp.data);
      }
    } catch (error) {
      console.error("Fetch current user info error:", error);
    }
  }, []);

  const fetchData = useCallback(
    async (
      page = current,
      size = pageSize,
      name = searchKeyword,
      role = roleFilter,
      status = statusFilter,
    ) => {
      setLoading(true);
      try {
        const resp = await ApiGet("/manage/user/list", {
          current: page,
          pageSize: size,
          userName: name,
          role,
          status,
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
    [current, pageSize, searchKeyword, roleFilter, statusFilter],
  );

  useEffect(() => {
    fetchCurrentUser();
    fetchData();
  }, [fetchCurrentUser, fetchData]);

  const handleSearch = (value: string) => {
    const trimmed = value.trim();
    setInputValue(trimmed);
    setSearchKeyword(trimmed);
    setCurrent(1);
    void fetchData(1, pageSize, trimmed, roleFilter, statusFilter);
  };

  const handleReset = () => {
    setInputValue("");
    setSearchKeyword("");
    setRoleFilter(-1);
    setStatusFilter(-1);
    setCurrent(1);
    void fetchData(1, pageSize, "", -1, -1);
  };

  const handleInputChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const val = e.target.value;
    setInputValue(val);
    if (val === "" && searchKeyword !== "") {
      setSearchKeyword("");
      setCurrent(1);
    }
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
      role: record.role ?? (record.isAdmin ? 1 : record.isVisitor ? 2 : 0),
    });
    setIsModalOpen(true);
  };

  const handleDelete = async (id: number) => {
    try {
      const resp = await ApiPost("/manage/user/del", { id: String(id) });
      if (resp.code === 0) {
        message.success("账号删除成功");
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
        message.success(editingUser ? "账号信息更新成功" : "新增账号成功");
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

  const columns: ColumnsType<any> = [
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      width: 80,
      fixed: "left",
      align: "center",
      render: (value: number) => <Tag color="purple">{value}</Tag>,
    },
    {
      title: "用户账户",
      dataIndex: "userName",
      key: "userName",
      align: "left",
      render: (text: string, record: any) => (
        <div className={styles.userCell}>
          <Avatar
            src={record.avatar && record.avatar !== "empty" ? record.avatar : null}
            icon={<UserOutlined />}
            style={{ backgroundColor: record.isAdmin ? "#f5222d" : "#1677ff" }}
          />
          <div className={styles.userInfo}>
            <span className={styles.userName}>{text}</span>
            {record.nickName ? (
              <span className={styles.userMeta}>{record.nickName}</span>
            ) : null}
          </div>
        </div>
      ),
    },
    {
      title: "身份角色",
      dataIndex: "roleName",
      key: "roleName",
      align: "center",
      render: (_: string, record: any) => {
        if (record.isAdmin) {
          return (
            <Tag color="gold" icon={<CrownOutlined />}>
              超级管理员
            </Tag>
          );
        }
        if (record.isVisitor) {
          return (
            <Tag color="blue" icon={<EyeOutlined />}>
              访客只读
            </Tag>
          );
        }
        return (
          <Tag color="cyan" icon={<UserOutlined />}>
            普通用户
          </Tag>
        );
      },
    },
    {
      title: "邮箱地址",
      dataIndex: "email",
      key: "email",
      align: "left",
      render: (text: string) => text || <Text type="secondary">-</Text>,
    },
    {
      title: "账号状态",
      dataIndex: "status",
      key: "status",
      align: "center",
      render: (status: number) => (
        <Tag
          color={status === 0 ? "success" : "error"}
          icon={status === 0 ? <CheckCircleOutlined /> : <StopOutlined />}
        >
          {status === 0 ? "正常" : "禁用"}
        </Tag>
      ),
    },
    {
      title: "操作",
      key: "action",
      fixed: "right",
      align: "center",
      render: (_: any, record: any) => (
        <Space size={4}>
          <Tooltip
            title={
              !currentUser?.canWrite
                ? "访客账号仅允许查看"
                : record.isAdmin && !currentUser?.isAdmin
                  ? "权限不足，仅超级管理员可修改超级管理员信息"
                  : "编辑账号"
            }
          >
            <Button
              type="link"
              size="small"
              icon={<EditOutlined />}
              disabled={
                !currentUser?.canWrite ||
                (record.isAdmin && !currentUser?.isAdmin)
              }
              onClick={() => handleEdit(record)}
            >
              编辑
            </Button>
          </Tooltip>
          {currentUser?.canWrite && currentUser?.isAdmin && !record.isAdmin && !record.isVisitor && (
            <Popconfirm
              title="确定要删除该用户账号吗？"
              description="删除后无法撤销，该账号将失去所有后台访问权限。"
              onConfirm={() => handleDelete(record.id)}
              okText="确定"
              cancelText="取消"
              okButtonProps={{ danger: true }}
            >
              <Tooltip title="删除账号">
                <Button type="link" danger size="small" icon={<DeleteOutlined />}>
                  删除
                </Button>
              </Tooltip>
            </Popconfirm>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className={styles.pageStack}>
      {embedded ? null : (
        <ManagePageHeader
          title="账号管理"
          description="统一维护后台账号、权限身份和基础状态，支持快速搜索与编辑。"
        />
      )}

      <div className={styles.filterBar}>
        <div className={styles.filterLeft}>
          <Space size={8}>
            <Select
              placeholder="全部角色"
              value={roleFilter}
              onChange={(value) => setRoleFilter(value)}
              options={[
                { value: -1, label: "全部角色" },
                { value: 0, label: "普通用户" },
                { value: 1, label: "超级管理员" },
                { value: 2, label: "访客" },
              ]}
              style={{ width: 130 }}
            />
            <Select
              placeholder="全部状态"
              value={statusFilter}
              onChange={(value) => setStatusFilter(value)}
              options={[
                { value: -1, label: "全部状态" },
                { value: 0, label: "正常" },
                { value: 1, label: "禁用" },
              ]}
              style={{ width: 120 }}
            />
            <Input
              placeholder="搜索用户名"
              value={inputValue}
              onChange={handleInputChange}
              onPressEnter={() => handleSearch(inputValue)}
              className={styles.searchInput}
              allowClear
            />
            <Button
              type="primary"
              icon={<SearchOutlined />}
              onClick={() => handleSearch(inputValue)}
            >
              搜索
            </Button>
            <Button
              icon={<ReloadOutlined />}
              onClick={handleReset}
            >
              重置
            </Button>
          </Space>
        </div>
      </div>

      <Table
        bordered
        columns={columns}
        dataSource={data}
        rowKey="id"
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: "max-content" }}
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>账号列表</div>
            <div className={styles.tableActions}>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={handleAdd}
                disabled={!currentUser?.canWrite}
              >
                新增账号
              </Button>
            </div>
          </div>
        )}
        footer={() => (
          <div className={styles.pagination}>
            <Pagination
              current={current}
              pageSize={pageSize}
              total={total}
              showSizeChanger
              pageSizeOptions={[10, 20, 50, 100]}
              showTotal={(total) => `共 ${total} 条`}
              onChange={(page, size) => {
                setCurrent(page);
                setPageSize(size);
                fetchData(page, size);
              }}
            />
          </div>
        )}
      />

      <Modal
        title={
          <Space>
            <UserAddOutlined style={{ color: "#1677ff" }} />
            <span>{editingUser ? "编辑账号" : "新增账号"}</span>
          </Space>
        }
        open={isModalOpen}
        onOk={handleModalOk}
        onCancel={() => setIsModalOpen(false)}
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
            <Input.Password
              prefix={<LockOutlined />}
              placeholder="请输入密码"
            />
          </Form.Item>

          <Form.Item name="nickName" label="显示昵称">
            <Input placeholder="用户显示的别名或昵称" />
          </Form.Item>

          <Form.Item
            name="email"
            label="电子邮箱"
            rules={[{ type: "email", message: "请输入有效的电子邮箱地址" }]}
          >
            <Input prefix={<MailOutlined />} placeholder="例如 user@example.com" />
          </Form.Item>

          <Form.Item name="gender" label="性别" initialValue={0}>
            <Select>
              <Option value={0}>保密</Option>
              <Option value={1}>男</Option>
              <Option value={2}>女</Option>
            </Select>
          </Form.Item>

          <Form.Item name="role" label="身份角色" initialValue={0}>
            <Select
              disabled={
                editingUser?.id === 1 ||
                editingUser?.userName === "visitor" ||
                !currentUser?.isAdmin
              }
            >
              <Option value={0}>普通用户</Option>
              <Option value={1}>超级管理员</Option>
              <Option value={2}>访客只读</Option>
            </Select>
          </Form.Item>

          <Form.Item name="status" label="账号状态" initialValue={0}>
            <Select disabled={editingUser?.isAdmin || editingUser?.isVisitor}>
              <Option value={0}>正常 (允许登录和访问)</Option>
              <Option value={1}>禁用 (禁止登录并拉黑)</Option>
            </Select>
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
