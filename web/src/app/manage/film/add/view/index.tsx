"use client";

import React, { useState, useEffect, useRef, Suspense, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import {
  Form,
  Input,
  Select,
  Button,
  Upload,
  InputNumber,
  Space,
  Spin,
  Card,
  Row,
  Col,
  Tag,
  Radio,
  Image as AntImage,
  Typography,
} from "antd";
import {
  UploadOutlined,
  InfoCircleOutlined,
  UserOutlined,
  GlobalOutlined,
  DatabaseOutlined,
  ContainerOutlined,
  PictureOutlined,
  CompassOutlined,
  UndoOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { isFormValuesEqual } from "@/lib/deep-equal";
import ImagePicker from "@/app/manage/components/image-picker";
import TmdbModal from "../../components/tmdb-modal";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import {
  IMAGE_UPLOAD_ACCEPT,
  isAllowedImageFile,
} from "@/lib/imageUpload";
import styles from "./index.module.less";

const { TextArea } = Input;
const { Text } = Typography;

function FilmAddForm() {
  const [form] = Form.useForm();
  const tmdbEnabled = useTmdbEnabled();
  const [categories, setCategories] = useState<any[]>([]);
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [tmdbModalOpen, setTmdbModalOpen] = useState(false);
  const [tmdbSnapshot, setTmdbSnapshot] = useState<Record<string, any> | null>(null);
  const searchParams = useSearchParams();
  const router = useRouter();
  const id = searchParams.get("id");
  const readOnly = Boolean(id) && searchParams.get("readonly") === "1";
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const handleBack = useCallback(() => {
    if (typeof window !== "undefined" && window.history.length > 1) {
      router.back();
    } else {
      router.push("/manage/film");
    }
  }, [router]);

  const watchedFollowPosterSource = Form.useWatch("followPosterSource", form);
  const watchedPicture = Form.useWatch("picture", form);

  const loadedDetailRef = useRef<any>(null);
  const [loadedDetail, setLoadedDetail] = useState<any>(null);
  const [isDirty, setIsDirty] = useState(false);
  const baselineRef = useRef<Record<string, any> | null>(null);

  const checkDirty = useCallback((allValues?: any) => {
    if (!baselineRef.current) {
      setIsDirty(false);
      return;
    }
    const current = allValues ?? form.getFieldsValue();
    const equal = isFormValuesEqual(current, baselineRef.current);
    setIsDirty(!equal);
  }, [form]);

  const populateForm = useCallback((filmData: any) => {
    if (!filmData) return;
    const filmDescriptor = filmData.descriptor || {};

    const isCustom = filmData.isCustomPicture === true;
    const displayPic = String(filmData.picture || "").trim();
    const savedCustom = String(filmData.customPicture || "").trim();
    const customPic = isCustom ? savedCustom || displayPic : displayPic;
    const slidePic = filmData.customPictureSlide || filmData.pictureSlide || "";

    form.setFieldsValue({
      id: filmData.id,
      cid: filmData.cid,
      pid: filmData.pid,
      cName: filmData.cName || "",
      name: filmData.name,
      followPosterSource: !isCustom,
      picture: customPic,
      pictureSlide: slidePic,
      subTitle: filmDescriptor.subTitle || "",
      initial: filmDescriptor.initial || "",
      classTag: filmDescriptor.classTag || "",
      director: filmDescriptor.director || "",
      actor: filmDescriptor.actor || "",
      writer: filmDescriptor.writer || "",
      remarks: filmDescriptor.remarks || "",
      releaseDate: filmDescriptor.releaseDate || "",
      area: filmDescriptor.area || "",
      lang: filmDescriptor.language || "",
      year: filmDescriptor.year || "",
      state: filmDescriptor.state || "",
      dbId: filmDescriptor.dbId ?? 0,
      dbScore: filmDescriptor.dbScore || "",
      hits: filmDescriptor.hits ?? 0,
      content: filmDescriptor.content || "",
    });
    baselineRef.current = form.getFieldsValue();
    setIsDirty(false);
  }, [form]);

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    // 记录填充前的表单快照（优先保留最初未被 TMDB 覆盖的表单状态，避免连续预填后丢失原数据）
    const currentValues = form.getFieldsValue();
    setTmdbSnapshot((prev) => prev || currentValues);

    const shouldFill = (name: string, ...aliases: string[]) => {
      if (!fields || fields.length === 0) return true;
      return [name, ...aliases].some((key) => fields.includes(key));
    };

    const updates: Record<string, any> = {};
    if (!form.getFieldValue("name") && data.name) {
      updates.name = data.name;
    }
    if (shouldFill("poster", "picture") && data.picture) {
      updates.picture = data.picture;
      updates.followPosterSource = false;
    }
    if (shouldFill("backdrop", "pictureSlide") && data.pictureSlide) {
      updates.pictureSlide = data.pictureSlide;
    }
    if (shouldFill("overview", "content") && data.content) {
      updates.content = data.content;
    }
    if (shouldFill("subTitle") && data.subTitle) {
      updates.subTitle = data.subTitle;
    }
    if (shouldFill("actor") && data.actor) {
      updates.actor = data.actor;
    }
    if (shouldFill("director") && data.director) {
      updates.director = data.director;
    }
    if (shouldFill("year", "releaseDate")) {
      if (data.year) updates.year = data.year;
      if (data.releaseDate) updates.releaseDate = data.releaseDate;
    }
    if (shouldFill("score") && data.dbScore) {
      updates.dbScore = data.dbScore;
    }
    if (shouldFill("tag") && data.classTag) {
      updates.classTag = data.classTag;
    }

    if (Object.keys(updates).length > 0) {
      form.setFieldsValue(updates);
      checkDirty();
    }
  };

  const handleUndoTmdb = () => {
    if (!tmdbSnapshot) return;
    form.setFieldsValue(tmdbSnapshot);
    setTmdbSnapshot(null);
    checkDirty();
    message.info("已撤销 TMDB 填充，表单已恢复填充前状态");
  };

  const handleResetToOriginal = () => {
    if (loadedDetailRef.current) {
      populateForm(loadedDetailRef.current);
      setTmdbSnapshot(null);
      setIsDirty(false);
      message.success("已还原初始数据");
    }
  };

  const handleClearForm = () => {
    form.resetFields();
    form.setFieldsValue({ followPosterSource: false });
    setTmdbSnapshot(null);
    setIsDirty(false);
    message.info("已重置表单");
  };

  useEffect(() => {
    ApiGet("/manage/film/class/tree").then((resp: any) => {
      if (resp.code === 0) {
        let list: any[] = [];
        resp.data.children?.forEach((parent: any) => {
          if (parent.children && parent.children.length > 0) {
            list = [...list, ...parent.children];
          }
        });
        setCategories(list);
      }
    });

    if (!id) {
      baselineRef.current = form.getFieldsValue();
      setIsDirty(false);
    } else {
      setFetching(true);
      ApiGet(`/films/play-info`, { id, stored: 1 })
        .then((resp: any) => {
          if (resp.code === 0 && resp.data?.detail) {
            const filmData = resp.data.detail;
            loadedDetailRef.current = filmData;
            setLoadedDetail(filmData);
            populateForm(filmData);
          } else {
            message.error("获取影片详情失败");
          }
        })
        .finally(() => setFetching(false));
    }
  }, [id, form, message, populateForm]);

  const handleClassChange = (value: number) => {
    const selected = categories.find((c) => c.id === value);
    if (selected) {
      form.setFieldsValue({
        cid: selected.id,
        pid: selected.pid,
        cName: selected.name,
      });
      checkDirty();
    }
  };

  const onFinish = async (values: any) => {
    if (readOnly) return;
    setLoading(true);
    try {
      const isCustom = !values.followPosterSource;
      const customPic = isCustom ? (values.picture || "").trim() : "";
      const sourcePic = loadedDetailRef.current?.picture || "";
      const customSlide = isCustom
        ? (values.pictureSlide || loadedDetailRef.current?.customPictureSlide || "").trim()
        : "";
      // 明确剔除剧集播放资源字段，不向后端传递 playLink，绝不更新剧集数据
      const { playLink, playForm, ...restValues } = values;
      const payload = {
        ...restValues,
        language: restValues.lang || restValues.language || "",
        id: id ? Number(id) : 0,
        dbId: Number(restValues.dbId) || 0,
        hits: Number(restValues.hits) || 0,
        isCustomPicture: isCustom,
        customPicture: customPic,
        picture: isCustom ? (sourcePic || customPic) : "",
        customPictureSlide: customSlide,
      };

      const resp = await ApiPost("/manage/film/add", payload);
      if (resp.code === 0) {
        message.success(id ? "影视更新成功" : "影片添加成功");
        setTmdbSnapshot(null);
        if (!id) {
          form.resetFields();
          form.setFieldsValue({ followPosterSource: false });
        } else {
          ApiGet(`/films/play-info`, { id, stored: 1 }).then((fresh: any) => {
            if (fresh.code === 0 && fresh.data?.detail) {
              loadedDetailRef.current = fresh.data.detail;
              setLoadedDetail(fresh.data.detail);
            }
          });
        }
      } else {
        message.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  };

  const customUpload = async (options: any) => {
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
        message.success(resp.msg);
        const fullUrl =
          typeof window !== "undefined" && String(resp.data).startsWith("/")
            ? `${window.location.origin}${resp.data}`
            : resp.data;
        form.setFieldValue("picture", fullUrl);
        onSuccess(fullUrl);
        checkDirty();
      } else {
        message.error(resp.msg);
        onError(resp.msg);
      }
    } catch (err: any) {
      // 拦截器已统一提示，避免重复弹窗
      onError(err);
    }
  };

  if (fetching) {
    return (
      <div className={styles.loadingContainer}>
        <Spin size="large" description="正在加载影片数据..." />
      </div>
    );
  }

  return (
    <div className={styles.pageContainer}>
      <div className={styles.fixedHeader}>
        <ManagePageHeader
          className={styles.pageHeaderReset}
          title={readOnly ? "影片详情" : id ? "编辑影片" : "新增影片"}
          description={readOnly ? "查看影片信息" : id ? "修改影片详情与播放资源" : "录入影片基本信息与播放资源"}
          onBack={handleBack}
        />
      </div>

      <div className={styles.scrollBody}>
        <Form
          form={form}
          layout="vertical"
          onFinish={onFinish}
          onValuesChange={(_changed, allValues) => checkDirty(allValues)}
          disabled={readOnly}
          className={`${styles.form} ${styles.formCompact}`}
        initialValues={{
          dbId: 0,
          hits: 0,
          followPosterSource: Boolean(id),
        }}
        requiredMark="optional"
      >
        <Space orientation="vertical" size={16} className={styles.formSections}>
          <Card
            title={
              <Space>
                <InfoCircleOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
                基础信息
              </Space>
            }
            extra={readOnly ? null : (
              <Space>
                {tmdbSnapshot && (
                  <Button
                    icon={<UndoOutlined />}
                    onClick={handleUndoTmdb}
                  >
                    撤销填入
                  </Button>
                )}
                {tmdbEnabled ? (
                  <Button
                    icon={<CompassOutlined />}
                    style={{ color: "#722ed1", borderColor: "#722ed1" }}
                    onClick={() => setTmdbModalOpen(true)}
                    disabled={!canWrite}
                  >
                    刮削
                  </Button>
                ) : null}
              </Space>
            )}
            className={styles.sectionCard}
            styles={{
              header: {
                background: "rgba(255, 255, 255, 0.02)",
                borderBottom: "1px solid var(--ant-color-border-secondary)",
              },
            }}
          >
            <div className={styles.basicInfoLayout}>
              {/* 左侧：封面图展示与快捷操作 */}
              <div className={styles.posterColumn}>
                <div className={styles.posterWrapper}>
                  <AntImage
                    src={watchedPicture || loadedDetail?.picture || FALLBACK_IMG}
                    fallback={FALLBACK_IMG}
                    alt="影片封面"
                    preview={{ mask: "查看封面" }}
                  />
                  <div className={styles.posterTagBadge}>
                    <Tag color={watchedFollowPosterSource ? "blue" : "orange"} style={{ margin: 0 }}>
                      {watchedFollowPosterSource ? "跟随海报源" : "自定义封面"}
                    </Tag>
                  </div>
                </div>
              </div>

              {/* 右侧：基础字段表单 */}
              <div className={styles.formColumn}>
                <Row gutter={[24, 0]}>
                  <Col xs={24} sm={12}>
                    <Form.Item
                      label="影片名称"
                      name="name"
                      rules={[{ required: true, message: "请输入名称" }]}
                    >
                      <Input placeholder="请输入影片名称" />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12}>
                    <Form.Item label="影片别名" name="subTitle">
                      <Input placeholder="如: 英文名、又名" />
                    </Form.Item>
                  </Col>
                  <Col xs={24} sm={12}>
                    <Form.Item
                      label="所属分类"
                      name="cid"
                      rules={[{ required: true, message: "请选择分类" }]}
                    >
                      <Select
                        placeholder="请选择"
                        onChange={handleClassChange}
                        options={categories.map((c: any) => ({
                          label: c.name,
                          value: c.id,
                        }))}
                      />
                    </Form.Item>
                  </Col>

                  <Form.Item name="pid" hidden>
                    <Input />
                  </Form.Item>
                  <Form.Item name="cName" hidden>
                    <Input />
                  </Form.Item>

                  <Col xs={24} sm={12}>
                    <Form.Item
                      label="是否跟随海报源"
                      name="followPosterSource"
                      tooltip="关闭后保存自定义封面，采集不覆盖。"
                      initialValue={Boolean(id)}
                    >
                      <Radio.Group
                        buttonStyle="solid"
                        options={[
                          { label: "是（跟随海报源）", value: true },
                          { label: "否（自定义封面）", value: false },
                        ]}
                      />
                    </Form.Item>
                  </Col>

                  {!watchedFollowPosterSource ? (
                    <Col xs={24}>
                      <Form.Item
                        label="封面图片地址"
                        tooltip="采集不覆盖。对得上海报源时，前台用那边的最终图。"
                      >
                        <Space.Compact style={{ width: "100%" }}>
                          <Form.Item
                            name="picture"
                            noStyle
                            rules={[{ required: true, message: "请输入自定义封面图片地址或上传" }]}
                          >
                            <Input
                              className={styles.posterInput}
                              placeholder="输入图片URL或点击右侧上传/选图"
                            />
                          </Form.Item>
                          {!readOnly && (
                            <Upload
                              customRequest={customUpload}
                              showUploadList={false}
                              accept={IMAGE_UPLOAD_ACCEPT}
                              disabled={!canWrite}
                            >
                              <Button icon={<UploadOutlined />}>上传封面</Button>
                            </Upload>
                          )}
                          {!readOnly && (
                            <Button
                              icon={<PictureOutlined />}
                              disabled={!canWrite}
                              onClick={() => setPickerOpen(true)}
                            >
                              选图
                            </Button>
                          )}
                        </Space.Compact>
                      </Form.Item>
                    </Col>
                  ) : (
                    <Col xs={24}>
                      <div
                        style={{
                          padding: "8px 12px",
                          background: "rgba(255, 255, 255, 0.03)",
                          borderRadius: 6,
                          border: "1px dashed var(--ant-color-border)",
                          marginBottom: 20,
                        }}
                      >
                        <Text style={{ fontSize: 13, color: "var(--ant-color-text-secondary)", wordBreak: "break-all" }}>
                          {String(loadedDetail?.picture || "").trim() || "暂无封面"}
                        </Text>
                      </div>
                    </Col>
                  )}
                </Row>
              </div>
            </div>
          </Card>

            <Card
              title={
                <Space>
                <UserOutlined style={{ color: "var(--ant-color-primary)" }} />
                演职人员
              </Space>
            }
            className={styles.sectionCard}
            styles={{
              header: {
                background: "rgba(255, 255, 255, 0.02)",
                borderBottom: "1px solid var(--ant-color-border-secondary)",
              },
            }}
          >
            <Row gutter={[40, 0]} className={styles.formRow}>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="导演" name="director">
                  <Input placeholder="多个以逗号分隔" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="主演" name="actor">
                  <Input placeholder="多个以逗号分隔" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="作者/编剧" name="writer">
                  <Input placeholder="多个以逗号分隔" />
                </Form.Item>
              </Col>
            </Row>
          </Card>

          <Card
            title={
              <Space>
                <GlobalOutlined style={{ color: "var(--ant-color-primary)" }} />
                发行与元数据
              </Space>
            }
            className={styles.sectionCard}
            styles={{
              header: {
                background: "rgba(255, 255, 255, 0.02)",
                borderBottom: "1px solid var(--ant-color-border-secondary)",
              },
            }}
          >
            <Row gutter={[40, 0]} className={styles.formRow}>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="上映日期" name="releaseDate">
                  <Input placeholder="YYYY-MM-DD" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="制作地区" name="area">
                  <Input placeholder="如: 中国大陆, 美国" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="语言" name="lang">
                  <Input placeholder="如: 国语, 英语" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="上映年份" name="year">
                  <Input placeholder="YYYY" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="检索首字母" name="initial">
                  <Input placeholder="大写字母" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="剧情标签" name="classTag">
                  <Input placeholder="如: 动作, 冒险" />
                </Form.Item>
              </Col>
            </Row>
          </Card>

          <Card
            title={
              <Space>
                <DatabaseOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
                状态与外部数据
              </Space>
            }
            className={styles.sectionCard}
            styles={{
              header: {
                background: "rgba(255, 255, 255, 0.02)",
                borderBottom: "1px solid var(--ant-color-border-secondary)",
              },
            }}
          >
            <Row gutter={[40, 0]} className={styles.formRow}>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="更新备注" name="remarks">
                  <Input placeholder="如: 完结, 第10集" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="影片状态" name="state">
                  <Input placeholder="如: 正片, 预告" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="影片热度" name="hits">
                  <InputNumber style={{ width: "100%" }} />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="播放来源标识" name="playForm">
                  <Input placeholder="如: m3u8_list" />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="豆瓣 ID" name="dbId">
                  <InputNumber style={{ width: "100%" }} />
                </Form.Item>
              </Col>
              <Col xs={24} lg={12} xl={8}>
                <Form.Item label="豆瓣评分" name="dbScore">
                  <Input />
                </Form.Item>
              </Col>
            </Row>
          </Card>

          <Card
            title={
              <Space>
                <ContainerOutlined
                  style={{ color: "var(--ant-color-primary)" }}
                />
                剧情详情
              </Space>
            }
            className={styles.sectionCard}
            styles={{
              header: {
                background: "rgba(255, 255, 255, 0.02)",
                borderBottom: "1px solid var(--ant-color-border-secondary)",
              },
            }}
          >
            <Form.Item name="content" noStyle>
              <TextArea rows={6} placeholder="输入剧情详细描述..." />
            </Form.Item>
          </Card>


        </Space>
        </Form>
      </div>

      <ImagePicker
        open={pickerOpen}
        onCancel={() => setPickerOpen(false)}
        onSelect={(link) => {
          form.setFieldValue("picture", link);
          setPickerOpen(false);
          checkDirty();
        }}
      />

      <TmdbModal
        open={tmdbModalOpen}
        mid={id ? Number(id) : undefined}
        initialName={form.getFieldValue("name")}
        onClose={() => setTmdbModalOpen(false)}
        onPrefill={handleTmdbPrefill}
      />

      <UnsavedChangesBar
        visible={isDirty && !readOnly}
        saving={loading}
        discardText={id ? "还原" : "重置"}
        saveText={id ? "保存" : "提交"}
        onDiscard={id ? handleResetToOriginal : handleClearForm}
        onSave={() => form.submit()}
        saveDisabled={!canWrite}
        discardDisabled={!canWrite}
      />
    </div>
  );
}

export default function FilmAddPageView() {
  return (
    <Suspense
      fallback={
        <div className={styles.loadingContainer}>
          <Spin size="large" />
        </div>
      }
    >
      <FilmAddForm />
    </Suspense>
  );
}
