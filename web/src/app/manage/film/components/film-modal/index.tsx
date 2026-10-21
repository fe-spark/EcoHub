"use client";

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
import {
  Modal,
  Form,
  Input,
  InputNumber,
  Select,
  TreeSelect,
  Radio,
  Space,
  Button,
  Upload,
  Tag,
  Image as AntImage,
  Typography,
  Row,
  Col,
  Card,
  Spin,
  Alert,
  Flex,
} from "antd";
import {
  InfoCircleOutlined,
  UndoOutlined,
  CompassOutlined,
  UploadOutlined,
  PictureOutlined,
  UserOutlined,
  GlobalOutlined,
  DatabaseOutlined,
  ContainerOutlined,
  PlayCircleOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import { isAllowedImageFile, IMAGE_UPLOAD_ACCEPT } from "@/lib/imageUpload";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import TmdbModal from "../tmdb-modal";
import ImagePicker from "@/app/manage/components/image-picker";
import { formatCategoryNodes, flattenCategoryNodes, resolveCategoryFields } from "./utils";
import type { FilmModalProps } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;
const { TextArea } = Input;

export default function FilmModal({
  open,
  filmId,
  defaultSourceId,
  defaultCid,
  onClose,
  onSuccess,
}: FilmModalProps) {
  const [form] = Form.useForm();
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const tmdbEnabled = useTmdbEnabled();

  const [sources, setSources] = useState<{ id: string; name: string; state?: boolean }[]>([]);
  const [categoryTreeNodes, setCategoryTreeNodes] = useState<any[]>([]);
  const [categoryLoading, setCategoryLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const [pickerOpen, setPickerOpen] = useState(false);
  const [tmdbModalOpen, setTmdbModalOpen] = useState(false);
  const [tmdbSnapshot, setTmdbSnapshot] = useState<Record<string, any> | null>(null);

  const loadedDetailRef = useRef<any>(null);
  const [loadedDetail, setLoadedDetail] = useState<any>(null);
  const allCategoryListRef = useRef<any[]>([]);

  const watchedFollowPosterSource = Form.useWatch("followPosterSource", form);
  const watchedPicture = Form.useWatch("picture", form);

  const categoryTreeData = useMemo(() => formatCategoryNodes(categoryTreeNodes), [categoryTreeNodes]);
  const allCategoryList = useMemo(() => flattenCategoryNodes(categoryTreeNodes), [categoryTreeNodes]);
  allCategoryListRef.current = allCategoryList;

  const loadCategoryTree = useCallback(
    async (targetSourceId: string) => {
      setCategoryLoading(true);
      try {
        const resp = await ApiGet("/manage/film/search/options", {
          sourceId: targetSourceId || undefined,
        });
        if (resp.code === 0 && resp.data) {
          if (Array.isArray(resp.data.sources)) {
            setSources(resp.data.sources);
          }
          const treeNodes = Array.isArray(resp.data.categoryTree) ? resp.data.categoryTree : [];
          setCategoryTreeNodes(treeNodes);
          const flatList = Array.isArray(resp.data.class) ? resp.data.class : flattenCategoryNodes(treeNodes);
          allCategoryListRef.current = flatList;
          return {
            sources: (resp.data.sources || []) as { id: string; name: string; state?: boolean }[],
            treeNodes,
            allCategories: flatList,
            defaultSourceId: String(resp.data.defaultSourceId || ""),
            currentSourceId: String(resp.data.currentSourceId || targetSourceId || ""),
          };
        }
      } catch {
        message.error("加载采集站分类失败");
      } finally {
        setCategoryLoading(false);
      }
      return null;
    },
    [message],
  );

  const populateForm = useCallback(
    (filmData: any, ownSourceId?: string, categories?: any[]) => {
      if (!filmData) return;
      const desc = filmData.descriptor || {};
      const isCustom = filmData.isCustomPicture === true;
      const displayPic = String(filmData.picture || "").trim();
      const savedCustom = String(filmData.customPicture || "").trim();
      const customPic = isCustom ? savedCustom || displayPic : displayPic;
      const slidePic = filmData.customPictureSlide || filmData.pictureSlide || "";
      const sourceId =
        ownSourceId ||
        filmData.sourceId ||
        filmData.firstSourceId ||
        (filmData.list && filmData.list[0]?.id) ||
        "";
      const cat = resolveCategoryFields(filmData, categories || allCategoryListRef.current);

      form.setFieldsValue({
        id: filmData.id,
        sourceId,
        cid: cat.cid,
        pid: cat.pid,
        cName: cat.cName,
        name: filmData.name,
        followPosterSource: !isCustom,
        picture: customPic,
        pictureSlide: slidePic,
        subTitle: desc.subTitle || "",
        initial: desc.initial || "",
        classTag: desc.classTag || "",
        director: desc.director || "",
        actor: desc.actor || "",
        writer: desc.writer || "",
        remarks: desc.remarks || "",
        releaseDate: desc.releaseDate || "",
        area: desc.area || "",
        lang: desc.language || "",
        year: desc.year || "",
        state: desc.state || "",
        dbId: desc.dbId ?? 0,
        dbScore: desc.dbScore || "",
        hits: desc.hits ?? 0,
        content: desc.content || "",
      });
    },
    [form],
  );

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    form.resetFields();
    setTmdbSnapshot(null);

    if (filmId) {
      setFetching(true);
      ApiGet("/films/play-info", { id: filmId, stored: 1 })
        .then(async (resp: any) => {
          if (cancelled) return;
          if (resp.code === 0 && resp.data?.detail) {
            const filmData = resp.data.detail;
            loadedDetailRef.current = filmData;
            setLoadedDetail(filmData);
            const ownSourceId =
              filmData.sourceId ||
              filmData.firstSourceId ||
              (filmData.list && filmData.list[0]?.id) ||
              "";
            const catData = await loadCategoryTree(ownSourceId);
            if (cancelled) return;
            populateForm(filmData, ownSourceId, catData?.allCategories);
          } else {
            message.error("获取影片详情失败");
          }
        })
        .finally(() => {
          if (!cancelled) setFetching(false);
        });
    } else {
      setFetching(true);
      loadedDetailRef.current = null;
      setLoadedDetail(null);
      loadCategoryTree(defaultSourceId || "")
        .then((data) => {
          if (cancelled) return;
          const resolvedSourceId =
            defaultSourceId || data?.currentSourceId || data?.defaultSourceId || data?.sources?.[0]?.id || "";
          const initialValues: Record<string, any> = {
            sourceId: resolvedSourceId,
            followPosterSource: false,
            dbId: 0,
            hits: 0,
          };
          if (defaultCid && data?.allCategories) {
            const matched = data.allCategories.find((c: any) => Number(c.id) === defaultCid);
            if (matched) {
              initialValues.cid = Number(matched.id);
              initialValues.pid = Number(matched.pid <= 0 ? matched.id : matched.pid);
              initialValues.cName = matched.name;
            }
          }
          form.setFieldsValue(initialValues);
        })
        .finally(() => {
          if (!cancelled) setFetching(false);
        });
    }

    return () => {
      cancelled = true;
    };
  }, [open, filmId, defaultSourceId, defaultCid, form, loadCategoryTree, message, populateForm]);

  const handleSourceChange = async (newSourceId: string) => {
    form.setFieldsValue({
      sourceId: newSourceId,
      cid: undefined,
      pid: 0,
      cName: "",
    });
    await loadCategoryTree(newSourceId);
  };

  const handleClassChange = (value?: number) => {
    if (!value) {
      form.setFieldsValue({ cid: undefined, pid: 0, cName: "" });
      return;
    }
    const selected = allCategoryList.find((c) => c.id === value);
    if (selected) {
      form.setFieldsValue({
        cid: selected.id,
        pid: selected.pid <= 0 ? selected.id : selected.pid,
        cName: selected.name,
      });
    }
  };

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    setTmdbSnapshot((prev) => prev || form.getFieldsValue(true));
    const shouldFill = (name: string, ...aliases: string[]) => {
      if (!fields || fields.length === 0) return true;
      return [name, ...aliases].some((key) => fields.includes(key));
    };

    const updates: Record<string, any> = {};

    // 1. 海报与背景图：填入海报时自动切换为自定义封面（关闭跟随海报源）
    if (shouldFill("poster", "picture") && data.picture) {
      updates.picture = data.picture;
      updates.followPosterSource = false;
    }
    if (shouldFill("backdrop", "pictureSlide") && data.pictureSlide) {
      updates.pictureSlide = data.pictureSlide;
    }

    // 2. 片名与原名/副标题
    if (shouldFill("name", "title") && data.name) {
      updates.name = data.name;
    }
    if (shouldFill("subTitle", "originalTitle") && (data.subTitle || data.originalTitle)) {
      updates.subTitle = data.subTitle || data.originalTitle;
    }

    // 3. 剧情简介 (overview/content/blurb)
    if (shouldFill("overview", "content", "blurb") && (data.content || data.overview || data.blurb)) {
      updates.content = data.content || data.overview || data.blurb;
    }

    // 4. 演职人员
    if (shouldFill("director") && data.director) {
      updates.director = data.director;
    }
    if (shouldFill("actor") && data.actor) {
      updates.actor = data.actor;
    }
    if (shouldFill("writer") && data.writer) {
      updates.writer = data.writer;
    }

    // 5. 年份与上映日期 (year/releaseDate)
    if (shouldFill("year", "releaseDate")) {
      if (data.year) updates.year = String(data.year);
      if (data.releaseDate) {
        updates.releaseDate = String(data.releaseDate);
        if (!updates.year && String(data.releaseDate).length >= 4) {
          updates.year = String(data.releaseDate).slice(0, 4);
        }
      }
    }

    // 6. 评分 (score/dbScore/voteAverage)
    if (shouldFill("score", "dbScore") && (data.dbScore || data.score || data.voteAverage)) {
      const scoreVal = data.dbScore || data.score || (data.voteAverage > 0 ? Number(data.voteAverage).toFixed(1) : "");
      if (scoreVal) {
        updates.dbScore = String(scoreVal);
      }
    }

    // 7. 标签 (tag/classTag/genres)
    if (shouldFill("tag", "classTag") && (data.classTag || data.tag || data.genres)) {
      updates.classTag = data.classTag || data.tag || (Array.isArray(data.genres) ? data.genres.join(",") : "");
    }

    // 8. 地区与语言
    if (shouldFill("area") && data.area) {
      updates.area = data.area;
    }
    if (shouldFill("lang", "language") && (data.lang || data.language)) {
      updates.lang = data.lang || data.language;
    }

    if (Object.keys(updates).length > 0) {
      form.setFieldsValue(updates);
    }
  };

  const handleUndoTmdb = () => {
    if (!tmdbSnapshot) return;
    form.setFieldsValue(tmdbSnapshot);
    setTmdbSnapshot(null);
    message.info("已撤销 TMDB 填充");
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
        form.setFieldValue("picture", resp.data);
        onSuccess?.(resp.data);
      } else {
        message.error(resp.msg);
        onError?.(new Error(resp.msg));
      }
    } catch (err: any) {
      message.error(err.message || "上传图片失败");
      onError?.(err);
    }
  };

  const onFinish = async (values: any) => {
    setSubmitting(true);
    try {
      const isCustom = !values.followPosterSource;
      const customPic = isCustom ? (values.picture || "").trim() : "";
      const sourcePic = loadedDetailRef.current?.picture || "";
      const customSlide = isCustom
        ? (values.pictureSlide || loadedDetailRef.current?.customPictureSlide || "").trim()
        : "";
      const { playLink, playForm, ...restValues } = values;
      const payload: Record<string, any> = {
        ...restValues,
        sourceId: values.sourceId || "",
        language: restValues.lang || restValues.language || "",
        id: filmId ? Number(filmId) : 0,
        dbId: Number(restValues.dbId) || 0,
        hits: Number(restValues.hits) || 0,
        isCustomPicture: isCustom,
        customPicture: customPic,
        picture: isCustom ? (sourcePic || customPic) : "",
        customPictureSlide: customSlide,
      };
      if (!filmId) {
        payload.playLink = (playLink || "").trim();
        payload.playForm = playForm || "";
      }

      const resp = await ApiPost("/manage/film/add", payload);
      if (resp.code === 0) {
        message.success(filmId ? "影视更新成功" : "影片添加成功");
        onSuccess();
        onClose();
      } else {
        message.error(resp.msg || "保存失败");
      }
    } finally {
      setSubmitting(false);
    }
  };

  const handleAfterClose = () => {
    form.resetFields();
    setTmdbSnapshot(null);
    setLoadedDetail(null);
    loadedDetailRef.current = null;
    setPickerOpen(false);
    setTmdbModalOpen(false);
  };

  return (
    <>
      <Modal
        title={
          <div className={styles.modalHeader}>
            <div className={styles.modalTitleRow}>
              <span className={styles.modalTitle}>{filmId ? "编辑影片信息" : "新增录入影片"}</span>
              {filmId && (
                <Tag color="purple" className={styles.idTag}>
                  MID: #{filmId}
                </Tag>
              )}
            </div>
            <span className={styles.modalSubtitle}>
              {filmId
                ? "修改影片基础档案、海报封面、演职阵容与元数据"
                : "录入影片基本信息、封面海报与播放分集地址"}
            </span>
          </div>
        }
        open={open}
        onCancel={() => {
          if (!submitting) onClose();
        }}
        width={920}
        centered
        destroyOnHidden
        afterClose={handleAfterClose}
        footer={[
          <Button key="cancel" size="middle" onClick={onClose} disabled={submitting}>
            取消
          </Button>,
          <Button
            key="submit"
            type="primary"
            size="middle"
            loading={submitting}
            disabled={!canWrite}
            onClick={() => form.submit()}
          >
            {filmId ? "保存修改" : "确认创建"}
          </Button>,
        ]}
      >
        <div className={styles.modalBody}>
          {fetching ? (
            <div className={styles.loadingWrap}>
              <Spin size="large" />
              <Text type="secondary" style={{ fontSize: 13 }}>正在加载影片详情数据...</Text>
            </div>
          ) : (
            <Form
              form={form}
              layout="vertical"
              onFinish={onFinish}
              requiredMark="optional"
              initialValues={{
                sourceId: defaultSourceId || undefined,
                dbId: 0,
                hits: 0,
                followPosterSource: Boolean(filmId),
              }}
            >
              <Form.Item name="pid" hidden>
                <Input />
              </Form.Item>
              <Form.Item name="cName" hidden>
                <Input />
              </Form.Item>

              <Flex vertical gap={18} style={{ width: "100%" }}>
                {/* 1. 基础信息 */}
                <Card
                  className={`${styles.sectionCard} ${styles.cardHeadBlue}`}
                  title={
                    <div className={styles.sectionTitleWrap}>
                      <span className={styles.sectionIconBadge} style={{ background: "rgba(22, 119, 255, 0.16)", color: "#1677ff" }}>
                        <InfoCircleOutlined />
                      </span>
                      <span className={styles.sectionTitle}>基础信息</span>
                      <span className={styles.sectionSub}>档案与分类体系</span>
                    </div>
                  }
                  extra={
                    <Tag color="blue" style={{ margin: 0, borderRadius: 4 }}>
                      基本档案
                    </Tag>
                  }
                  styles={{
                    header: {
                      background: "rgba(255, 255, 255, 0.025)",
                      borderBottom: "1px solid var(--ant-color-border-secondary)",
                      padding: "10px 18px",
                      minHeight: 44,
                    },
                    body: { padding: "16px 18px" },
                  }}
                >
                  <div className={styles.basicInfoLayout}>
                    <div className={styles.posterColumn}>
                      <div className={styles.posterWrapper}>
                        <AntImage
                          width="100%"
                          height="100%"
                          style={{
                            width: "100%",
                            height: "100%",
                            objectFit: "cover",
                            display: "block",
                          }}
                          wrapperStyle={{
                            width: "100%",
                            height: "100%",
                            display: "block",
                          }}
                          src={watchedPicture || loadedDetail?.picture}
                          alt="海报封面"
                          fallback={FALLBACK_IMG}
                          preview={{ mask: "点击放大" }}
                        />
                        {watchedFollowPosterSource && (
                          <div className={styles.posterTagBadge}>
                            已跟随海报源
                          </div>
                        )}
                      </div>
                    </div>

                    <div className={styles.formColumn}>
                      <Row gutter={[16, 12]}>
                        <Col xs={24}>
                          <Form.Item
                            label="影片名称"
                            className={styles.formItemReset}
                            required
                          >
                            <Space.Compact style={{ width: "100%" }}>
                              <Form.Item
                                name="name"
                                noStyle
                                rules={[{ required: true, message: "请输入名称" }]}
                              >
                                <Input placeholder="请输入影片名称" />
                              </Form.Item>
                              {tmdbEnabled ? (
                                <Button
                                  icon={<CompassOutlined />}
                                  style={{ color: "#722ed1", borderColor: "#722ed1", fontWeight: 500 }}
                                  onClick={() => setTmdbModalOpen(true)}
                                  disabled={!canWrite}
                                >
                                  TMDB 刮削
                                </Button>
                              ) : null}
                              {tmdbSnapshot && (
                                <Button icon={<UndoOutlined />} onClick={handleUndoTmdb}>
                                  撤销填入
                                </Button>
                              )}
                            </Space.Compact>
                          </Form.Item>
                        </Col>
                        <Col xs={24} sm={12}>
                          <Form.Item label="影片别名" name="subTitle" className={styles.formItemReset}>
                            <Input placeholder="如: 英文名、又名" />
                          </Form.Item>
                        </Col>
                        <Col xs={24} sm={12}>
                          <Form.Item
                            label="归属采集站"
                            name="sourceId"
                            className={styles.formItemReset}
                            rules={[{ required: true, message: "请选择归属采集站" }]}
                            tooltip={filmId ? "已建档影片归属站已锁定" : "决定影片收录的采集源与分类体系"}
                          >
                            <Select
                              style={{ width: "100%" }}
                              placeholder="选择采集站"
                              onChange={handleSourceChange}
                              disabled={Boolean(filmId)}
                              options={sources.map((s) => ({
                                label: s.state === false ? `${s.name} (已停用)` : s.name,
                                value: s.id,
                              }))}
                            />
                          </Form.Item>
                        </Col>
                        <Col xs={24} sm={12}>
                          <Form.Item
                            label="所属分类"
                            name="cid"
                            className={styles.formItemReset}
                            rules={[{ required: true, message: "请选择分类" }]}
                          >
                            <TreeSelect
                              style={{ width: "100%" }}
                              placeholder={categoryLoading ? "分类加载中..." : "请选择分类"}
                              loading={categoryLoading}
                              disabled={categoryLoading}
                              onChange={handleClassChange}
                              treeData={categoryTreeData}
                              treeDefaultExpandAll
                              treeLine={{ showLeafIcon: false }}
                              popupMatchSelectWidth={false}
                              allowClear
                              showSearch
                              treeNodeFilterProp="title"
                              styles={{ popup: { root: { minWidth: 240, maxHeight: 380, overflow: "auto" } } }}
                            />
                          </Form.Item>
                        </Col>
                        <Col xs={24} sm={12}>
                          <Form.Item
                            label="是否跟随海报源"
                            name="followPosterSource"
                            className={styles.formItemReset}
                            tooltip="开启后使用采集源最新封面；关闭后支持自定义封面且采集不覆盖。"
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
                              className={styles.formItemReset}
                              tooltip="自定义封面不会被采集任务覆盖。"
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
                                <Upload
                                  customRequest={customUpload}
                                  showUploadList={false}
                                  accept={IMAGE_UPLOAD_ACCEPT}
                                  disabled={!canWrite}
                                >
                                  <Button icon={<UploadOutlined />}>上传封面</Button>
                                </Upload>
                                <Button
                                  icon={<PictureOutlined />}
                                  disabled={!canWrite}
                                  onClick={() => setPickerOpen(true)}
                                >
                                  选图
                                </Button>
                              </Space.Compact>
                            </Form.Item>
                          </Col>
                        ) : (
                          <Col xs={24}>
                            <div className={styles.currentCoverNotice}>
                              <span className={styles.coverNoticeLabel}>当前跟随采集海报源图片地址：</span>
                              <span className={styles.coverNoticeText}>
                                {String(loadedDetail?.picture || "").trim() || "暂无采集封面地址"}
                              </span>
                            </div>
                          </Col>
                        )}
                      </Row>
                    </div>
                  </div>
                </Card>

                {/* 2. 演职人员 */}
                <Card
                  className={`${styles.sectionCard} ${styles.cardHeadPurple}`}
                  title={
                    <div className={styles.sectionTitleWrap}>
                      <span className={styles.sectionIconBadge} style={{ background: "rgba(114, 46, 209, 0.16)", color: "#722ed1" }}>
                        <UserOutlined />
                      </span>
                      <span className={styles.sectionTitle}>演职阵容</span>
                      <span className={styles.sectionSub}>导演、主演与编剧</span>
                    </div>
                  }
                  extra={
                    <Tag color="purple" style={{ margin: 0, borderRadius: 4 }}>
                      主创阵容
                    </Tag>
                  }
                  styles={{
                    header: {
                      background: "rgba(255, 255, 255, 0.025)",
                      borderBottom: "1px solid var(--ant-color-border-secondary)",
                      padding: "10px 18px",
                      minHeight: 44,
                    },
                    body: { padding: "16px 18px" },
                  }}
                >
                  <Row gutter={[16, 12]}>
                    <Col xs={24} sm={8}>
                      <Form.Item label="导演" name="director" className={styles.formItemReset}>
                        <Input placeholder="多个以逗号分隔" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="主演" name="actor" className={styles.formItemReset}>
                        <Input placeholder="多个以逗号分隔" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="作者/编剧" name="writer" className={styles.formItemReset}>
                        <Input placeholder="多个以逗号分隔" />
                      </Form.Item>
                    </Col>
                  </Row>
                </Card>

                {/* 3. 发行与元数据 */}
                <Card
                  className={`${styles.sectionCard} ${styles.cardHeadCyan}`}
                  title={
                    <div className={styles.sectionTitleWrap}>
                      <span className={styles.sectionIconBadge} style={{ background: "rgba(19, 194, 194, 0.16)", color: "#13c2c2" }}>
                        <GlobalOutlined />
                      </span>
                      <span className={styles.sectionTitle}>发行与元数据</span>
                      <span className={styles.sectionSub}>年份、地区、标签与首字母</span>
                    </div>
                  }
                  extra={
                    <Tag color="cyan" style={{ margin: 0, borderRadius: 4 }}>
                      发行档案
                    </Tag>
                  }
                  styles={{
                    header: {
                      background: "rgba(255, 255, 255, 0.025)",
                      borderBottom: "1px solid var(--ant-color-border-secondary)",
                      padding: "10px 18px",
                      minHeight: 44,
                    },
                    body: { padding: "16px 18px" },
                  }}
                >
                  <Row gutter={[16, 12]}>
                    <Col xs={24} sm={8}>
                      <Form.Item label="上映日期" name="releaseDate" className={styles.formItemReset}>
                        <Input placeholder="YYYY-MM-DD" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="制作地区" name="area" className={styles.formItemReset}>
                        <Input placeholder="如: 中国大陆, 美国" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="语言" name="lang" className={styles.formItemReset}>
                        <Input placeholder="如: 国语, 英语" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="上映年份" name="year" className={styles.formItemReset}>
                        <Input placeholder="YYYY" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="检索首字母" name="initial" className={styles.formItemReset}>
                        <Input placeholder="大写字母" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="剧情标签" name="classTag" className={styles.formItemReset}>
                        <Input placeholder="如: 动作, 冒险" />
                      </Form.Item>
                    </Col>
                  </Row>
                </Card>

                {/* 4. 状态与外部数据 */}
                <Card
                  className={`${styles.sectionCard} ${styles.cardHeadOrange}`}
                  title={
                    <div className={styles.sectionTitleWrap}>
                      <span className={styles.sectionIconBadge} style={{ background: "rgba(250, 140, 22, 0.16)", color: "#fa8c16" }}>
                        <DatabaseOutlined />
                      </span>
                      <span className={styles.sectionTitle}>状态与外部数据</span>
                      <span className={styles.sectionSub}>更新状态、热度与豆瓣评分</span>
                    </div>
                  }
                  extra={
                    <Tag color="orange" style={{ margin: 0, borderRadius: 4 }}>
                      指标状态
                    </Tag>
                  }
                  styles={{
                    header: {
                      background: "rgba(255, 255, 255, 0.025)",
                      borderBottom: "1px solid var(--ant-color-border-secondary)",
                      padding: "10px 18px",
                      minHeight: 44,
                    },
                    body: { padding: "16px 18px" },
                  }}
                >
                  <Row gutter={[16, 12]}>
                    <Col xs={24} sm={8}>
                      <Form.Item label="更新备注" name="remarks" className={styles.formItemReset}>
                        <Input placeholder="如: 完结, 第10集" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="影片状态" name="state" className={styles.formItemReset}>
                        <Input placeholder="如: 正片, 预告" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="影片热度" name="hits" className={styles.formItemReset}>
                        <InputNumber style={{ width: "100%" }} placeholder="0" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="播放来源标识" name="playForm" className={styles.formItemReset}>
                        <Input placeholder="如: m3u8_list" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="豆瓣 ID" name="dbId" className={styles.formItemReset}>
                        <InputNumber style={{ width: "100%" }} placeholder="0" />
                      </Form.Item>
                    </Col>
                    <Col xs={24} sm={8}>
                      <Form.Item label="豆瓣评分" name="dbScore" className={styles.formItemReset}>
                        <Input placeholder="如: 8.5" />
                      </Form.Item>
                    </Col>
                  </Row>
                </Card>

                {/* 5. 剧情详情 */}
                <Card
                  className={`${styles.sectionCard} ${styles.cardHeadGeekblue}`}
                  title={
                    <div className={styles.sectionTitleWrap}>
                      <span className={styles.sectionIconBadge} style={{ background: "rgba(47, 84, 235, 0.16)", color: "#2f54eb" }}>
                        <ContainerOutlined />
                      </span>
                      <span className={styles.sectionTitle}>剧情简介</span>
                      <span className={styles.sectionSub}>影片故事梗概与完整描述</span>
                    </div>
                  }
                  extra={
                    <Tag color="geekblue" style={{ margin: 0, borderRadius: 4 }}>
                      图文详情
                    </Tag>
                  }
                  styles={{
                    header: {
                      background: "rgba(255, 255, 255, 0.025)",
                      borderBottom: "1px solid var(--ant-color-border-secondary)",
                      padding: "10px 18px",
                      minHeight: 44,
                    },
                    body: { padding: "16px 18px" },
                  }}
                >
                  <Form.Item name="content" noStyle>
                    <TextArea rows={4} placeholder="输入剧情详细描述..." style={{ resize: "vertical" }} />
                  </Form.Item>
                </Card>

                {/* 6. 播放资源（仅新增时展示） */}
                {!filmId && (
                  <Card
                    className={`${styles.sectionCard} ${styles.cardHeadGreen}`}
                    title={
                      <div className={styles.sectionTitleWrap}>
                        <span className={styles.sectionIconBadge} style={{ background: "rgba(82, 196, 26, 0.16)", color: "#52c41a" }}>
                          <PlayCircleOutlined />
                        </span>
                        <span className={styles.sectionTitle}>播放资源</span>
                        <span className={styles.sectionSub}>初次录入分集播放地址</span>
                      </div>
                    }
                    extra={
                      <Tag color="green" style={{ margin: 0, borderRadius: 4 }}>
                        播放直链 (必填)
                      </Tag>
                    }
                    styles={{
                      header: {
                        background: "rgba(255, 255, 255, 0.025)",
                        borderBottom: "1px solid var(--ant-color-border-secondary)",
                        padding: "10px 18px",
                        minHeight: 44,
                      },
                      body: { padding: "16px 18px" },
                    }}
                  >
                    <Alert
                      type="info"
                      showIcon
                      className={styles.playResourceAlert}
                      title="格式规范"
                      description="格式为: 章节名称$视频播放地址，多集之间使用井号 (#) 隔开。"
                    />
                    <Form.Item
                      label="分集播放链接"
                      name="playLink"
                      style={{ marginBottom: 0 }}
                      rules={[{ required: true, message: "新增影片必须录入至少一条有效播放地址" }]}
                    >
                      <TextArea
                        rows={4}
                        placeholder="示例: &#10;第01集$https://example.com/play/1.m3u8#第02集$https://example.com/play/2.m3u8"
                        style={{ resize: "vertical", fontFamily: "monospace", fontSize: 13 }}
                      />
                    </Form.Item>
                  </Card>
                )}
              </Flex>
            </Form>
          )}
        </div>
      </Modal>

      <ImagePicker
        open={pickerOpen}
        onCancel={() => setPickerOpen(false)}
        onSelect={(link) => {
          form.setFieldValue("picture", link);
          setPickerOpen(false);
        }}
      />

      <TmdbModal
        open={tmdbModalOpen}
        mid={filmId ? Number(filmId) : undefined}
        initialName={form.getFieldValue("name")}
        onClose={() => setTmdbModalOpen(false)}
        onPrefill={handleTmdbPrefill}
      />
    </>
  );
}
