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
  Row,
  Col,
  Image as AntImage,
  Typography,
  Radio,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  PlusCircleOutlined,
  UploadOutlined,
  PictureOutlined,
} from "@ant-design/icons";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ManagePageHeader from "@/app/manage/components/page-header";
import ImagePicker from "@/app/manage/components/image-picker";
import {
  IMAGE_UPLOAD_ACCEPT,
  isAllowedImageFile,
} from "@/lib/imageUpload";
import styles from "./index.module.less";

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
  customPicture?: string;
  sort?: number;
  isCustomPic?: boolean;
};

type BannerFormValues = {
  mid?: number;
  name: string;
  cName: string;
  year?: number;
  picture?: string;
  customPicture?: string;
  sort?: number;
  isCustomPic?: boolean;
  followPosterSource?: boolean;
};

type FilmOption = {
  id: number;
  name?: string;
  cName?: string;
  year?: string | number;
  remarks?: string;
  picture?: string;
  area?: string;
  director?: string;
  actor?: string;
  label: string;
  value: number;
};

type EditorMode = "create" | "edit";
type UploadFieldName = "picture";

function resolveEditablePicture(record?: Partial<BannerRecord> | null): string {
  if (!record) {
    return "";
  }

  return record.customPicture || (record.isCustomPic ? record.picture : "") || "";
}

function resolvePreviewPicture(
  record?: BannerRecord | FilmOption | null,
): string {
  if (!record) {
    return "";
  }

  const primaryPicture = record.picture || "";
  if (primaryPicture) {
    return primaryPicture;
  }

  if ("poster" in record && record.poster) {
    return record.poster;
  }

  if ("pictureSlide" in record && record.pictureSlide) {
    return record.pictureSlide;
  }

  return "";
}

export default function BannersPageView() {
  const [banners, setBanners] = useState<BannerRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");

  const [form] = Form.useForm<BannerFormValues>();

  const [filmOptions, setFilmOptions] = useState<FilmOption[]>([]);
  const [filmLoading, setFilmLoading] = useState(false);
  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);

  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const watchedName = Form.useWatch("name", form);
  const watchedCName = Form.useWatch("cName", form);
  const watchedYear = Form.useWatch("year", form);
  const watchedPicture = Form.useWatch("picture", form);
  const watchedFollowPosterSource = Form.useWatch("followPosterSource", form);

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
    void fetchBanners();
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
    picture: "",
    followPosterSource: true,
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
    setPickerOpen(false);
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
    setEditorVisible(true);
  };

  useEffect(() => {
    if (editorVisible) {
      if (editorMode === "edit" && currentRow) {
        const isCustom = currentRow.isCustomPic === true;
        form.setFieldsValue({
          mid: currentRow.mid,
          name: currentRow.name,
          cName: currentRow.cName,
          year: currentRow.year,
          followPosterSource: !isCustom,
          picture: isCustom ? resolveEditablePicture(currentRow) : "",
          sort: currentRow.sort ?? 0,
        });
        if (currentRow.mid && currentRow.name) {
          ApiGet("/searchFilm", { keyword: currentRow.name, current: 0 }).then((resp: any) => {
            if (resp.code === 0 && resp.data?.list) {
              const match = resp.data.list.find((f: any) => String(f.id) === String(currentRow.mid));
              if (match) {
                setSelectedFilm(match);
              }
            }
          });
        }
      } else if (editorMode === "create") {
        form.setFieldsValue({
          followPosterSource: true,
          sort: 0,
        });
      }
    }
  }, [editorVisible, editorMode, currentRow, form]);

  const closeEditor = () => {
    setEditorVisible(false);
  };

  const buildBannerPayload = (values: BannerFormValues): BannerRecord => {
    const isCustom = values.followPosterSource === false;
    const customPic = isCustom ? (values.picture || "").trim() : "";
    const livePic =
      selectedFilm?.picture ||
      (currentRow && !currentRow.isCustomPic ? currentRow.picture : "") ||
      "";

    return {
      id: currentRow?.id || "",
      mid: values.mid || currentRow?.mid || 0,
      name: values.name.trim(),
      cName: values.cName.trim(),
      year: values.year,
      poster: isCustom ? customPic : livePic,
      picture: isCustom ? customPic : livePic,
      pictureSlide: isCustom
        ? customPic
        : currentRow?.pictureSlide || selectedFilm?.picture || livePic,
      customPicture: customPic,
      sort: values.sort ?? 0,
      remark: "",
      isCustomPic: isCustom,
    };
  };

  const previewFilm = selectedFilm || currentRow;
  const previewName = watchedName || previewFilm?.name || "未选择影片";
  const previewCategory = watchedCName || previewFilm?.cName || "未分类";
  const previewYear = watchedYear || previewFilm?.year || "未知年份";
  const previewRemark =
    selectedFilm?.remarks || currentRow?.remark || "前台按片库实时状态展示";
  const previewArea = selectedFilm?.area || "未知地区";
  const previewDirector = selectedFilm?.director || "暂无";
  const previewActor = selectedFilm?.actor || "暂无";
  const liveFilmPic =
    selectedFilm?.picture ||
    (currentRow && !currentRow.isCustomPic ? currentRow.picture : "") ||
    "";
  const previewPicture = watchedFollowPosterSource
    ? liveFilmPic
    : (watchedPicture || liveFilmPic || resolvePreviewPicture(previewFilm));

  const handleCustomUpload = async (
    options: any,
    fieldName: UploadFieldName,
  ) => {
    const { file, onSuccess, onError } = options;
    if (!isAllowedImageFile(file)) {
      message.error("仅支持上传 JPG/JPEG/PNG/WebP/ICO 格式的图片");
      onError?.(new Error("unsupported image type"));
      return;
    }
    const formData = new FormData();
    formData.append("file", file);
    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        const fullUrl =
          typeof window !== "undefined" && String(resp.data).startsWith("/")
            ? `${window.location.origin}${resp.data}`
            : resp.data;
        form.setFieldValue(fieldName, fullUrl);
        message.success(resp.msg);
        onSuccess?.(fullUrl);
      } else {
        message.error(resp.msg);
        onError?.(new Error(resp.msg));
      }
    } catch (err) {
      // 拦截器已统一提示，避免重复弹窗
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
      const requestPath =
        editorMode === "create"
          ? "/manage/banner/add"
          : "/manage/banner/update";
      const requestPayload =
        editorMode === "create" ? payload : { ...currentRow, ...payload };
      const resp = await ApiPost(requestPath, requestPayload);
      if (resp.code === 0) {
        message.success(resp.msg);
        closeEditor();
        fetchBanners();
      } else {
        message.error(resp.msg);
      }
    } catch (err: any) {
      if (err?.errorFields) {
        return;
      }
      message.error(err?.message || "提交轮播信息失败");
    }
  };

  const columns = [
    {
      title: "ID",
      dataIndex: "id",
      key: "id",
      width: 80,
      fixed: "left" as const,
      align: "center" as const,
      render: (value: string) => <Tag color="purple">{value}</Tag>,
    },
    { title: "影片名称", dataIndex: "name", key: "name", align: "left" as const },
    {
      title: "影片类型",
      dataIndex: "cName",
      key: "cName",
      align: "center" as const,
      render: (t: string) => <Tag color="warning">{t}</Tag>,
    },
    {
      title: "上映年份",
      dataIndex: "year",
      key: "year",
      align: "center" as const,
      render: (t: number) => <Tag color="warning">{t}</Tag>,
    },
    {
      title: "影片封面",
      dataIndex: "picture",
      key: "picture",
      align: "center" as const,
      render: (src: string, record: BannerRecord) => (
        <div style={{ display: "flex", flexDirection: "column", alignItems: "center", gap: 4 }}>
          <AntImage 
            src={src || FALLBACK_IMG} 
            width={48}
            height={64} 
            style={{ objectFit: "cover", background: "var(--public-surface-3)", borderRadius: 4 }} 
            fallback={FALLBACK_IMG}
            placeholder={<div style={{ width: '100%', height: '100%', background: 'var(--public-surface-3)', borderRadius: 4 }} />}
          />
          {record.isCustomPic ? (
            <Tooltip title="自定义封面（锁定保护）">
              <Tag color="warning" style={{ margin: 0, fontSize: 11, lineHeight: "16px", padding: "0 4px", cursor: "help" }}>
                自定义锁定
              </Tag>
            </Tooltip>
          ) : (
            <Tooltip title="跟随海报源（自动同步）">
              <Tag color="processing" style={{ margin: 0, fontSize: 11, lineHeight: "16px", padding: "0 4px", cursor: "help" }}>
                海报源联动
              </Tag>
            </Tooltip>
          )}
        </div>
      ),
    },
    {
      title: "排序",
      dataIndex: "sort",
      key: "sort",
      align: "center" as const,
      render: (s: number) => <Tag>{s}</Tag>,
    },
    {
      title: "操作",
      key: "action",
      align: "center" as const,
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
              disabled={!canWrite}
              onClick={() => openEditEditor(record)}
            />
          </Tooltip>
          <Popconfirm
            title="确认删除该轮播图？"
            onConfirm={() => handleDelete(record.id)}
          >
            <Tooltip title="删除">
              <Button
                type="primary"
                danger
                shape="circle"
                size="small"
                icon={<DeleteOutlined />}
                disabled={!canWrite}
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
          value={selectedFilm?.id || currentRow?.mid || undefined}
          notFoundContent={filmLoading ? "搜索中..." : null}
          options={
            filmOptions.length > 0
              ? filmOptions
              : currentRow?.mid
                ? [
                    {
                      id: currentRow.mid,
                      label: `${currentRow.name} (${currentRow.cName || "影片"})`,
                      value: currentRow.mid,
                      name: currentRow.name,
                      cName: currentRow.cName,
                      year: currentRow.year,
                      picture: resolveEditablePicture(currentRow),
                    },
                  ]
                : []
          }
        />
      </Form.Item>
      {previewFilm && (
        <Card size="small" bordered style={{ borderRadius: 12 }}>
          <Flex gap={16} align="flex-start">
            <div style={{ flexShrink: 0 }}>
              <AntImage
                src={previewPicture || FALLBACK_IMG}
                width={96}
                height={132}
                style={{ objectFit: "cover", borderRadius: 8, background: "var(--public-surface-3)" }}
                fallback={FALLBACK_IMG}
                placeholder={<div style={{ width: '100%', height: '100%', background: 'var(--public-surface-3)', borderRadius: 8 }} />}
              />
            </div>
            <Space
              direction="vertical"
              size={4}
              style={{ width: "100%", minWidth: 0 }}
            >
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
          <Form.Item
            name="name"
            label="影片名称"
            rules={[{ required: true, message: "请输入影片名称" }]}
          >
            <Input placeholder="封面卡片展示名称" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item
            name="cName"
            label="影片分类"
            rules={[{ required: true, message: "请输入影片分类" }]}
          >
            <Input placeholder="封面卡片展示分类" />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item
            name="year"
            label="上映年份"
            rules={[{ required: true, message: "请输入上映年份" }]}
          >
            <InputNumber min={0} max={2100} style={{ width: "100%" }} />
          </Form.Item>
        </Col>
        <Col span={12}>
          <Form.Item name="sort" label="排序分值">
            <InputNumber min={-100} max={100} style={{ width: "100%" }} />
          </Form.Item>
        </Col>
      </Row>
      <Form.Item
        label="是否跟随海报源"
        name="followPosterSource"
        tooltip="开启自动同步海报源；关闭可自定义并锁定图片。"
        initialValue={true}
      >
        <Radio.Group
          buttonStyle="solid"
          options={[
            { label: "是（跟随海报源）", value: true },
            { label: "否（自定义封面）", value: false },
          ]}
        />
      </Form.Item>

      {watchedFollowPosterSource ? (
        <Form.Item label="影片封面">
          <div
            style={{
              padding: "8px 12px",
              background: "rgba(255, 255, 255, 0.04)",
              borderRadius: 6,
              border: "1px dashed var(--ant-color-border)",
              display: "flex",
              alignItems: "center",
              justifyContent: "space-between",
              minHeight: 32,
            }}
          >
            <Text style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>
              已开启跟随海报源，将自动同步最新封面与幻灯图。
            </Text>
            {previewPicture && (
              <AntImage
                src={previewPicture || FALLBACK_IMG}
                height={32}
                style={{ borderRadius: 4, objectFit: "cover" }}
                fallback={FALLBACK_IMG}
              />
            )}
          </div>
        </Form.Item>
      ) : (
        <Form.Item
          label="封面图片地址"
          tooltip="自定义封面，锁定后不被海报源或采集覆盖。"
        >
          <Space.Compact style={{ width: "100%" }}>
            <Form.Item
              name="picture"
              noStyle
              rules={[{ required: true, message: "请输入自定义封面图片地址或上传/选图" }]}
            >
              <Input placeholder="输入封面访问 URL" />
            </Form.Item>
            <Upload
              showUploadList={false}
              accept={IMAGE_UPLOAD_ACCEPT}
              customRequest={(o) => handleCustomUpload(o, "picture")}
            >
              <Button icon={<UploadOutlined />} style={{ marginLeft: 8 }}>
                上传
              </Button>
            </Upload>
            <Button
              icon={<PictureOutlined />}
              onClick={() => setPickerOpen(true)}
            >
              选图
            </Button>
          </Space.Compact>
        </Form.Item>
      )}
      {previewPicture && (
        <Card size="small" title="影片封面预览" style={{ borderRadius: 12 }}>
          <AntImage
            src={previewPicture || FALLBACK_IMG}
            width={160}
            height={220}
            style={{ objectFit: "cover", borderRadius: 8, background: "var(--public-surface-3)" }}
            fallback={FALLBACK_IMG}
            placeholder={<div style={{ width: '100%', height: '100%', background: 'var(--public-surface-3)', borderRadius: 8 }} />}
          />
        </Card>
      )}
    </Space>
  );

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="首页轮播"
        description="维护首页和推荐位所用的轮播内容，统一管理排序、轮播图与影片绑定信息。"
      />

      <Table
        bordered
        dataSource={banners}
        columns={columns}
        rowKey="id"
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: "max-content" }}
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>轮播列表</div>
            <div className={styles.tableActions}>
              <Button
                type="primary"
                icon={<PlusCircleOutlined />}
                onClick={openCreateEditor}
              >
                添加轮播
              </Button>
            </div>
          </div>
        )}
      />



      <Modal
        title={editorMode === "create" ? "添加轮播" : "修改轮播信息"}
        open={editorVisible}
        onOk={handleSubmit}
        onCancel={closeEditor}
        width={720}
        styles={{ body: { paddingBottom: 12 } }}
        destroyOnHidden
        afterClose={resetEditorState}
      >
        <Form form={form} layout="vertical">
          {formItems}
        </Form>
        <ImagePicker
          open={pickerOpen}
          onCancel={() => setPickerOpen(false)}
          onSelect={(link) => {
            form.setFieldValue("picture", link);
            setPickerOpen(false);
          }}
        />
      </Modal>
    </div>
  );
}
