"use client";

import React, { useState, useEffect, useCallback } from "react";
import { Table, Tag, Switch, Button, Modal, Form, Tooltip, Space } from "antd";
import { EditOutlined } from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageHeader from "@/app/manage/components/page-header";
import styles from "./index.module.less";
import EditScheduleForm from "./components/edit-schedule-form";
import {
  getTaskDescription,
  getTaskScheduleText,
  getTaskTypeText,
  toTaskFormValues,
  type CronTask,
  buildCronSpec,
  buildTaskDescription,
} from "./utils/schedule";

export default function CronManagePageView() {
  const [taskList, setTaskList] = useState<CronTask[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = useAppMessage();

  const [editOpen, setEditOpen] = useState(false);
  const [form] = Form.useForm();
  const editModel = Form.useWatch("model", form);

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
      const task = resp.data as CronTask;
      form.setFieldsValue({
        ...toTaskFormValues(task),
      });
      setEditOpen(true);
    } else {
      message.error(resp.msg);
    }
  };

  const onEditFinish = async (values: any) => {
    const resp = await ApiPost("/manage/cron/update", {
      id: values.id,
      spec: buildCronSpec(values),
      remark: buildTaskDescription(values),
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
      fixed: "left",
      align: "center",
      render: (v) => <Tag color="purple">{v}</Tag>,
    },
    {
      title: "任务描述",
      dataIndex: "remark",
      align: "left",
      ellipsis: true,
      render: (_, record) => getTaskDescription(record),
    },
    {
      title: "任务类型",
      dataIndex: "model",
      align: "center",
      render: (v) => (
        <Tag color="cyan">{getTaskTypeText(v)}</Tag>
      ),
    },
    {
      title: "运行时间",
      key: "schedule",
      align: "center",
      render: (_, record) => <Tag>{getTaskScheduleText(record)}</Tag>,
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
      fixed: "right",
      render: (_, record) => (
        <Tooltip title="修改运行时间">
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

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="计划任务"
        description="统一维护后台自动更新、采集重试和清理类计划任务。"
      />

      <Table
        columns={columns}
        dataSource={taskList}
        rowKey="id"
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: "max-content" }}
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>任务列表</div>
            <Space size={[8, 8]} wrap className={styles.tableActions} />
          </div>
        )}
      />

      <Modal
        title="修改运行时间"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => form.validateFields().then(onEditFinish)}
        width={560}
      >
        <Form form={form} layout="vertical">
          <EditScheduleForm editModel={Number(editModel)} />
        </Form>
      </Modal>
    </div>
  );
}
