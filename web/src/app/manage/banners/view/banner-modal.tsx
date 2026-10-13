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

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ImagePicker from "@/app/manage/components/image-picker";
import FilmPicker from "@/app/manage/components/film-picker";
import TmdbModal from "@/app/manage/film/components/tmdb-modal";
import { IMAGE_UPLOAD_ACCEPT, isAllowedImageFile } from "@/lib/imageUpload";
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

interface BannerSnapshot {
  formValues: BannerFormValues;
  selectedFilm: FilmOption | null;
}

interface BannerModalProps {
  open: boolean;
  mode: EditorMode;
  currentRow: BannerRecord | null;
  tmdbEnabled?: boolean;
  onClose: () => void;
  onSuccess: () => void;
}

export default function BannerModal({
  open,
  mode,
  currentRow,
  tmdbEnabled = false,
  onClose,
  onSuccess,
}: BannerModalProps) {
  const { message } = useAppMessage();
  const [form] = Form.useForm<BannerFormValues>();

  const [selectedFilm, setSelectedFilm] = useState<FilmOption | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [pickerTarget, setPickerTarget] = useState<UploadFieldName>("picture");
  const [filmPickerOpen, setFilmPickerOpen] = useState(false);
  const [tmdbOpen, setTmdbOpen] = useState(false);
  const [tmdbSnapshot, setTmdbSnapshot] = useState<BannerSnapshot | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const watchedName = Form.useWatch("name", form);
  const watchedCName = Form.useWatch("cName", form);
  const watchedYear = Form.useWatch("year", form);
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
    try {
      const resp = await ApiGet("/films/play-info", { id: mid });
      const d = resp?.data?.detail;
      if (resp?.code === 0 && d) {
        const option: FilmOption = {
          id: d.id || mid,
          name: d.name || "",
          cName: d.cName || "",
          year: d.year || "",
          remarks: d.remarks || "",
          picture: d.picture || "",
          pictureSlide: d.pictureSlide || "",
          area: d.area,
          director: d.director,
          actor: d.actor,
          label: d.name || "",
          value: d.id || mid,
          isScraped: d.isScraped,
        };
        setSelectedFilm(option);
        return option;
      }
    } catch {}
    return null;
  }, []);

  const onFilmSelect = (film: FilmOption) => {
    setSelectedFilm(film);
    setTmdbSnapshot(null);
    form.setFieldsValue({
      ...form.getFieldsValue(),
      mid: film.id,
      name: film.name || "",
      cName: film.cName || "",
      year: parseInt(String(film.year || "0"), 10) || undefined,
      picture: "",
      pictureSlide: "",
      isCustomPic: false,
    });
    setFilmPickerOpen(false);
    if (film.id) void loadFilmDetail(film.id);
  };

  useEffect(() => {
    if (!open) return;
    setTmdbSnapshot(null);
    if (mode === "edit" && currentRow) {
      const isCustom = currentRow.isCustomPic === true || Boolean(currentRow.customPicture?.trim());
      form.setFieldsValue({
        mid: currentRow.mid,
        name: currentRow.name,
        cName: currentRow.cName,
        year: currentRow.year,
        isCustomPic: isCustom,
        picture: isCustom ? resolveEditablePicture(currentRow) : "",
        pictureSlide: isCustom ? (currentRow.pictureSlide || "") : "",
        sort: currentRow.sort ?? 0,
      });
      if (currentRow.mid) void loadFilmDetail(currentRow.mid);
    } else if (mode === "create") {
      form.setFieldsValue({ isCustomPic: false, sort: 0 });
    }
  }, [open, mode, currentRow, form, loadFilmDetail]);

  const handleCustomUpload = async (options: any, fieldName: UploadFieldName) => {
    const { file, onSuccess: uploadSuccess, onError: uploadError } = options;
    if (!isAllowedImageFile(file)) {
      message.error("仅支持上传 JPG/JPEG/PNG/WebP/ICO 格式的图片");
      uploadError?.(new Error("unsupported image type"));
      return;
    }
    const formData = new FormData();
    formData.append("file", file);
    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        const fullUrl = typeof window !== "undefined" && String(resp.data).startsWith("/")
          ? `${window.location.origin}${resp.data}`
          : resp.data;
        form.setFieldValue(fieldName, fullUrl);
        message.success(resp.msg);
        uploadSuccess?.(fullUrl);
      } else {
        message.error(resp.msg);
        uploadError?.(new Error(resp.msg));
      }
    } catch (err) {
      uploadError?.(err);
    }
  };

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    // 记录最初未被刮削覆盖的表单与选中影片状态，用于后续一键还原
    if (!tmdbSnapshot) {
      setTmdbSnapshot({
        formValues: form.getFieldsValue(true),
        selectedFilm: selectedFilm ? { ...selectedFilm } : null,
      });
    }

    const shouldFill = (name: string, ...aliases: string[]) => {
      if (!fields || fields.length === 0) return true;
      return [name, ...aliases].some((key) => fields.includes(key));
    };

    const currentValues = form.getFieldsValue();
    const updates: Partial<BannerFormValues> = {};

    if (shouldFill("name") && data.name) {
      updates.name = data.name;
    }
    if (shouldFill("subTitle", "cName") && (data.subTitle || data.cName)) {
      updates.cName = data.subTitle || data.cName;
    }
    if (shouldFill("year", "releaseDate") && (data.year || data.releaseDate)) {
      const parsedYear = parseInt(String(data.year || data.releaseDate || "0"), 10);
      if (parsedYear > 0) {
        updates.year = parsedYear;
      }
    }

    const fillPoster = shouldFill("poster", "picture") && Boolean(data.picture);
    const fillSlide = shouldFill("backdrop", "pictureSlide") && Boolean(data.pictureSlide);

    if (fillPoster || fillSlide) {
      // 开启自定义封面，并将刮削结果作为当前轮播的自定义图片覆盖展示
      updates.isCustomPic = true;
      if (fillPoster) {
        updates.picture = data.picture;
      } else {
        updates.picture = currentValues.picture || selectedFilm?.picture || "";
      }
      if (fillSlide) {
        updates.pictureSlide = data.pictureSlide;
      } else {
        updates.pictureSlide = currentValues.pictureSlide || selectedFilm?.pictureSlide || "";
      }
    }

    form.setFieldsValue(updates);

    // 仅同步前端临时展示对象，不向后端发送持久化请求
    setSelectedFilm((prev) => {
      const base: FilmOption = prev || {
        id: currentMid || 0,
        name: data.name || "",
        label: data.name || "",
        value: currentMid || 0,
      };
      return {
        ...base,
        name: updates.name || base.name,
        cName: updates.cName || base.cName,
        year: updates.year || base.year,
        director: data.director || base.director,
        actor: data.actor || base.actor,
      };
    });

    setTmdbOpen(false);
  };

  const handleUndoTmdb = () => {
    if (!tmdbSnapshot) return;
    form.resetFields();
    form.setFieldsValue(tmdbSnapshot.formValues);
    setSelectedFilm(tmdbSnapshot.selectedFilm);
    setTmdbSnapshot(null);
    message.info("已还原刮削，表单已恢复填充前状态");
  };

  const buildBannerPayload = (values: BannerFormValues): BannerRecord => {
    const isCustom = Boolean(values.isCustomPic);
    const customPic = isCustom ? (values.picture || "").trim() : "";
    const customSlide = isCustom ? (values.pictureSlide || "").trim() : "";
    const livePic = selectedFilm?.picture || currentRow?.picture || "";
    const liveSlide = selectedFilm?.pictureSlide || currentRow?.pictureSlide || livePic;

    return {
      id: currentRow?.id || "",
      mid: values.mid || currentRow?.mid || 0,
      name: String(values.name || "").trim(),
      cName: String(values.cName || "").trim(),
      year: values.year,
      poster: isCustom && customPic ? customPic : livePic,
      picture: isCustom && customPic ? customPic : livePic,
      pictureSlide: isCustom && customSlide ? customSlide : (customPic || liveSlide),
      customPicture: isCustom ? customPic : "",
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
      const requestPath = mode === "create" ? "/manage/banner/add" : "/manage/banner/update";
      const requestPayload = mode === "create" ? payload : { ...currentRow, ...payload };
      const resp = await ApiPost(requestPath, requestPayload);
      if (resp.code === 0) {
        message.success(resp.msg);
        onSuccess();
        onClose();
      } else {
        message.error(resp.msg);
      }
    } catch (err: any) {
      if (err?.errorFields) return;
      message.error(err?.message || "提交轮播信息失败");
    } finally {
      setSubmitting(false);
    }
  };

  const currentMid = selectedFilm?.id || currentRow?.mid || form.getFieldValue("mid");
  const previewFilm = selectedFilm || currentRow;
  const previewName = watchedName || previewFilm?.name || "未选择影片";
  const previewCategory = watchedCName || previewFilm?.cName || "未分类";
  const previewYear = watchedYear || previewFilm?.year || "未知年份";
  const previewRemark = selectedFilm?.remarks || currentRow?.remark || "前台按片库实时状态展示";
  const previewArea = selectedFilm?.area || "未知地区";
  const previewDirector = selectedFilm?.director || "暂无";
  const previewActor = selectedFilm?.actor || "暂无";

  const liveFilmPic = selectedFilm?.picture || (currentRow && !currentRow.isCustomPic ? currentRow.picture : "") || "";
  const previewPicture = !watchedIsCustomPic
    ? (liveFilmPic || resolvePreviewPicture(previewFilm))
    : (watchedPicture || liveFilmPic || resolvePreviewPicture(previewFilm));

  const liveFilmSlide = selectedFilm?.pictureSlide || currentRow?.pictureSlide || "";
  const previewSlide = !watchedIsCustomPic
    ? liveFilmSlide
    : (watchedPictureSlide || liveFilmSlide);

  const renderImageField = (fieldName: UploadFieldName, label: string, tooltip: string) => (
    <Form.Item label={label} tooltip={tooltip}>
      <Space.Compact style={{ width: "100%" }}>
        <Form.Item name={fieldName} noStyle rules={fieldName === "picture" ? [{ required: true, message: "请输入图片地址或选图" }] : []}>
          <Input placeholder={`输入 ${label} URL`} />
        </Form.Item>
        <Upload showUploadList={false} accept={IMAGE_UPLOAD_ACCEPT} customRequest={(o) => handleCustomUpload(o, fieldName)}>
          <Button size="middle" icon={<UploadOutlined />} style={{ marginLeft: 8 }}>上传</Button>
        </Upload>
        <Button size="middle" icon={<PictureOutlined />} onClick={() => { setPickerTarget(fieldName); setPickerOpen(true); }}>
          选图
        </Button>
      </Space.Compact>
    </Form.Item>
  );

  return (
    <Modal
      title={mode === "create" ? "添加轮播" : "修改轮播信息"}
      open={open}
      onOk={handleSubmit}
      onCancel={onClose}
      confirmLoading={submitting}
      okButtonProps={{ size: "middle" }}
      cancelButtonProps={{ size: "middle" }}
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
            tooltip={
              tmdbEnabled
                ? "点击从片库选择影片，可在弹窗内一键 TMDB 刮削获取高清横图。"
                : "点击从片库选择影片。"
            }
          >
            {previewFilm?.name ? (
              <Card size="small" bordered className={styles.filmSelectedCard} styles={{ body: { padding: 12 } }}>
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
                      <Flex align="center" gap={6}>
                        <Title level={5} style={{ margin: 0 }} ellipsis={{ tooltip: previewName }}>{previewName}</Title>
                        {(currentRow?.isScraped || selectedFilm?.isScraped) && (
                          <Tag color="#722ed1" style={{ borderRadius: 4, margin: 0, fontSize: 11, lineHeight: "18px", padding: "0 4px" }}>
                            已刮削
                          </Tag>
                        )}
                      </Flex>
                      <Text type="secondary" style={{ fontSize: 12 }}>{previewCategory} · {previewYear} · {previewArea}</Text>
                      <Text type="secondary" style={{ fontSize: 12 }} ellipsis={{ tooltip: `导演: ${previewDirector} | 主演: ${previewActor}` }}>
                        状态: {previewRemark}
                      </Text>
                    </Space>
                  </Flex>
                  <Space size={8}>
                    {tmdbSnapshot && (
                      <Button
                        size="middle"
                        icon={<UndoOutlined />}
                        onClick={handleUndoTmdb}
                      >
                        还原刮削
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
                      更换影片
                    </Button>
                  </Space>
                </Flex>
              </Card>
            ) : (
              <div className={styles.filmSelectTrigger} onClick={() => setFilmPickerOpen(true)}>
                <VideoCameraAddOutlined className={styles.triggerIcon} />
                <div className={styles.triggerText}>点击从片库搜索并选择影片</div>
                <div className={styles.triggerHint}>支持按片名模糊检索、排序筛选与海报图文预览</div>
              </div>
            )}
          </Form.Item>

          <Form.Item name="mid" label="影片ID" hidden rules={[{ required: true, message: "请选择绑定的影片" }]}>
            <InputNumber style={{ width: "100%" }} />
          </Form.Item>

          <Row gutter={12}>
            <Col span={12}>
              <Form.Item name="name" label="影片名称" rules={[{ required: true, message: "请输入影片名称" }]}>
                <Input placeholder="封面卡片展示名称" />
              </Form.Item>
            </Col>
            <Col span={12}>
              <Form.Item name="cName" label="影片分类" rules={[{ required: true, message: "请输入影片分类" }]}>
                <Input placeholder="封面卡片展示分类" />
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

          <Form.Item
            label="自定义封面"
            name="isCustomPic"
            valuePropName="checked"
            initialValue={false}
            tooltip="关闭时默认自动同步影片的海报源或 TMDB 刮削数据；开启后可为本轮播单独配置自定义封面或横屏大图，覆盖底层影片数据。"
          >
            <Switch
              checkedChildren="自定义开启"
              unCheckedChildren="跟随影片"
            />
          </Form.Item>

          {!watchedIsCustomPic ? (
            <Form.Item label="封面图片展示来源">
              <div
                style={{
                  padding: "10px 14px",
                  background: "rgba(255, 255, 255, 0.04)",
                  borderRadius: 6,
                  border: "1px dashed var(--ant-color-border)",
                  display: "flex",
                  alignItems: "center",
                  justifyContent: "space-between",
                  gap: 16,
                }}
              >
                <Space direction="vertical" size={2}>
                  <Text style={{ fontSize: 13, color: "var(--ant-color-text-secondary)" }}>
                    未开启自定义封面，自动采用影片原本的数据
                    {selectedFilm?.isScraped || currentRow?.isScraped ? "（已应用 TMDB 刮削数据）" : "（海报源原始数据）"}。
                  </Text>
                  <Text type="secondary" style={{ fontSize: 12 }}>
                    开启「自定义封面」开关后，可为此轮播项单独上传或指定封面与横屏大图。
                  </Text>
                </Space>
                {previewPicture && (
                  <AntImage
                    src={previewPicture || FALLBACK_IMG}
                    height={38}
                    style={{ borderRadius: 4, objectFit: "cover" }}
                    fallback={FALLBACK_IMG}
                    preview={false}
                  />
                )}
              </div>
            </Form.Item>
          ) : (
            <>
              {renderImageField("picture", "竖版封面地址", "自定义竖版封面图片，覆盖底层影片的海报源或刮削数据。")}
              {renderImageField("pictureSlide", "横屏大图地址", "自定义横屏背景大图 (Backdrop)，覆盖底层影片的海报源或刮削数据。")}
            </>
          )}

          <Row gutter={12}>
            <Col span={10}>
              <Card size="small" title="竖版封面预览" style={{ borderRadius: 8 }}>
                <AntImage
                  src={previewPicture || FALLBACK_IMG}
                  height={150}
                  style={{ objectFit: "cover", borderRadius: 6, width: "100%", background: "var(--public-surface-3)" }}
                  fallback={FALLBACK_IMG}
                />
              </Card>
            </Col>
            <Col span={14}>
              <Card size="small" title="横屏大图预览 (Backdrop)" style={{ borderRadius: 8 }}>
                {previewSlide && previewSlide !== previewPicture ? (
                  <AntImage
                    src={previewSlide}
                    height={150}
                    style={{ objectFit: "cover", borderRadius: 6, width: "100%", background: "var(--public-surface-3)" }}
                    fallback={FALLBACK_IMG}
                  />
                ) : (
                  <Flex
                    vertical
                    align="center"
                    justify="center"
                    style={{
                      height: 150,
                      borderRadius: 6,
                      background: "rgba(255, 255, 255, 0.02)",
                      border: "1px dashed var(--ant-color-border-secondary)",
                    }}
                  >
                    <Text type="secondary" style={{ fontSize: 13 }}>暂无横屏大图</Text>
                    {tmdbEnabled ? (
                      <Text type="secondary" style={{ fontSize: 12, marginTop: 4 }}>点击上方【TMDB 刮削】可一键补齐</Text>
                    ) : (
                      <Text type="secondary" style={{ fontSize: 12, marginTop: 4 }}>可上传图片或输入外部图片地址</Text>
                    )}
                  </Flex>
                )}
              </Card>
            </Col>
          </Row>
        </Space>
      </Form>

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
          initialName={previewName !== "未选择影片" ? previewName : ""}
          initialYear={previewYear !== "未知年份" ? previewYear : ""}
          onClose={() => setTmdbOpen(false)}
          onPrefill={handleTmdbPrefill}
        />
      )}
    </Modal>
  );
}
