"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Drawer,
  Form,
  InputNumber,
  Image as AntImage,
  Typography,
  Space,
  Flex,
  Button,
  Tag,
  Switch,
} from "antd";
import {
  PictureOutlined,
  VideoCameraAddOutlined,
  SwapOutlined,
  ThunderboltOutlined,
  UndoOutlined,
  ExportOutlined,
} from "@ant-design/icons";

import { ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ImagePicker from "@/app/manage/components/image-picker";
import FilmPicker from "@/app/manage/components/film-picker";
import TmdbModal from "@/app/manage/film/components/tmdb-modal";
import { isAllowedImageFile } from "@/lib/imageUpload";
import {
  BannerRecord,
  BannerFormValues,
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
import BannerDrawerFrame from "./banner-drawer-frame";
import styles from "./banner-drawer.module.less";

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
  const [submitting, setSubmitting] = useState(false);
  const [scrapedData, setScrapedData] = useState<{
    score?: number;
    blurb?: string;
    tmdbId?: number;
  }>({});

  const watchedPicture = Form.useWatch("picture", form);
  const watchedPictureSlide = Form.useWatch("pictureSlide", form);
  const watchedIsCustomPic = Form.useWatch("isCustomPic", form);

  const resetState = useCallback(() => {
    form.resetFields();
    setSelectedFilm(null);
    setPickerOpen(false);
    setFilmPickerOpen(false);
    setTmdbOpen(false);
    setScrapedData({});
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
    setScrapedData({});
    form.setFieldsValue({
      mid: film.id,
      picture: "",
      pictureSlide: "",
      isCustomPic: false,
    });
    setFilmPickerOpen(false);
    if (film.id) {
      const fullDetail = await loadFilmDetail(film.id);
      if (fullDetail) {
        setSelectedFilm((prev) => ({
          ...film,
          ...fullDetail,
          picture: fullDetail.picture || film.picture || "",
          pictureSlide: fullDetail.pictureSlide || film.pictureSlide || "",
          cName: fullDetail.cName || film.cName || "",
          year: fullDetail.year || film.year || "",
          area: fullDetail.area || film.area || "",
          remarks: fullDetail.remarks || film.remarks || "",
        }));
      }
    }
  };

  useEffect(() => {
    if (!open) return;
    setSelectedFilm(null);
    setScrapedData({});
    form.resetFields();

    const initialVals = initBannerFormValues(mode, currentRow);
    form.setFieldsValue(initialVals);

    if (mode === "edit" && currentRow?.mid) {
      loadFilmDetail(currentRow.mid).catch(() => {});
      setScrapedData({
        score: currentRow.score,
        blurb: currentRow.blurb,
        tmdbId: currentRow.tmdbId,
      });
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

  const isReplacingFilm =
    mode === "edit" && selectedFilm !== null && selectedFilm.id !== currentRow?.mid;
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

  const handleResetToFilmDefault = () => {
    form.setFieldsValue({
      isCustomPic: false,
      picture: "",
      pictureSlide: "",
    });
    setScrapedData({});
    message.success("已恢复片库默认图片");
  };

  const currentMid = preview.currentMid || form.getFieldValue("mid");

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    const currentValues = form.getFieldsValue();
    const updates = computeTmdbPrefillUpdates(data, fields, currentValues, selectedFilm);
    form.setFieldsValue(updates);

    const nextScraped: { score?: number; blurb?: string; tmdbId?: number } = { ...scrapedData };
    if (data.id || data.tmdbId) {
      nextScraped.tmdbId = Number(data.id || data.tmdbId);
    }
    if (data.dbScore || data.score) {
      const s = parseFloat(String(data.dbScore || data.score));
      if (!isNaN(s) && s > 0) nextScraped.score = s;
    }
    if (data.content || data.blurb) {
      nextScraped.blurb = data.content || data.blurb;
    }
    setScrapedData(nextScraped);
    setTmdbOpen(false);
    message.success("填入完成");
  };

  const handleGoToFilmList = () => {
    if (!currentMid) return;
    const filmName =
      preview.previewName !== "未选择影片" ? encodeURIComponent(preview.previewName) : "";
    window.open(`/manage/film?name=${filmName}`, "_blank");
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
        scrapedData,
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

  const hasSlide = Boolean(preview.previewSlide?.trim());

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
            <span style={{ fontSize: 16, fontWeight: 600 }}>
              {mode === "create" ? "新增轮播" : "编辑轮播"}
            </span>
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
            <Button type="primary" loading={submitting} onClick={handleSubmit}>
              {mode === "create" ? "创建" : "保存"}
            </Button>
          </Space>
        }
      >
        <Form form={form} layout="vertical">
          <div className={styles.drawerStack}>
            {/* 1. 绑定影片展示卡片（只读展示，严禁在此编辑原片） */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <span>已绑定影片</span>
                {activeFilm?.name && (
                  <Space size={6}>
                    {hasSlide && (
                      <Tag color="success" style={{ margin: 0, borderRadius: 4 }}>
                        横屏大图已配置
                      </Tag>
                    )}
                    {preview.isScraped && (
                      <Tag color="purple" style={{ margin: 0, borderRadius: 4 }}>
                        已刮削
                      </Tag>
                    )}
                  </Space>
                )}
              </div>

              {activeFilm?.name ? (
                <div className={styles.filmSelectedCard}>
                  <Flex gap={14} align="center" justify="space-between" wrap="wrap">
                    <Flex gap={12} align="center" style={{ minWidth: 0 }}>
                      <AntImage
                        src={preview.liveFilmPic || FALLBACK_IMG}
                        width={50}
                        height={70}
                        style={{
                          objectFit: "cover",
                          borderRadius: 6,
                          background: "var(--public-surface-3)",
                        }}
                        fallback={FALLBACK_IMG}
                        preview={false}
                      />
                      <Space direction="vertical" size={2} style={{ minWidth: 0 }}>
                        <Flex align="center" gap={6}>
                          <Title
                            level={5}
                            style={{ margin: 0 }}
                            ellipsis={{ tooltip: preview.previewName }}
                          >
                            {preview.previewName}
                          </Title>
                        </Flex>
                        <Text type="secondary" style={{ fontSize: 12 }}>
                          {preview.previewCategory} · {preview.previewYear} · {preview.previewArea}
                        </Text>
                        <Text
                          type="secondary"
                          style={{ fontSize: 12 }}
                          ellipsis={{ tooltip: `状态: ${preview.previewRemark}` }}
                        >
                          状态: {preview.previewRemark}
                        </Text>
                      </Space>
                    </Flex>
                    <Space size={8} wrap>
                      <Button
                        size="middle"
                        onClick={() => setFilmPickerOpen(true)}
                      >
                        更换影片
                      </Button>
                      {tmdbEnabled && (
                        <Button
                          size="middle"
                          onClick={() => setTmdbOpen(true)}
                          disabled={!currentMid}
                        >
                          TMDB 刮削
                        </Button>
                      )}
                      <Button
                        size="middle"
                        onClick={handleGoToFilmList}
                        disabled={!currentMid}
                      >
                        前往片库编辑
                      </Button>
                      {watchedIsCustomPic && (
                        <Button
                          size="middle"
                          onClick={handleResetToFilmDefault}
                        >
                          恢复片库默认
                        </Button>
                      )}
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

            <Form.Item
              name="mid"
              label="影片ID"
              hidden
              rules={[{ required: true, message: "请选择影片" }]}
            >
              <InputNumber style={{ width: "100%" }} />
            </Form.Item>

            {/* 2. 画幅预览与封面配置 */}
            <BannerDrawerFrame
              preview={preview}
              watchedIsCustomPic={watchedIsCustomPic}
              slideUrl={form.getFieldValue("pictureSlide")}
              onCustomSwitchChange={handleCustomSwitchChange}
              onCustomUpload={handleCustomUpload}
              onOpenPicker={(t) => {
                setPickerTarget(t);
                setPickerOpen(true);
              }}
              onClearSlide={() => form.setFieldValue("pictureSlide", "")}
            />

            {/* 3. 排片配置属性 */}
            <div className={styles.drawerSectionCard}>
              <div className={styles.drawerSectionTitle}>
                <span>排片设置</span>
              </div>
              <Flex gap={24} align="center">
                <Form.Item name="sort" label="排序分值" style={{ marginBottom: 0, flex: 1 }}>
                  <InputNumber min={0} max={9999} style={{ width: "100%" }} />
                </Form.Item>
                <Form.Item
                  name="isPinned"
                  label="置顶锁定"
                  valuePropName="checked"
                  style={{ marginBottom: 0 }}
                >
                  <Switch />
                </Form.Item>
              </Flex>
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
