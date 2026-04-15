"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Table,
  Button,
  Tag,
  Space,
  Tooltip,
  Popconfirm,
  Modal,
  Form,
  Input,
  InputNumber,
  Upload,
  Select,
  Image as AntImage,
  Typography,
} from "antd";
import {
  LinkOutlined,
  EditOutlined,
  DeleteOutlined,
  PlusCircleOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";

const { Title, Text } = Typography;

export default function BannersPage() {
  const [banners, setBanners] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = useAppMessage();

  // Dialog visibilities
  const [addVisible, setAddVisible] = useState(false);
  const [editVisible, setEditVisible] = useState(false);
  const [bindVisible, setBindVisible] = useState(false);

  // Forms
  const [form] = Form.useForm();

  // Film Search
  const [filmOptions, setFilmOptions] = useState<any[]>([]);
  const [filmLoading, setFilmLoading] = useState(false);
  const [selectedFilm, setSelectedFilm] = useState<any>(null);

  // Current row references
  const [currentRow, setCurrentRow] = useState<any>(null);

  const fetchBanners = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/banner/list");
      if (resp.code === 0) {
        setBanners(resp.data || []);
      } else {
        message.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    fetchBanners();
  }, [fetchBanners]);

  const handleDelete = async (id: number) => {
    const resp = await ApiPost("/manage/banner/del", { id: String(id) });
    if (resp.code === 0) {
      message.success(resp.msg);
      fetchBanners();
    } else {
      message.error(resp.msg);
    }
  };

  const searchFilms = async (query: string) => {
    if (!query) return;
    setFilmLoading(true);
    try {
      const resp = await ApiGet("/searchFilm", { keyword: query, current: 0 });
      if (resp.code === 0 && resp.data?.list) {
        setFilmOptions(
          resp.data.list.map((f: any) => ({
            label: f.name,
            value: f.id,
            ...f,
          })),
        );
      } else {
        setFilmOptions([]);
      }
    } finally {
      setFilmLoading(false);
    }
  };

  const onFilmSelect = (val: number) => {
    const film = filmOptions.find((f) => f.id === val);
    setSelectedFilm(film);
  };

  const handleCustomUpload = async (options: any, fieldName: string) => {
    const formData = new FormData();
    formData.append("file", options.file);
    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        form.setFieldValue(fieldName, resp.data);
        message.success(resp.msg);
      } else {
        message.error(resp.msg);
      }
    } catch {
      message.error("上传失败");
    }
  };

  const handleAddSubmit = async () => {
    try {
      const values = await form.validateFields();
      if (selectedFilm) {
        values.mid = selectedFilm.id;
        values.name = selectedFilm.name || values.name;
        values.cName = selectedFilm.cName || values.cName;
        values.year = parseInt(selectedFilm.year || values.year || 0);
        values.remark = selectedFilm.remarks || values.remark;
      }
      const resp = await ApiPost("/manage/banner/add", values);
      if (resp.code === 0) {
        message.success(resp.msg);
        setAddVisible(false);
        fetchBanners();
      } else {
        message.error(resp.msg);
      }
    } catch {}
  };

  const handleEditSubmit = async () => {
    try {
      const values = await form.validateFields();
      const payload = { ...currentRow, ...values };
      const resp = await ApiPost("/manage/banner/update", payload);
      if (resp.code === 0) {
        message.success(resp.msg);
        setEditVisible(false);
        fetchBanners();
      } else {
        message.error(resp.msg);
      }
    } catch {}
  };

  const handleBindSubmit = async () => {
    if (!selectedFilm) {
      message.warning("请先搜索并选择一个影片");
      return;
    }
    const payload = { ...currentRow };
    payload.mid = selectedFilm.id;
    payload.name = selectedFilm.name;
    payload.cName = selectedFilm.cName;
    payload.picture = selectedFilm.picture;
    payload.year = parseInt(selectedFilm.year);
    payload.remark = selectedFilm.remarks;

    const resp = await ApiPost("/manage/banner/update", payload);
    if (resp.code === 0) {
      message.success(resp.msg);
      setBindVisible(false);
      fetchBanners();
    } else {
      message.error(resp.msg);
    }
  };

  const columns = [
    { title: "影片名称", dataIndex: "name", key: "name" },
    {
      title: "影片类型",
      dataIndex: "cName",
      key: "cName",
      render: (t: string) => <Tag color="warning">{t}</Tag>,
    },
    {
      title: "上映年份",
      dataIndex: "year",
      key: "year",
      render: (t: number) => <Tag color="warning">{t}</Tag>,
    },
    {
      title: "影片海报",
      dataIndex: "poster",
      key: "poster",
      render: (src: string) => (
        <AntImage src={src} height={50} style={{ objectFit: "contain" }} />
      ),
    },
    {
      title: "影片封面",
      dataIndex: "picture",
      key: "picture",
      render: (src: string) => (
        <AntImage src={src} height={50} style={{ objectFit: "cover" }} />
      ),
    },
    {
      title: "排序",
      dataIndex: "sort",
      key: "sort",
      render: (s: number) => <Tag>{s}</Tag>,
    },
    {
      title: "连载状态",
      dataIndex: "remark",
      key: "remark",
      render: (t: string) => (
        <Tag color={t.includes("更新") ? "processing" : "success"}>{t}</Tag>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "center" as const,
      width: 140,
      fixed: "right" as const,
      render: (_: any, record: any) => (
        <Space size={8}>
          <Tooltip title="绑定影片">
            <Button
              type="primary"
              shape="circle"
              size="small"
              icon={<LinkOutlined />}
              onClick={() => {
                setCurrentRow(record);
                setSelectedFilm(null);
                setFilmOptions([]);
                setBindVisible(true);
              }}
            />
          </Tooltip>
          <Tooltip title="修改内容">
            <Button
              type="primary"
              shape="circle"
              size="small"
              style={{ background: "#1890ff", borderColor: "#1890ff" }}
              icon={<EditOutlined />}
              onClick={() => {
                setCurrentRow(record);
                form.setFieldsValue(record);
                setEditVisible(true);
              }}
            />
          </Tooltip>
          <Popconfirm title="确认删除该轮播图？" onConfirm={() => handleDelete(record.id)}>
            <Tooltip title="删除">
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

  const formItems = (
    <>
      <Form.Item name="mid" label="影片ID">
        <InputNumber style={{ width: "100%" }} placeholder="关联的影片唯一ID" />
      </Form.Item>
      <Form.Item name="name" label="影片名称">
        <Input placeholder="影片名称" />
      </Form.Item>
      <Form.Item name="cName" label="影片分类">
        <Input placeholder="影片所属分类" />
      </Form.Item>
      <Form.Item label="影片海报">
        <Space.Compact style={{ width: "100%" }}>
          <Form.Item name="poster" noStyle>
            <Input placeholder="输入海报访问URL" />
          </Form.Item>
          <Upload
            showUploadList={false}
            customRequest={(o) => handleCustomUpload(o, "poster")}
          >
            <Button icon={<UploadOutlined />} style={{ marginLeft: 8 }}>
              上传
            </Button>
          </Upload>
        </Space.Compact>
      </Form.Item>
      <Form.Item label="影片封面">
        <Space.Compact style={{ width: "100%" }}>
          <Form.Item name="picture" noStyle>
            <Input placeholder="输入封面访问URL" />
          </Form.Item>
          <Upload
            showUploadList={false}
            customRequest={(o) => handleCustomUpload(o, "picture")}
          >
            <Button icon={<UploadOutlined />} style={{ marginLeft: 8 }}>
              上传
            </Button>
          </Upload>
        </Space.Compact>
      </Form.Item>
      <Form.Item name="remark" label="更新状态">
        <Input placeholder="例如: 已完结 / 更新至20集" />
      </Form.Item>
      <Form.Item name="year" label="上映年份">
        <InputNumber min={0} max={2100} style={{ width: "100%" }} />
      </Form.Item>
      <Form.Item name="sort" label="排序分值">
        <InputNumber min={-100} max={100} style={{ width: "100%" }} />
      </Form.Item>
    </>
  );

  return (
    <div style={{ background: "transparent" }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          marginBottom: 16,
        }}
      >
        <Title level={4}>首页横幅管理</Title>
        <Space>
          <Button
            type="primary"
            icon={<PlusCircleOutlined />}
            onClick={() => {
              form.resetFields();
              setAddVisible(true);
              setSelectedFilm(null);
            }}
          >
            添加海报
          </Button>
        </Space>
      </div>

      <Table
        dataSource={banners}
        columns={columns}
        rowKey="id"
        loading={loading}
        bordered
        scroll={{ x: "max-content" }}
      />

      {/* Add Modal */}
      <Modal
        title="添加海报"
        open={addVisible}
        onOk={handleAddSubmit}
        onCancel={() => setAddVisible(false)}
        width={720}
      >
        <Form form={form} labelCol={{ span: 4 }} wrapperCol={{ span: 18 }}>
          {formItems}
          <Form.Item label="绑定搜索">
            <Select
              showSearch
              placeholder="搜索关联影片(可选)"
              filterOption={false}
              onSearch={searchFilms}
              onChange={onFilmSelect}
              notFoundContent={filmLoading ? "搜索中..." : null}
              options={filmOptions}
            />
          </Form.Item>
          {selectedFilm && (
            <div style={{ paddingLeft: 120 }}>
              <Text type="secondary">
                选中: {selectedFilm.name} ({selectedFilm.cName},{" "}
                {selectedFilm.year})
              </Text>
            </div>
          )}
        </Form>
      </Modal>

      {/* Edit Modal */}
      <Modal
        title="修改海报信息"
        open={editVisible}
        onOk={handleEditSubmit}
        onCancel={() => setEditVisible(false)}
        width={720}
      >
        <Form form={form} labelCol={{ span: 4 }} wrapperCol={{ span: 18 }}>
          {formItems}
        </Form>
      </Modal>

      {/* Bind Modal */}
      <Modal
        title="绑定横幅到电影"
        open={bindVisible}
        onOk={handleBindSubmit}
        onCancel={() => setBindVisible(false)}
      >
        <Form layout="vertical">
          <Form.Item label="搜索影片">
            <Select
              showSearch
              placeholder="请输入影片名称搜索"
              filterOption={false}
              onSearch={searchFilms}
              onChange={onFilmSelect}
              notFoundContent={filmLoading ? "搜索中..." : null}
              options={filmOptions}
            />
          </Form.Item>
        </Form>
        {selectedFilm && (
          <div style={{ display: "flex", gap: 16, marginTop: 16 }}>
            <AntImage
              src={selectedFilm.picture}
              width={100}
              height={140}
              style={{ objectFit: "cover" }}
            />
            <div style={{ flex: 1 }}>
              <Title level={5}>{selectedFilm.name}</Title>
              <Text>
                {selectedFilm.cName} | {selectedFilm.year} | {selectedFilm.area}
              </Text>
              <br />
              <Text>导演: {selectedFilm.director}</Text>
              <br />
              <Text ellipsis style={{ maxWidth: 250 }}>
                主演: {selectedFilm.actor}
              </Text>
            </div>
          </div>
        )}
      </Modal>
    </div>
  );
}
