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
import { API_PRIMARY_MISMATCH, ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { usePrimarySource } from "@/lib/manage-primary";
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
  isSourcePlayLine,
  resolveCategoryFields,
  renderDrawerRequiredMark,
  formatEpisodesToPlayLink,
  mergePlayLinesForSave,
  buildFilmDetailFormValues,
} from "./utils";
import type { FilmDrawerProps, PlayLine } from "./types";
import styles from "./index.module.less";

export default function FilmDrawer({
  open,
  filmId,
  defaultCid,
  onClose,
  onSuccess,
}: FilmDrawerProps) {
  const [form] = Form.useForm();
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const { primary, refresh: refreshPrimary } = usePrimarySource();
  const primaryId = primary?.id || "";
  const previousPrimaryIdRef = useRef(primaryId);
  const tmdbEnabled = useTmdbEnabled();

  const [categoryTreeNodes, setCategoryTreeNodes] = useState<any[]>([]);
  const [categoryLoading, setCategoryLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [playLines, setPlayLines] = useState<PlayLine[]>([]);
  const [lineLinksMap, setLineLinksMap] = useState<Record<string, string>>({});
  const [lineNamesMap, setLineNamesMap] = useState<Record<string, string>>({});

  const [pickerOpen, setPickerOpen] = useState(false);
  const [tmdbModalOpen, setTmdbModalOpen] = useState(false);
  const [tmdbSnapshot, setTmdbSnapshot] = useState<Record<string, any> | null>(null);

  const loadedDetailRef = useRef<any>(null);
  const [loadedDetail, setLoadedDetail] = useState<any>(null);
  const [isManualFilm, setIsManualFilm] = useState(false);
  const allCategoryListRef = useRef<any[]>([]);

  const watchedFollowPosterSource = Form.useWatch("followPosterSource", form);
  const watchedPicture = Form.useWatch("picture", form);

  const allCategoryList = useMemo(() => flattenCategoryNodes(categoryTreeNodes), [categoryTreeNodes]);
  allCategoryListRef.current = allCategoryList;
  const categoryTreeData = useMemo(() => {
    const base = formatCategoryNodes(categoryTreeNodes, true);
    if (!loadedDetail) return base;
    const cat = resolveCategoryFields(loadedDetail, allCategoryList);
    if (cat.cid && !allCategoryList.some((c) => Number(c.id) === Number(cat.cid))) {
      return [
        ...base,
        {
          title: `${cat.cName || `分类 #${cat.cid}`}（已隐藏）`,
          label: `${cat.cName || `分类 #${cat.cid}`}（已隐藏）`,
          value: Number(cat.cid),
          key: String(cat.cid),
        },
      ];
    }
    return base;
  }, [allCategoryList, categoryTreeNodes, loadedDetail]);

  const loadCategoryTree = useCallback(
    async () => {
      if (!primaryId) {
        setCategoryTreeNodes([]);
        return null;
      }
      setCategoryLoading(true);
      try {
        const resp = await ApiGet("/manage/film/search/options");
        if (resp.code === 0 && resp.data) {
          if (
            typeof resp.data.primarySourceId === "string" &&
            resp.data.primarySourceId !== primaryId
          ) {
            void refreshPrimary();
            return null;
          }
          const treeNodes = Array.isArray(resp.data.categoryTree) ? resp.data.categoryTree : [];
          setCategoryTreeNodes(treeNodes);
          const flatList = Array.isArray(resp.data.class) ? resp.data.class : flattenCategoryNodes(treeNodes);
          allCategoryListRef.current = flatList;
          return {
            primarySourceId: resp.data.primarySourceId || primaryId,
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
    [message, primaryId, refreshPrimary],
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
      const allLines = Array.isArray(filmData.list) ? (filmData.list as PlayLine[]) : [];
      const lines = allLines.filter((line) => isSourcePlayLine(line, primaryId));
      setPlayLines(lines);
      const initialLinks: Record<string, string> = {};
      const initialNames: Record<string, string> = {};
      lines.forEach((l) => {
        const k = l.id || l.name;
        initialLinks[k] = formatEpisodesToPlayLink(l.linkList || []);
        initialNames[k] = l.name || l.id || "";
      });
      setLineLinksMap(initialLinks);
      setLineNamesMap(initialNames);
      const ownerLine = lines.find((l) => isSourcePlayLine(l, primaryId)) || lines[0];

      const formattedPlayLink =
        ownerLine && ownerLine.linkList && ownerLine.linkList.length > 0
          ? formatEpisodesToPlayLink(ownerLine.linkList)
          : "";

      form.setFieldsValue({
        ...buildFilmDetailFormValues(
          filmData,
          cat,
          isCustom,
          customPic,
          slidePic,
          ownerLine,
          formattedPlayLink,
        ),
        sourceId: primaryId,
      });
    },
    [form, primaryId],
  );

  useEffect(() => {
    if (previousPrimaryIdRef.current !== primaryId) {
      if (open && previousPrimaryIdRef.current && form.isFieldsTouched()) {
        message.warning("主站已变更，未保存的影片编辑已重新加载");
      }
      previousPrimaryIdRef.current = primaryId;
    }
    if (!open) return;
    if (!primaryId) {
      setFetching(false);
      setCategoryTreeNodes([]);
      return;
    }
    let cancelled = false;
    setFetching(true);
    setTmdbSnapshot(null);

    if (filmId) {
      ApiGet("/manage/film/detail", { id: filmId })
        .then(async (resp) => {
          if (cancelled) return;
          if (resp.code === 0 && resp.data) {
            const filmData = resp.data.detail || resp.data;
            setIsManualFilm(Boolean(resp.data.isManualFilm));
            loadedDetailRef.current = filmData;
            setLoadedDetail(filmData);

            const optionsData = await loadCategoryTree();
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
      setIsManualFilm(true);
      setPlayLines([]);
      loadCategoryTree()
        .then((data) => {
          if (cancelled) return;
          const initialValues: Record<string, any> = {
            sourceId: primaryId,
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
  }, [open, filmId, primaryId, defaultCid, form, loadCategoryTree, message, populateForm]);

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
      ["dbScore", ["score", "dbScore"], data.dbScore],
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
        sourceId: primaryId,
        language: restValues.language || "",
        id: filmId ? Number(filmId) : 0,
        dbId: Number(restValues.dbId) || 0,
        hits: Number(restValues.hits) || 0,
        isCustomPicture: isCustom,
        customPicture: customPic,
        picture: isCustom ? (sourcePic || customPic) : (sourcePic || customPic),
        customPictureSlide: customSlide,
      };

      if (!filmId || isManualFilm) {
        const targetSourceId = String(payload.sourceId || loadedDetailRef.current?.firstSourceId || "");
        const ownerLines = playLines.filter((l) => !targetSourceId || isSourcePlayLine(l, targetSourceId));
        const mergedPlay = mergePlayLinesForSave(ownerLines, lineLinksMap, values.playLink, playForm, lineNamesMap);
        if (!String(mergedPlay.playLink || "").trim()) {
          message.error("请至少输入一条有效播放地址");
          setSubmitting(false);
          return;
        }
        Object.assign(payload, mergedPlay);
        if (filmId) {
          payload.manualLinesUpdate = true;
        }
      }
      const resp = await ApiPost("/manage/film/save", payload);
      if (resp.code === 0) {
        message.success(filmId ? "更新成功" : "添加成功");
        onSuccess();
        onClose();
      } else if (resp.code === API_PRIMARY_MISMATCH) {
        message.warning(resp.msg || "主站已变更，请刷新后重试");
        void refreshPrimary();
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
    setIsManualFilm(false);
    setPlayLines([]);
    setLineLinksMap({});
    setLineNamesMap({});
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
              sourceId: primaryId || undefined,
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
                onClassChange={handleClassChange}
              />

              <FilmMetaSections />

              <FilmPlaySection
                lines={playLines}
                filmId={filmId}
                editable={!filmId || isManualFilm}
                ownerSourceId={primaryId}
                lineLinks={lineLinksMap}
                lineNames={lineNamesMap}
                onLineLinkChange={(lineKey, newLink) => {
                  setLineLinksMap((prev) => ({ ...prev, [lineKey]: newLink }));
                }}
                onLineNameChange={(lineKey, newName) => {
                  setLineNamesMap((prev) => ({ ...prev, [lineKey]: newName }));
                }}
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
