"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Modal,
  Form,
  Input,
  InputNumber,
  Upload,
  Card,
  Row,
  Col,
  Image as AntImage,
  Typography,
  Radio,
  Space,
  Flex,
  Button,
} from "antd";
import {
  UploadOutlined,
  PictureOutlined,
  VideoCameraAddOutlined,
  SwapOutlined,
} from "@ant-design/icons";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ImagePicker from "@/app/manage/components/image-picker";
import FilmPicker from "@/app/manage/components/film-picker";
import {
  IMAGE_UPLOAD_ACCEPT,
  isAllowedImageFile,
} from "@/lib/imageUpload";
import {
  BannerRecord,
  BannerFormValues,
  FilmOption,
  EditorMode,
  UploadFieldName,
  resolveEditablePicture,
  resolvePreviewPicture,
} from "./types";
import styles from "./index.module.less";

const { Title, Text } = Typography;

interface BannerModalProps {
  open: boolean;
  mode: EditorMode;
  currentRow: BannerRecord | null;
  onClose: () => void;
  onSuccess: () => void;
}

export default function BannerModal({
  open,
  mode,
  currentRow,
  onClose,
  onSuccess,
}: BannerModalProps) {
  const { message } = useAppMessage();
  const [form] = Form.useForm<BannerFormValues>();

  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [filmPickerOpen, setFilmPickerOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const watchedName = Form.useWatch("name", form);
  const watchedCName = Form.useWatch("cName", form);
  const watchedYear = Form.useWatch("year", form);
  const watchedPicture = Form.useWatch("picture", form);
  const watchedFollowPosterSource = Form.useWatch("followPosterSource", form);

  const resetState = useCallback(() => {
    form.resetFields();
    setSelectedFilm(null);
    setPickerOpen(false);
    setFilmPickerOpen(false);
  }, [form]);

  const buildFilmDefaults = (film: FilmOption): BannerFormValues => ({
    mid: film.id,
    name: film.name || "",
    cName: film.cName || "",
    year: parseInt(String(film.year || "0"), 10) || undefined,
    picture: "",
    followPosterSource: true,
  });

  const onFilmSelect = (film: FilmOption) => {
    setSelectedFilm(film);
    form.setFieldsValue({
      ...form.getFieldsValue(),
      ...buildFilmDefaults(film),
    });
    setFilmPickerOpen(false);
  };

  useEffect(() => {
    if (open) {
      if (mode === "edit" && currentRow) {
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
        if (currentRow.mid) {
          ApiGet("/filmPlayInfo", { id: currentRow.mid }).then((resp: any) => {
            if (resp.code === 0 && resp.data?.detail) {
              const detail = resp.data.detail;
              setSelectedFilm({
                id: detail.id || currentRow.mid,
                name: detail.name || currentRow.name,
                cName: detail.cName || currentRow.cName,
                year: detail.year || currentRow.year,
                remarks: detail.remarks || currentRow.remark,
                picture: detail.picture || currentRow.picture,
                area: detail.area,
                director: detail.director,
                actor: detail.actor,
                label: detail.name || currentRow.name,
                value: detail.id || currentRow.mid,
              });
            } else if (currentRow.name) {
              ApiGet("/searchFilm", { keyword: currentRow.name, current: 1 }).then((sResp: any) => {
                if (sResp.code === 0 && sResp.data?.list) {
                  const match = sResp.data.list.find((f: any) => String(f.id) === String(currentRow.mid));
                  if (match) {
                    setSelectedFilm({
                      ...match,
                      label: match.name,
                      value: match.id,
                    });
                  }
                }
              });
            }
          });
        }
      } else if (mode === "create") {
        form.setFieldsValue({
          followPosterSource: true,
          sort: 0,
        });
      }
    }
  }, [open, mode, currentRow, form]);

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
      onError?.(err);
    }
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
      name: String(values.name || "").trim(),
      cName: String(values.cName || "").trim(),
      year: values.year,
      poster: isCustom ? customPic : livePic,
      picture: isCustom ? customPic : livePic,
      pictureSlide: isCustom
        ? customPic
        : currentRow?.pictureSlide || selectedFilm?.picture || livePic,
      customPicture: customPic,
      sort: values.sort ?? 0,
      remark: currentRow?.remark || selectedFilm?.remarks || "",
      isCustomPic: isCustom,
    };
  };

  const handleSubmit = async () => {
    try {
      await form.validateFields();
      const values = form.getFieldsValue(true) as BannerFormValues;
      const payload = buildBannerPayload(values);
      if (mode === "create" && !payload.mid) {
        message.error("请先从片库选择要绑定的影片");
        return;
      }
      setSubmitting(true);
      const requestPath =
        mode === "create"
          ? "/manage/banner/add"
          : "/manage/banner/update";
      const requestPayload =
        mode === "create" ? payload : { ...currentRow, ...payload };
      const resp = await ApiPost(requestPath, requestPayload);
      if (resp.code === 0) {
        message.success(resp.msg);
        onSuccess();
        onClose();
      } else {
        message.error(resp.msg);
      }
    } catch (err: any) {
      if (err?.errorFields) {
        return;
      }
      message.error(err?.message || "提交轮播信息失败");
    } finally {
      setSubmitting(false);
    }
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

  return (
    <Modal
      title={mode === "create" ? "添加轮播" : "修改轮播信息"}
      open={open}
      onOk={handleSubmit}
      onCancel={onClose}
      confirmLoading={submitting}
      width={720}
      styles={{ body: { paddingBottom: 12 } }}
      destroyOnHidden
      afterClose={resetState}
    >
      <Form form={form} layout="vertical">
        <Space direction="vertical" size={16} style={{ width: "100%" }}>
          <Form.Item
            label="绑定影片"
            required
            tooltip="点击从片库搜索并选择影片，系统将自动填充影片ID、名称、分类及海报。"
          >
            {previewFilm?.name ? (
              <Card
                size="small"
                bordered
                className={styles.filmSelectedCard}
                styles={{ body: { padding: 12 } }}
              >
                <Flex gap={14} align="center" justify="space-between">
                  <Flex gap={12} align="center" style={{ minWidth: 0 }}>
                    <AntImage
                      src={liveFilmPic || FALLBACK_IMG}
                      width={48}
                      height={68}
                      style={{ objectFit: "cover", borderRadius: 6, background: "var(--public-surface-3)" }}
                      fallback={FALLBACK_IMG}
                      preview={false}
                    />
                    <Space direction="vertical" size={2} style={{ minWidth: 0 }}>
                      <Title level={5} style={{ margin: 0 }} ellipsis={{ tooltip: previewName }}>
                        {previewName}
                      </Title>
                      <Text type="secondary" style={{ fontSize: 12 }}>
                        {previewCategory} · {previewYear} · {previewArea}
                      </Text>
                      <Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: `导演: ${previewDirector} | 主演: ${previewActor}` }}>
                        状态: {previewRemark}
                      </Text>
                    </Space>
                  </Flex>
                  <Button
                    icon={<SwapOutlined />}
                    onClick={() => setFilmPickerOpen(true)}
                  >
                    更换影片
                  </Button>
                </Flex>
              </Card>
            ) : (
              <div
                className={styles.filmSelectTrigger}
                onClick={() => setFilmPickerOpen(true)}
              >
                <VideoCameraAddOutlined className={styles.triggerIcon} />
                <div className={styles.triggerText}>点击从片库搜索并选择影片</div>
                <div className={styles.triggerHint}>支持按片名模糊检索、排序筛选与海报图文预览</div>
              </div>
            )}
          </Form.Item>

          <Form.Item
            name="mid"
            label="影片ID"
            hidden
            rules={[{ required: true, message: "请先从片库选择要绑定的影片" }]}
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
      </Form>

      <FilmPicker
        open={filmPickerOpen}
        selectedMid={selectedFilm?.id || currentRow?.mid}
        onCancel={() => setFilmPickerOpen(false)}
        onSelect={onFilmSelect}
      />

      <ImagePicker
        open={pickerOpen}
        onCancel={() => setPickerOpen(false)}
        onSelect={(link) => {
          form.setFieldValue("picture", link);
          setPickerOpen(false);
        }}
      />
    </Modal>
  );
}
