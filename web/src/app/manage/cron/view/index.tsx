"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import { Table, Tag, Switch, Button, Modal, Form, Tooltip, Space, Popconfirm } from "antd";
import { EditOutlined, ThunderboltOutlined, StopOutlined } from "@ant-design/icons";
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

const RUNNING_POLL_INTERVAL_MS = 5000;

export default function CronManagePageView() {
  const [taskList, setTaskList] = useState<CronTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [runningId, setRunningId] = useState<string | null>(null);
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

  const hasRunningTask = useMemo(() => taskList.some((t) => t.running), [taskList]);

  useEffect(() => {
    if (!hasRunningTask) return;
    const timer = setInterval(getTaskList, RUNNING_POLL_INTERVAL_MS);
    return () => clearInterval(timer);
  }, [hasRunningTask, getTaskList]);

  const changeTaskState = async (id: string, state: boolean) => {
    const resp = await ApiPost("/manage/cron/change", { id, state });
    if (resp.code === 0) {
      message.success(resp.msg);
      getTaskList();
    } else {
      message.error(resp.msg);
    }
  };

  const runTaskOnce = async (id: string) => {
    setRunningId(id);
    try {
      const resp = await ApiPost("/manage/cron/run", { id });
      if (resp.code === 0) {
        message.success(resp.msg);
        getTaskList();
      } else {
        message.error(resp.msg);
      }
    } finally {
      setRunningId(null);
    }
  };

  const terminateTask = async (id: string) => {
    const resp = await ApiPost("/manage/cron/change", { id, state: false });
    if (resp.code === 0) {
      message.success("已终止调度，当前执行将继续完成");
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
      render: (v, record) => {
        if (record.running) {
          return (
            <Tag color={record.state ? "processing" : "warning"}>
              {record.state ? "执行中" : "已终止·等待完成"}
            </Tag>
          );
        }
        return (
          <Switch
            checked={v}
            onChange={(checked) => changeTaskState(record.id, checked)}
            checkedChildren="启用"
            unCheckedChildren="禁用"
          />
        );
      },
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
      render: (_, record) => {
        if (record.running) {
          return (
            <Popconfirm
              title="终止调度？"
              description="将停止未来定时调度，当前正在执行的任务会继续跑完。"
              onConfirm={() => terminateTask(record.id)}
              disabled={!record.state}
              okText="终止"
              cancelText="取消"
              okButtonProps={{ danger: true }}
            >
              <Button
                danger
                size="small"
                icon={<StopOutlined />}
                disabled={!record.state}
              >
                {record.state ? "终止" : "已终止"}
              </Button>
            </Popconfirm>
          );
        }
        const isRunning = runningId === record.id;
        const runDisabled = !record.state || isRunning;
        return (
          <Space size={8}>
            <Tooltip title={record.state ? "立即执行一次" : "请先启用任务"}>
              <span>
                <Popconfirm
                  title="立即执行该定时任务？"
                  description="将立即触发一次执行，结果请查看运行日志。"
                  onConfirm={() => runTaskOnce(record.id)}
                  okText="执行"
                  cancelText="取消"
                >
                  <Button
                    type="primary"
                    shape="circle"
                    size="small"
                    icon={<ThunderboltOutlined />}
                    disabled={runDisabled}
                    loading={isRunning}
                  />
                </Popconfirm>
              </span>
            </Tooltip>
            <Tooltip title="修改运行时间">
              <Button
                shape="circle"
                size="small"
                icon={<EditOutlined />}
                onClick={() => openEditDialog(record.id)}
              />
            </Tooltip>
          </Space>
        );
      },
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
