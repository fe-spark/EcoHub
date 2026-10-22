"use client";

import React, { useState, useEffect, useMemo, useRef, useCallback } from "react";
import {
  Drawer,
  Form,
  Input,
  Button,
  Tag,
  Spin,
  Flex,
  Space,
} from "antd";
import { VideoCameraOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import { isAllowedImageFile } from "@/lib/imageUpload";
import TmdbModal from "../tmdb-modal";
import ImagePicker from "@/app/manage/components/image-picker";
import FilmBasicSection from "./film-basic-section";
import FilmMetaSections from "./film-meta-sections";
import FilmPlaySection from "./film-play-section";
import {
  formatCategoryNodes,
  flattenCategoryNodes,
  resolveCategoryFields,
  renderDrawerRequiredMark,
  formatEpisodesToPlayLink,
} from "./utils";
import type { FilmDrawerProps, PlayLine } from "./types";
import styles from "./index.module.less";

export default function FilmDrawer({
  open,
  filmId,
  defaultSourceId,
  defaultCid,
  onClose,
  onSuccess,
}: FilmDrawerProps) {
  const [form] = Form.useForm();
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const tmdbEnabled = useTmdbEnabled();

  const [sources, setSources] = useState<{ id: string; name: string; state?: boolean }[]>([]);
  const [categoryTreeNodes, setCategoryTreeNodes] = useState<any[]>([]);
  const [categoryLoading, setCategoryLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [playLines, setPlayLines] = useState<PlayLine[]>([]);

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
            sources: resp.data.sources,
            currentSourceId: resp.data.currentSourceId,
            defaultSourceId: resp.data.defaultSourceId,
            allCategories: flatList,
          };
        }
      } catch (err: any) {
        message.error(err.message || "加载失败");
      } finally {
        setCategoryLoading(false);
      }
      return null;
    },
    [message],
  );

  const populateForm = useCallback(
    (filmData: any, categoryList: any[]) => {
      if (!filmData) return;
      const desc = filmData.descriptor || {};
      const isCustom = filmData.isCustomPicture === true;
      const displayPic = String(filmData.picture || "").trim();
      const savedCustom = String(filmData.customPicture || "").trim();
      const customPic = isCustom ? savedCustom || displayPic : displayPic;
      const slidePic = filmData.customPictureSlide || filmData.pictureSlide || "";
      const cat = resolveCategoryFields(filmData, categoryList || allCategoryListRef.current);
      const lines = Array.isArray(filmData.list) ? (filmData.list as PlayLine[]) : [];
      setPlayLines(lines);

      const targetSourceId = String(filmData.sourceId || filmData.firstSourceId || "");
      const ownerLine =
        lines.find(
          (l) =>
            targetSourceId &&
            (l.sourceId === targetSourceId || l.id?.startsWith(targetSourceId)),
        ) || lines[0];

      const formattedPlayLink =
        ownerLine && ownerLine.linkList && ownerLine.linkList.length > 0
          ? formatEpisodesToPlayLink(ownerLine.linkList)
          : "";

      form.setFieldsValue({
        id: filmData.id,
        sourceId: filmData.sourceId || "",
        name: filmData.name || "",
        subTitle: desc.subTitle || filmData.subTitle || "",
        cid: cat.cid,
        pid: cat.pid,
        cName: cat.cName,
        followPosterSource: !isCustom,
        picture: customPic,
        pictureSlide: slidePic,
        director: desc.director || filmData.director || "",
        actor: desc.actor || filmData.actor || "",
        writer: desc.writer || filmData.writer || "",
        releaseDate: desc.releaseDate || filmData.releaseDate || "",
        area: desc.area || filmData.area || "",
        language: desc.language || filmData.language || "",
        year: desc.year || (filmData.year ? String(filmData.year) : "") || "",
        initial: desc.initial || filmData.initial || "",
        classTag: desc.classTag || filmData.classTag || "",
        remarks: desc.remarks || filmData.remarks || "",
        state: desc.state || filmData.state || "",
        hits: Number(desc.hits || filmData.hits) || 0,
        playForm: ownerLine?.name || ownerLine?.id || "",
        playLink: formattedPlayLink,
        dbId: Number(desc.dbId || filmData.dbId) || 0,
        dbScore: desc.dbScore || (filmData.score ? String(filmData.score) : "") || "",
        content: desc.content || filmData.content || desc.blurb || filmData.blurb || "",
      });
    },
    [form],
  );

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setFetching(true);
    setTmdbSnapshot(null);

    if (filmId) {
      ApiGet("/films/play-info", { id: filmId, stored: 1 })
        .then(async (resp) => {
          if (cancelled) return;
          if (resp.code === 0 && resp.data) {
            const filmData = resp.data.detail || resp.data;
            loadedDetailRef.current = filmData;
            setLoadedDetail(filmData);

            const optionsData = await loadCategoryTree(filmData.sourceId || "");
            if (cancelled) return;
            const categoryList =
              optionsData?.allCategories || allCategoryListRef.current || [];
            populateForm(filmData, categoryList);
          } else {
            message.error(resp.msg || "获取失败");
          }
        })
        .catch((err) => {
          if (!cancelled) message.error(err.message || "请求失败");
        })
        .finally(() => {
          if (!cancelled) setFetching(false);
        });
    } else {
      loadedDetailRef.current = null;
      setLoadedDetail(null);
      setPlayLines([]);
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
    const selected = allCategoryList.find((c) => Number(c.id) === Number(value));
    if (selected) {
      form.setFieldsValue({
        cid: Number(selected.id),
        pid: Number(selected.pid <= 0 ? selected.id : selected.pid),
        cName: selected.name,
      });
    }
  };

  const handleTmdbPrefill = (data: Record<string, unknown>, fields?: string[]) => {
    setTmdbSnapshot((prev) => prev || form.getFieldsValue(true));
    const shouldFill = (name: string, ...aliases: string[]) =>
      !fields?.length || [name, ...aliases].some((key) => fields.includes(key));

    const updates: Record<string, unknown> = {};
    if (shouldFill("poster", "picture") && data.picture) {
      updates.picture = data.picture;
      updates.followPosterSource = false;
    }
    if (shouldFill("backdrop", "pictureSlide") && data.pictureSlide) {
      updates.pictureSlide = data.pictureSlide;
    }
    const textFields: [string, string[], unknown][] = [
      ["name", ["name", "title"], data.name],
      ["subTitle", ["subTitle", "originalTitle"], data.subTitle || data.originalTitle],
      ["releaseDate", ["releaseDate"], data.releaseDate],
      ["year", ["year"], data.year ? String(data.year) : undefined],
      ["area", ["area", "country"], data.area],
      ["language", ["lang", "language"], data.language || data.lang],
      ["director", ["director"], data.director],
      ["actor", ["actor"], data.actor],
      ["writer", ["writer"], data.writer],
      ["classTag", ["classTag", "genres"], data.classTag],
      ["content", ["content", "overview"], data.content || data.overview],
    ];
    for (const [key, aliases, val] of textFields) {
      if (val && shouldFill(key, ...aliases)) {
        updates[key] = val;
      }
    }
    form.setFieldsValue(updates);
    message.success("预填完成");
  };

  const handleUndoTmdb = () => {
    if (!tmdbSnapshot) return;
    form.setFieldsValue(tmdbSnapshot);
    setTmdbSnapshot(null);
    message.info("已撤销");
  };

  const customUpload = async (options: any) => {
    const { file, onSuccess, onError } = options;
    if (!isAllowedImageFile(file)) {
      message.error("格式不支持");
      onError?.(new Error("格式不支持"));
      return;
    }
    const formData = new FormData();
    formData.append("file", file);
    try {
      const resp = await ApiPost("/manage/file/upload", formData);
      if (resp.code === 0) {
        message.success(resp.msg || "上传成功");
        form.setFieldValue("picture", resp.data);
        onSuccess?.(resp.data);
      } else {
        message.error(resp.msg || "上传失败");
        onError?.(new Error(resp.msg));
      }
    } catch (err: any) {
      message.error(err.message || "上传失败");
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
        sourceId: values.sourceId || loadedDetailRef.current?.sourceId || defaultSourceId || "",
        language: restValues.language || "",
        id: filmId ? Number(filmId) : 0,
        dbId: Number(restValues.dbId) || 0,
        hits: Number(restValues.hits) || 0,
        isCustomPicture: isCustom,
        customPicture: customPic,
        picture: isCustom ? (sourcePic || customPic) : (sourcePic || customPic),
        customPictureSlide: customSlide,
      };

      if (!filmId && (!values.playLink || !values.playLink.trim())) {
        message.error("请至少输入一条有效播放地址");
        setSubmitting(false);
        return;
      }
      if (values.playLink && values.playLink.trim()) {
        payload.playLink = values.playLink.trim();
        payload.playForm = (playForm || "").trim();
      }
      const resp = await ApiPost("/manage/film/save", payload);
      if (resp.code === 0) {
        message.success(filmId ? "更新成功" : "添加成功");
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
    setPlayLines([]);
    loadedDetailRef.current = null;
    setPickerOpen(false);
    setTmdbModalOpen(false);
  };

  return (
    <>
      <Drawer
        open={open}
        onClose={() => {
          if (!submitting) onClose();
        }}
        size={800}
        destroyOnClose
        afterOpenChange={(visible) => {
          if (!visible) handleAfterClose();
        }}
        title={
          <Space size={10} align="center">
            <VideoCameraOutlined style={{ color: "var(--ant-color-primary)", fontSize: 18 }} />
            <span style={{ fontSize: 16, fontWeight: 600 }}>{filmId ? "编辑影片" : "新增影片"}</span>
            {filmId && (
              <Tag color="purple" style={{ borderRadius: 4, margin: 0, fontWeight: 600 }}>
                MID: #{filmId}
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
              disabled={!canWrite}
              onClick={() => form.submit()}
            >
              {filmId ? "保存" : "创建"}
            </Button>
          </Space>
        }
      >
        {fetching ? (
          <div className={styles.loadingWrap}>
            <Spin size="large" />
          </div>
        ) : (
          <Form
            form={form}
            layout="vertical"
            onFinish={onFinish}
            requiredMark={renderDrawerRequiredMark}
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
            <Form.Item name="pictureSlide" hidden>
              <Input />
            </Form.Item>

            <div className={styles.drawerStack}>
              <FilmBasicSection
                form={form}
                filmId={filmId}
                sources={sources}
                categoryTreeData={categoryTreeData}
                categoryLoading={categoryLoading}
                tmdbEnabled={tmdbEnabled}
                tmdbSnapshot={tmdbSnapshot}
                canWrite={canWrite}
                watchedPicture={watchedPicture}
                watchedFollowPosterSource={watchedFollowPosterSource}
                loadedDetail={loadedDetail}
                onOpenTmdb={() => setTmdbModalOpen(true)}
                onUndoTmdb={handleUndoTmdb}
                onOpenPicker={() => setPickerOpen(true)}
                onCustomUpload={customUpload}
                onSourceChange={handleSourceChange}
                onClassChange={handleClassChange}
              />

              <FilmMetaSections />

              <FilmPlaySection
                lines={playLines}
                filmId={filmId}
                ownerSourceId={form.getFieldValue("sourceId") || loadedDetail?.sourceId || ""}
              />
            </div>
          </Form>
        )}
      </Drawer>

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
        initialName={form.getFieldValue("name")}
        onClose={() => setTmdbModalOpen(false)}
        onPrefill={handleTmdbPrefill}
      />
    </>
  );
}
