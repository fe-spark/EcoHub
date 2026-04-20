"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Table,
  Button,
  Tag,
  Space,
  Flex,
  Tooltip,
  Popconfirm,
  Modal,
  Form,
  Input,
  InputNumber,
  Upload,
  Select,
  Card,
  Collapse,
  Row,
  Col,
  Image as AntImage,
  Typography,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  PlusCircleOutlined,
  UploadOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";

const { Title, Text } = Typography;

type BannerRecord = {
  id: string;
  mid: number;
  name: string;
  cName: string;
  year?: number;
  remark?: string;
  poster: string;
  picture: string;
  pictureSlide?: string;
  sort?: number;
};

type BannerFormValues = {
  mid?: number;
  name: string;
  cName: string;
  year?: number;
  remark?: string;
  poster: string;
  picture: string;
  pictureSlide: string;
  sort?: number;
};

type FilmOption = {
  id: number;
  name?: string;
  cName?: string;
  year?: string | number;
  remarks?: string;
  poster?: string;
  picture?: string;
  pictureSlide?: string;
  area?: string;
  director?: string;
  actor?: string;
  label: string;
  value: number;
};

type EditorMode = "create" | "edit";
type UploadFieldName = "poster" | "picture" | "pictureSlide";

export default function BannersPageView() {
  const [banners, setBanners] = useState<BannerRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = useAppMessage();

  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");

  const [form] = Form.useForm<BannerFormValues>();

  const [filmOptions, setFilmOptions] = useState<FilmOption[]>([]);
  const [filmLoading, setFilmLoading] = useState(false);
  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);

  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);
  const watchedName = Form.useWatch("name", form);
  const watchedCName = Form.useWatch("cName", form);
  const watchedYear = Form.useWatch("year", form);
  const watchedRemark = Form.useWatch("remark", form);
  const watchedPoster = Form.useWatch("poster", form);
  const watchedPicture = Form.useWatch("picture", form);
  const watchedPictureSlide = Form.useWatch("pictureSlide", form);

  const fetchBanners = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/banner/list");
      if (resp.code === 0) {
        setBanners((resp.data || []) as BannerRecord[]);
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

  const handleDelete = async (id: string) => {
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
          resp.data.list.map((f: FilmOption) => ({
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

  const buildFilmDefaults = (film: FilmOption): BannerFormValues => ({
    mid: film.id,
    name: film.name || "",
    cName: film.cName || "",
    year: parseInt(String(film.year || "0"), 10) || undefined,
    remark: film.remarks || "",
    poster: film.poster || film.picture || film.pictureSlide || "",
    picture: film.picture || film.poster || film.pictureSlide || "",
    pictureSlide: film.pictureSlide || film.poster || film.picture || "",
  });

  const onFilmSelect = (val: number | string) => {
    const film = filmOptions.find((f) => String(f.id) === String(val));
    if (!film) {
      message.warning("未找到对应影片，已跳过自动填充");
      return;
    }

    setSelectedFilm(film);
    form.setFieldsValue({
      ...form.getFieldsValue(),
      ...buildFilmDefaults(film),
    });
  };

  const resetEditorState = () => {
    form.resetFields();
    setSelectedFilm(null);
    setFilmOptions([]);
    setCurrentRow(null);
  };

  const openCreateEditor = () => {
    resetEditorState();
    setEditorMode("create");
    setEditorVisible(true);
  };

  const openEditEditor = (record: BannerRecord) => {
    resetEditorState();
    setEditorMode("edit");
    setCurrentRow(record);
    form.setFieldsValue({
      mid: record.mid,
      name: record.name,
      cName: record.cName,
      year: record.year,
      remark: record.remark,
      poster: record.poster,
      picture: record.picture,
      pictureSlide: record.pictureSlide,
      sort: record.sort,
    });
    setEditorVisible(true);
  };

  const closeEditor = () => {
    setEditorVisible(false);
  };

const buildBannerPayload = (values: BannerFormValues): BannerFormValues => ({
    mid: values.mid,
    name: values.name.trim(),
    cName: values.cName.trim(),
    year: values.year,
    remark: values.remark?.trim() || "",
    poster: values.poster.trim(),
    picture: values.picture.trim(),
    pictureSlide: values.pictureSlide.trim(),
    sort: values.sort ?? 0,
  });

  const previewFilm = selectedFilm || currentRow;
  const previewName = watchedName || previewFilm?.name || "未选择影片";
  const previewCategory = watchedCName || previewFilm?.cName || "未分类";
  const previewYear = watchedYear || previewFilm?.year || "未知年份";
  const previewBaseRemark = selectedFilm?.remarks || currentRow?.remark || "";
  const previewRemark = watchedRemark || previewBaseRemark || "暂无状态";
  const previewArea = selectedFilm?.area || "未知地区";
  const previewDirector = selectedFilm?.director || "暂无";
  const previewActor = selectedFilm?.actor || "暂无";
  const previewPoster = watchedPoster || previewFilm?.poster || previewFilm?.picture || "";
  const previewPicture = watchedPicture || previewFilm?.picture || previewFilm?.poster || "";
  const previewPictureSlide =
    watchedPictureSlide || previewFilm?.pictureSlide || previewFilm?.poster || previewFilm?.picture || "";

  const handleCustomUpload = async (options: any, fieldName: UploadFieldName) => {
    const { file, onSuccess, onError } = options;
    const formData = new FormData();
    formData.append("file", file);
    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        form.setFieldValue(fieldName, resp.data);
        message.success(resp.msg);
        onSuccess?.(resp.data);
      } else {
        message.error(resp.msg);
        onError?.(new Error(resp.msg));
      }
    } catch (err) {
      message.error("上传失败");
      onError?.(err);
    }
  };

  const handleSubmit = async () => {
    try {
      await form.validateFields();
      const values = form.getFieldsValue(true) as BannerFormValues;
      const payload = buildBannerPayload(values);
      if (editorMode === "create" && !payload.mid) {
        message.error("请先搜索并选择要绑定的影片");
        return;
      }
      const requestPath = editorMode === "create" ? "/manage/banner/add" : "/manage/banner/update";
      const requestPayload = editorMode === "create" ? payload : { ...currentRow, ...payload };
      const resp = await ApiPost(requestPath, requestPayload);
      if (resp.code === 0) {
        message.success(resp.msg);
        closeEditor();
        fetchBanners();
      } else {
        message.error(resp.msg);
      }
    } catch {}
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
      title: "横版幻灯图",
      dataIndex: "pictureSlide",
      key: "pictureSlide",
      render: (src: string) =>
        src ? <AntImage src={src} width={120} height={50} style={{ objectFit: "cover" }} /> : "-",
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
      width: 100,
      fixed: "right" as const,
      render: (_: unknown, record: BannerRecord) => (
        <Space size={8}>
          <Tooltip title="修改内容">
            <Button
              type="primary"
              shape="circle"
              size="small"
              style={{ background: "#1890ff", borderColor: "#1890ff" }}
              icon={<EditOutlined />}
              onClick={() => openEditEditor(record)}
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
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <Form.Item label="搜索影片">
        <Select
          showSearch
          placeholder="输入影片名称后选择，自动填充剩余字段"
          filterOption={false}
          onSearch={searchFilms}
          onChange={onFilmSelect}
          notFoundContent={filmLoading ? "搜索中..." : null}
          options={filmOptions}
        />
      </Form.Item>
      {previewFilm && (
        <Card size="small" bordered style={{ borderRadius: 12 }}>
          <Flex gap={16} align="flex-start">
              <div style={{ flexShrink: 0 }}>
                <AntImage
                  src={previewPicture || previewPoster}
                  width={96}
                  height={132}
                  style={{ objectFit: "cover", borderRadius: 8 }}
                />
              </div>
            <Space direction="vertical" size={4} style={{ width: "100%", minWidth: 0 }}>
              <Title level={5} style={{ margin: 0 }}>
                {previewName}
              </Title>
              <Text type="secondary">
                {previewCategory} | {previewYear} | {previewArea}
              </Text>
              <Text type="secondary">导演: {previewDirector}</Text>
              <Text ellipsis={{ tooltip: previewActor }} type="secondary">
                主演: {previewActor}
              </Text>
              <Text type="secondary">当前状态: {previewRemark}</Text>
            </Space>
          </Flex>
        </Card>
      )}
      <Form.Item
        name="mid"
        label="影片ID"
        hidden
        rules={[{ required: true, message: "请先搜索并选择要绑定的影片" }]}
      >
        <InputNumber style={{ width: "100%" }} />
      </Form.Item>
      <Row gutter={12}>
        <Col span={12}>
          <Form.Item name="name" label="影片名称" rules={[{ required: true, message: "请输入影片名称" }]}> 
            <Input placeholder="首页横幅展示名称" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="cName" label="影片分类" rules={[{ required: true, message: "请输入影片分类" }]}> 
            <Input placeholder="首页横幅展示分类" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="year" label="上映年份" rules={[{ required: true, message: "请输入上映年份" }]}> 
            <InputNumber min={0} max={2100} style={{ width: "100%" }} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="sort" label="排序分值">
            <InputNumber min={-100} max={100} style={{ width: "100%" }} />
          </Form.Item>
        </Col>
      </Row>
      <Collapse
        size="small"
        items={[
          {
            key: "basic-fields",
            label: "补充信息",
            forceRender: true,
            children: (
              <Row gutter={12}>
                <Col span={24}>
                  <Form.Item name="remark" label="更新状态">
                    <Input placeholder="例如: 已完结 / 更新至20集" />
                  </Form.Item>
                </Col>
              </Row>
            ),
          },
        ]}
      />
      <Form.Item label="横幅背景图" extra="默认会使用影片图片自动填充，可继续替换为更适合首页横幅的图片。">
        <Space.Compact style={{ width: "100%" }}>
          <Form.Item name="poster" noStyle rules={[{ required: true, message: "请上传或填写横幅背景图" }]}> 
            <Input placeholder="输入横幅背景图访问 URL" />
          </Form.Item>
          <Upload showUploadList={false} customRequest={(o) => handleCustomUpload(o, "poster")}>
            <Button icon={<UploadOutlined />} style={{ marginLeft: 8 }}>
              上传
            </Button>
          </Upload>
        </Space.Compact>
      </Form.Item>
      {previewPoster && (
        <Card size="small" title="横幅背景图预览" style={{ borderRadius: 12 }}>
          <AntImage src={previewPoster} width="100%" height={180} style={{ objectFit: "cover", borderRadius: 8 }} />
        </Card>
      )}
      <Form.Item label="横版幻灯图" extra="首页 banner 背景优先使用该字段；若采集站未提供，可手动上传或填写。">
        <Space.Compact style={{ width: "100%" }}>
          <Form.Item
            name="pictureSlide"
            noStyle
            rules={[{ required: true, message: "请上传或填写横版幻灯图" }]}
          >
            <Input placeholder="输入横版幻灯图访问 URL" />
          </Form.Item>
          <Upload showUploadList={false} customRequest={(o) => handleCustomUpload(o, "pictureSlide")}>
            <Button icon={<UploadOutlined />} style={{ marginLeft: 8 }}>
              上传
            </Button>
          </Upload>
        </Space.Compact>
      </Form.Item>
      {previewPictureSlide && (
        <Card size="small" title="横版幻灯图预览" style={{ borderRadius: 12 }}>
          <AntImage
            src={previewPictureSlide}
            width="100%"
            height={180}
            style={{ objectFit: "cover", borderRadius: 8 }}
          />
        </Card>
      )}
      <Form.Item label="封面图" extra="用于补充展示的封面图，默认自动填充，可按需要替换。">
        <Space.Compact style={{ width: "100%" }}>
          <Form.Item name="picture" noStyle rules={[{ required: true, message: "请上传或填写封面图" }]}> 
            <Input placeholder="输入封面访问 URL" />
          </Form.Item>
          <Upload showUploadList={false} customRequest={(o) => handleCustomUpload(o, "picture")}>
            <Button icon={<UploadOutlined />} style={{ marginLeft: 8 }}>
              上传
            </Button>
          </Upload>
        </Space.Compact>
      </Form.Item>
      {previewPicture && (
        <Card size="small" title="封面图预览" style={{ borderRadius: 12 }}>
          <AntImage src={previewPicture} width={160} height={220} style={{ objectFit: "cover", borderRadius: 8 }} />
        </Card>
      )}
    </Space>
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
            onClick={openCreateEditor}
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

      <Modal
        title={editorMode === "create" ? "添加海报" : "修改海报信息"}
        open={editorVisible}
        onOk={handleSubmit}
        onCancel={closeEditor}
        width={720}
        destroyOnHidden
        afterClose={resetEditorState}
      >
        <Form form={form} layout="vertical" preserve={false}>
          {formItems}
        </Form>
      </Modal>
    </div>
  );
}
