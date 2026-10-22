"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Drawer,
  Form,
  Input,
  InputNumber,
  Upload,
  Row,
  Col,
  Image as AntImage,
  Typography,
  Space,
  Flex,
  Button,
  Tag,
  Switch,
} from "antd";
import {
  UploadOutlined,
  PictureOutlined,
  VideoCameraAddOutlined,
  SwapOutlined,
  ThunderboltOutlined,
  UndoOutlined,
} from "@ant-design/icons";

import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ImagePicker from "@/app/manage/components/image-picker";
import FilmPicker from "@/app/manage/components/film-picker";
import TmdbModal from "@/app/manage/film/components/tmdb-modal";
import { IMAGE_UPLOAD_ACCEPT, isAllowedImageFile } from "@/lib/imageUpload";
import {
  BannerRecord,
  BannerFormValues,
  BannerSnapshot,
  FilmOption,
  EditorMode,
  UploadFieldName,
} from "./types";
import {
  buildBannerPayload,
  computeTmdbPrefillUpdates,
  fetchFilmOption,
  computeBannerPreview,
  uploadBannerFile,
  initBannerFormValues,
} from "./banner-modal-utils";
import styles from "./index.module.less";

const { Title, Text } = Typography;

interface BannerDrawerProps {
  open: boolean;
  mode: EditorMode;
  currentRow: BannerRecord | null;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export default function BannerDrawer({
  open,
  mode,
  currentRow,
  tmdbEnabled = false,
  onClose,
  onSuccess,
}: BannerDrawerProps) {
  const { message } = useAppMessage();
  const [form] = Form.useForm<BannerFormValues>();

  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerTarget, setPickerTarget] = useState<UploadFieldName>("picture");
  const [filmPickerOpen, setFilmPickerOpen] = useState(false);
  const [tmdbOpen, setTmdbOpen] = useState(false);
  const [tmdbSnapshot, setTmdbSnapshot] = useState<BannerSnapshot | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const watchedPicture = Form.useWatch("picture", form);
  const watchedPictureSlide = Form.useWatch("pictureSlide", form);
  const watchedIsCustomPic = Form.useWatch("isCustomPic", form);

  const resetState = useCallback(() => {
    form.resetFields();
    setSelectedFilm(null);
    setPickerOpen(false);
    setFilmPickerOpen(false);
    setTmdbOpen(false);
    setTmdbSnapshot(null);
  }, [form]);

  const loadFilmDetail = useCallback(async (mid: number) => {
    const option = await fetchFilmOption(mid);
    if (option) {
      setSelectedFilm(option);
      return option;
    }
    return null;
  }, []);

  const onFilmSelect = async (film: FilmOption) => {
    setSelectedFilm(film);
    setTmdbSnapshot(null);
    const parsedYear = parseInt(String(film.year || "0"), 10) || undefined;
    form.setFieldsValue({
      mid: film.id,
      name: film.name || "",
      cName: film.cName || "",
      year: parsedYear,
      picture: "",
      pictureSlide: "",
      isCustomPic: false,
    });
    setFilmPickerOpen(false);
    if (film.id) {
      const fullDetail = await loadFilmDetail(film.id);
      if (fullDetail) {
        setSelectedFilm(fullDetail);
        form.setFieldsValue({
          mid: fullDetail.id,
          name: fullDetail.name || film.name || "",
          cName: fullDetail.cName || film.cName || "",
          year: parseInt(String(fullDetail.year || film.year || "0"), 10) || undefined,
          picture: "",
          pictureSlide: "",
          isCustomPic: false,
        });
      }
    }
  };

  useEffect(() => {
    if (!open) return;
    setTmdbSnapshot(null);
    setSelectedFilm(null);
    form.resetFields();

    const initialVals = initBannerFormValues(mode, currentRow);
    form.setFieldsValue(initialVals);

    if (mode === "edit" && currentRow?.mid) {
      loadFilmDetail(currentRow.mid).catch(() => {});
    }
  }, [open, mode, currentRow, form, loadFilmDetail]);

  const handleCustomUpload = async (options: any, fieldName: UploadFieldName) => {
    const { file, onSuccess, onError } = options;
    if (!isAllowedImageFile(file)) {
      message.error("格式不支持");
      onError?.(new Error("格式不支持"));
      return;
    }
    try {
      const url = await uploadBannerFile(file);
      form.setFieldValue(fieldName, url);
      message.success("上传成功");
      onSuccess?.(url);
    } catch (err: any) {
      message.error(err?.message || "上传失败");
      onError?.(err);
    }
  };

  const isReplacingFilm = mode === "edit" && selectedFilm !== null && selectedFilm.id !== currentRow?.mid;
  const activeFilm = selectedFilm || (mode === "edit" && currentRow ? currentRow : null);

  const preview = computeBannerPreview({
    currentRow,
    selectedFilm,
    isReplacingFilm,
    watchedPicture,
    watchedPictureSlide,
    watchedIsCustomPic,
  });

  const handleCustomSwitchChange = (checked: boolean) => {
    form.setFieldValue("isCustomPic", checked);
    if (checked) {
      if (!form.getFieldValue("picture") && preview.liveFilmPic) {
        form.setFieldValue("picture", preview.liveFilmPic);
      }
      if (
        !form.getFieldValue("pictureSlide") &&
        preview.liveFilmSlide &&
        preview.liveFilmSlide !== preview.liveFilmPic
      ) {
        form.setFieldValue("pictureSlide", preview.liveFilmSlide);
      }
    }
  };

  const currentMid = preview.currentMid || form.getFieldValue("mid");

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    if (!tmdbSnapshot) {
      setTmdbSnapshot({
        formValues: form.getFieldsValue(true),
        selectedFilm: selectedFilm ? { ...selectedFilm } : null,
      });
    }

    const currentValues = form.getFieldsValue();
    const updates = computeTmdbPrefillUpdates(data, fields, currentValues, selectedFilm);
    form.setFieldsValue(updates);
    setTmdbOpen(false);
  };

  const handleUndoTmdb = () => {
    if (!tmdbSnapshot) return;
    form.resetFields();
    form.setFieldsValue(tmdbSnapshot.formValues);
    setSelectedFilm(tmdbSnapshot.selectedFilm);
    setTmdbSnapshot(null);
    message.info("已撤销");
  };

  const handleSubmit = async () => {
    try {
      await form.validateFields();
      const values = form.getFieldsValue(true) as BannerFormValues;
      const payload = buildBannerPayload({
        values,
        currentRow,
        selectedFilm,
        isReplacingFilm,
      });
      if (!payload.mid) {
        message.error("请选择影片");
        return;
      }
      setSubmitting(true);
      const resp = await ApiPost("/manage/banner/save", payload);
      if (resp.code === 0) {
        message.success(resp.msg || "保存成功");
        onSuccess();
        onClose();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } catch (err: any) {
      if (err?.errorFields) return;
      message.error(err?.message || "提交失败");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <>
      <Drawer
        open={open}
        onClose={onClose}
        width={720}
        destroyOnClose
        afterOpenChange={(visible) => {
          if (!visible) resetState();
        }}
        title={
          <Space size={10} align="center">
            <PictureOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
            <span style={{ fontSize: 16, fontWeight: 600 }}>{mode === "create" ? "新增轮播" : "编辑轮播"}</span>
            {currentRow?.mid && (
              <Tag color="purple" style={{ borderRadius: 4, margin: 0, fontWeight: 600 }}>
                MID: #{currentRow.mid}
              </Tag>
            )}
          </Space>
        }
        extra={
          <Space>
            <Button onClick={onClose} disabled={submitting}>
              取消
            </Button>
            <Button
              type="primary"
              loading={submitting}
              onClick={handleSubmit}
            >
              {mode === "create" ? "创建" : "保存"}
            </Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <div className={styles.drawerStack}>
            {/* 1. 绑定影片 */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <span>绑定影片</span>
              </div>
              {activeFilm?.name ? (
                <div className={styles.filmSelectedCard}>
                  <Flex gap={14} align="center" justify="space-between">
                    <Flex gap={12} align="center" style={{ minWidth: 0 }}>
                      <AntImage
                        src={preview.liveFilmPic || FALLBACK_IMG}
                        width={46}
                        height={64}
                        style={{ objectFit: "cover", borderRadius: 6, background: "var(--public-surface-3)" }}
                        fallback={FALLBACK_IMG}
                        preview={false}
                      />
                      {preview.liveFilmSlide && preview.liveFilmSlide !== preview.liveFilmPic ? (
                        <AntImage
                          src={preview.liveFilmSlide}
                          width={96}
                          height={54}
                          style={{ objectFit: "cover", borderRadius: 6, background: "var(--public-surface-3)" }}
                          fallback={FALLBACK_IMG}
                          preview={false}
                        />
                      ) : null}
                      <Space direction="vertical" size={2} style={{ minWidth: 0 }}>
                        <Flex align="center" gap={6}>
                          <Title level={5} style={{ margin: 0 }} ellipsis={{ tooltip: preview.previewName }}>
                            {preview.previewName}
                          </Title>
                        </Flex>
                        <Text type="secondary" style={{ fontSize: 12 }}>
                          {preview.previewCategory} · {preview.previewYear} · {preview.previewArea}
                        </Text>
                        <Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: `状态: ${preview.previewRemark}` }}>
                          状态: {preview.previewRemark}
                        </Text>
                      </Space>
                    </Flex>
                    <Space size={8}>
                      {tmdbSnapshot && (
                        <Button size="middle" icon={<UndoOutlined />} onClick={handleUndoTmdb}>
                          撤销
                        </Button>
                      )}
                      {tmdbEnabled && (
                        <Button
                          size="middle"
                          icon={<ThunderboltOutlined />}
                          onClick={() => setTmdbOpen(true)}
                          disabled={!currentMid}
                          style={{ color: "#722ed1", borderColor: "#d3adf7" }}
                        >
                          TMDB 刮削
                        </Button>
                      )}
                      <Button size="middle" icon={<SwapOutlined />} onClick={() => setFilmPickerOpen(true)}>
                        更换
                      </Button>
                    </Space>
                  </Flex>
                </div>
              ) : (
                <div className={styles.filmSelectTrigger} onClick={() => setFilmPickerOpen(true)}>
                  <VideoCameraAddOutlined className={styles.triggerIcon} />
                  <div className={styles.triggerText}>选择影片</div>
                </div>
              )}
            </div>

            <Form.Item name="mid" label="影片ID" hidden rules={[{ required: true, message: "请选择影片" }]}>
              <InputNumber style={{ width: "100%" }} />
            </Form.Item>

            {/* 2. 基础属性 */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <span>展示属性</span>
              </div>
              <Row gutter={16}>
                <Col span={12}>
                  <Form.Item name="name" label="影片名称" rules={[{ required: true, message: "请输入影片名称" }]}>
                    <Input placeholder="影片名称" allowClear />
                  </Form.Item>
                </Col>
                <Col span={12}>
                  <Form.Item name="cName" label="影片分类" rules={[{ required: true, message: "请输入影片分类" }]}>
                    <Input placeholder="影片分类" allowClear />
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
            </div>

            {/* 3. 画幅预览与封面 */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <span>画幅预览</span>
                <Space size={8} align="center">
                  <span style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>自定义图片</span>
                  <Form.Item name="isCustomPic" valuePropName="checked" noStyle>
                    <Switch size="small" onChange={handleCustomSwitchChange} />
                  </Form.Item>
                </Space>
              </div>

              <Flex gap={20} justify="center" align="flex-start" style={{ padding: "4px 0 12px" }}>
                <div className={styles.previewCol}>
                  <Text type="secondary" style={{ fontSize: 12 }}>竖版封面</Text>
                  <div className={styles.posterFrame}>
                    {preview.previewPicture ? (
                      <AntImage
                        src={preview.previewPicture}
                        width="100%"
                        height="100%"
                        styles={{ root: { width: "100%", height: "100%", display: "block" } }}
                        style={{ objectFit: "cover" }}
                        fallback={FALLBACK_IMG}
                      />
                    ) : (
                      <div className={styles.slideEmptyBox}>
                        <PictureOutlined style={{ fontSize: 22, color: "var(--ant-color-text-quaternary)" }} />
                        <Text type="secondary" style={{ fontSize: 12 }}>无封面</Text>
                      </div>
                    )}
                  </div>
                </div>

                <div className={styles.previewCol}>
                  <Text type="secondary" style={{ fontSize: 12 }}>横屏大图</Text>
                  <div className={styles.slideFrame}>
                    {preview.previewSlide ? (
                      <AntImage
                        src={preview.previewSlide}
                        width="100%"
                        height="100%"
                        styles={{ root: { width: "100%", height: "100%", display: "block" } }}
                        style={{ objectFit: "cover" }}
                        fallback={FALLBACK_IMG}
                      />
                    ) : (
                      <div className={styles.slideEmptyBox}>
                        <PictureOutlined style={{ fontSize: 22, color: "var(--ant-color-text-quaternary)" }} />
                        <Text type="secondary" style={{ fontSize: 12 }}>无横图</Text>
                      </div>
                    )}
                  </div>
                </div>
              </Flex>

              {watchedIsCustomPic && (
                <Space direction="vertical" size={12} style={{ width: "100%", marginTop: 8 }}>
                  <Form.Item label="竖版封面" required style={{ marginBottom: 0 }}>
                    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                      <Form.Item name="picture" noStyle rules={[{ required: true, message: "请输入竖版封面地址" }]}>
                        <Input placeholder="封面 URL" allowClear style={{ flex: 1 }} />
                      </Form.Item>
                      <Upload showUploadList={false} accept={IMAGE_UPLOAD_ACCEPT} customRequest={(o) => handleCustomUpload(o, "picture")}>
                        <Button icon={<UploadOutlined />}>上传</Button>
                      </Upload>
                      <Button icon={<PictureOutlined />} onClick={() => { setPickerTarget("picture"); setPickerOpen(true); }}>
                        选图
                      </Button>
                    </div>
                  </Form.Item>

                  <Form.Item label="横屏大图" style={{ marginBottom: 0 }}>
                    <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
                      <Form.Item name="pictureSlide" noStyle>
                        <Input placeholder="横图 URL" allowClear style={{ flex: 1 }} />
                      </Form.Item>
                      <Upload showUploadList={false} accept={IMAGE_UPLOAD_ACCEPT} customRequest={(o) => handleCustomUpload(o, "pictureSlide")}>
                        <Button icon={<UploadOutlined />}>上传</Button>
                      </Upload>
                      <Button icon={<PictureOutlined />} onClick={() => { setPickerTarget("pictureSlide"); setPickerOpen(true); }}>
                        选图
                      </Button>
                    </div>
                  </Form.Item>
                </Space>
              )}
            </div>
          </div>
        </Form>
      </Drawer>

      <FilmPicker
        open={filmPickerOpen}
        selectedMid={currentMid}
        onCancel={() => setFilmPickerOpen(false)}
        onSelect={onFilmSelect}
      />

      <ImagePicker
        open={pickerOpen}
        onCancel={() => setPickerOpen(false)}
        onSelect={(link) => {
          form.setFieldValue(pickerTarget, link);
          setPickerOpen(false);
        }}
      />

      {tmdbEnabled && (
        <TmdbModal
          open={tmdbOpen}
          mid={currentMid ? Number(currentMid) : undefined}
          initialName={preview.previewName !== "未选择影片" ? preview.previewName : ""}
          onClose={() => setTmdbOpen(false)}
          onPrefill={handleTmdbPrefill}
        />
      )}
    </>
  );
}
