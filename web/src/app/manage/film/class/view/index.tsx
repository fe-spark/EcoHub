"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Table,
  Tag,
  Switch,
  Button,
  Space,
  Modal,
  Input,
  Form,
  Popconfirm,
  Tooltip,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./index.module.less";

interface FilmClass {
  id: number;
  pid: number;
  name: string;
  show: boolean;
  children?: FilmClass[];
}

export default function FilmClassPageView() {
  const [classTree, setClassTree] = useState<FilmClass[]>([]);
  const [loading, setLoading] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [editForm] = Form.useForm();
  const [editingItem, setEditingItem] = useState<FilmClass | null>(null);
  const { message } = useAppMessage();

  const getFilmClassTree = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/film/class/tree");
      if (resp.code === 0) {
        setClassTree(resp.data.children || []);
      }
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    getFilmClassTree();
  }, [getFilmClassTree]);

  const changeClassState = async (id: number, show: boolean) => {
    const resp = await ApiPost("/manage/film/class/update", { id, show });
    if (resp.code === 0) {
      message.success(resp.msg);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const delClass = async (id: number) => {
    const resp = await ApiPost("/manage/film/class/del", { id: String(id) });
    if (resp.code === 0) {
      message.success(resp.msg);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const openEditDialog = async (id: number) => {
    const resp = await ApiGet("/manage/film/class/find", { id });
    if (resp.code === 0) {
      setEditingItem(resp.data);
      editForm.setFieldsValue(resp.data);
      setEditOpen(true);
    }
  };

  const onEditFinish = async (values: any) => {
    const resp = await ApiPost("/manage/film/class/update", {
      id: editingItem?.id,
      name: values.name,
      show: values.show,
    });
    if (resp.code === 0) {
      message.success(resp.msg);
      setEditOpen(false);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const resetFilmClass = async () => {
    const resp = await ApiPost("/manage/spider/class/cover", {});
    if (resp.code === 0) {
      message.success(resp.msg);
      getFilmClassTree();
    } else {
      message.error(resp.msg);
    }
  };

  const columns: ColumnsType<FilmClass> = [
    {
      title: "分类名称",
      dataIndex: "name",
      key: "name",
      render: (v, record) => (
        <Tag color={record.pid === 0 ? "success" : "warning"}>{v}</Tag>
      ),
    },
    {
      title: "是否展示",
      dataIndex: "show",
      align: "center",
      render: (v, record) => (
        <Switch
          checked={v}
          onChange={(checked) => changeClassState(record.id, checked)}
          checkedChildren="展示"
          unCheckedChildren="隐藏"
        />
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      width: 120,
      fixed: "right",
      render: (_, record) => (
        <Space size={8}>
          <Tooltip title="编辑分类">
            <Button
              type="primary"
              shape="circle"
              size="small"
              icon={<EditOutlined />}
              onClick={() => openEditDialog(record.id)}
            />
          </Tooltip>
          <Popconfirm title="确认删除此分类？" onConfirm={() => delClass(record.id)}>
            <Tooltip title="删除分类">
              <Button
                type="primary"
                danger
                shape="circle"
                size="small"
                icon={<DeleteOutlined />}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];

  return (
    <div className={styles.container}>
      <Table
        columns={columns}
        dataSource={classTree}
        rowKey="id"
        loading={loading}
        bordered
        size="middle"
        pagination={false}
        scroll={{ x: "max-content", y: "calc(100vh - 280px)" }}
      />

      <div className={styles.toolbar}>
        <Button
          type="primary"
          icon={<ReloadOutlined />}
          style={{ background: "#9b49e7", borderColor: "#9b49e7" }}
          onClick={resetFilmClass}
        >
          重置分类信息
        </Button>
      </div>

      <Modal
        title="更新分类信息"
        open={editOpen}
        onCancel={() => setEditOpen(false)}
        onOk={() => editForm.submit()}
        width={480}
      >
        <Form form={editForm} layout="vertical" onFinish={onEditFinish}>
          <Form.Item
            label="分类名称"
            name="name"
            rules={[{ required: true, message: "请输入分类名称" }]}
          >
            <Input placeholder="分类名称,用于首页导航展示" />
          </Form.Item>

          <Form.Item label="分类层级">
            <Tag color={editingItem?.pid === 0 ? "success" : "warning"}>
              {editingItem?.pid === 0 ? "一级分类" : "二级分类"}
            </Tag>
          </Form.Item>

          <Form.Item label="是否展示" name="show" valuePropName="checked">
            <Switch checkedChildren="展示" unCheckedChildren="隐藏" />
          </Form.Item>

          {editingItem?.children && editingItem.children.length > 0 && (
            <Form.Item label="拓展分类">
              <div className={styles.classSub}>
                {editingItem.children.map((c) => (
                  <Tag key={c.id} color="orange" className={styles.subTag}>
                    {c.name}
                  </Tag>
                ))}
              </div>
            </Form.Item>
          )}
        </Form>
      </Modal>
    </div>
  );
}
