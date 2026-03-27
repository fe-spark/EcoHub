"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Table,
  Tag,
  Switch,
  Button,
  Modal,
  Input,
  Form,
  Tooltip,
} from "antd";
import { EditOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/api";
import styles from "./page.module.less";
import { useAppMessage } from "@/lib/useAppMessage";

interface CronTask {
  id: string;
  cid: string;
  spec: string;
  remark: string;
  model: number;
  ids: string[];
  time: number;
  state: boolean;
  preV?: string;
  next?: string;
}

export default function CronManagePage() {
  const [taskList, setTaskList] = useState<CronTask[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = useAppMessage();

  const [editOpen, setEditOpen] = useState(false);
  const [form] = Form.useForm();

  const getTaskList = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/cron/list");
      if (resp.code === 0) {
        setTaskList(resp.data || []);
      } else {
        setTaskList([]);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    getTaskList();
  }, [getTaskList]);

  const changeTaskState = async (id: string, state: boolean) => {
    const resp = await ApiPost("/manage/cron/change", { id, state });
    if (resp.code === 0) {
      message.success(resp.msg);
      getTaskList();
    } else {
      message.error(resp.msg);
    }
  };

  const openEditDialog = async (id: string) => {
    form.resetFields();
    const resp = await ApiGet("/manage/cron/find", { id });
    if (resp.code === 0) {
      form.setFieldsValue(resp.data);
      setEditOpen(true);
    } else {
      message.error(resp.msg);
    }
  };

  const onEditFinish = async (values: any) => {
    const resp = await ApiPost("/manage/cron/update", {
      id: values.id,
      spec: values.spec,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setEditOpen(false);
      getTaskList();
    } else {
      message.error(resp.msg);
    }
  };

  const columns: ColumnsType<CronTask> = [
    {
      title: "任务ID",
      dataIndex: "id",
      width: 200,
      render: (v) => <Tag color="purple">{v}</Tag>,
    },
    {
      title: "任务描述",
      dataIndex: "remark",
      ellipsis: true,
    },
    {
      title: "任务类型",
      dataIndex: "model",
      align: "center",
      render: (v) => (
        <Tag color="cyan">
          {v === 0 ? "自动更新" : v === 1 ? "自定义更新" : v === 2 ? "采集重试" : "孤儿清理"}
        </Tag>
      ),
    },
    {
      title: "是否启用",
      dataIndex: "state",
      align: "center",
      render: (v, record) => (
        <Switch
          checked={v}
          onChange={(checked) => changeTaskState(record.id, checked)}
          checkedChildren="启用"
          unCheckedChildren="禁用"
        />
      ),
    },
    {
      title: "上次执行时间",
      dataIndex: "preV",
      align: "center",
      render: (v) => <Tag color="success">{v || "-"}</Tag>,
    },
    {
      title: "下次执行时间",
      dataIndex: "next",
      align: "center",
      render: (v) => <Tag color="warning">{v || "-"}</Tag>,
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      width: 70,
      render: (_, record) => (
        <Tooltip title="修改时间">
          <Button
            type="primary"
            shape="circle"
            size="small"
            icon={<EditOutlined />}
            onClick={() => openEditDialog(record.id)}
          />
        </Tooltip>
      ),
    },
  ];

  const editFormItems = () => (
    <>
      <Form.Item name="id" hidden>
        <Input />
      </Form.Item>
      <Form.Item label="任务标识" name="id">
        <Input disabled />
      </Form.Item>
      <Form.Item label="执行时间" name="spec" rules={[{ required: true, message: "请输入Cron表达式" }]}>
        <Input placeholder="例如: 0 */20 * * * ? (每20分钟执行一次)" />
      </Form.Item>
    </>
  );

  return (
    <div>
      <Table
        columns={columns}
        dataSource={taskList}
        rowKey="id"
        loading={loading}
        bordered
        size="middle"
        pagination={false}
        scroll={{ x: 900 }}
      />

      <Modal
        title="修改定时任务时间"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => form.validateFields().then(onEditFinish)}
        okButtonProps={{
          style: { background: "#9b49e7", borderColor: "#9b49e7" },
        }}
      >
        <Form form={form} layout="vertical">
          {editFormItems()}
        </Form>
      </Modal>
    </div>
  );
}
