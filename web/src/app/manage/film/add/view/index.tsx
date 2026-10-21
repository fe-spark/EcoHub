"use client";

import React, { useState, useEffect, useMemo, useRef, Suspense, useCallback } from "react";
import { useSearchParams, useRouter } from "next/navigation";
import { Form, Space, Spin } from "antd";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import UnsavedChangesBar from "@/app/manage/components/unsaved-changes-bar";
import { isFormValuesEqual } from "@/lib/deep-equal";
import ImagePicker from "@/app/manage/components/image-picker";
import TmdbModal from "../../components/tmdb-modal";
import { useTmdbEnabled } from "@/lib/useTmdbEnabled";
import { isAllowedImageFile } from "@/lib/imageUpload";
import FilmBasicCard from "./film-basic-card";
import FilmMetaCards from "./film-meta-cards";
import { formatCategoryNodes, flattenCategoryNodes, resolveCategoryFields } from "./film-add-utils";
import styles from "./index.module.less";

function FilmAddForm() {
  const [form] = Form.useForm();
  const tmdbEnabled = useTmdbEnabled();
  const [sources, setSources] = useState<{ id: string; name: string; state?: boolean }[]>([]);
  const [categoryTreeNodes, setCategoryTreeNodes] = useState<any[]>([]);
  const [categoryLoading, setCategoryLoading] = useState(false);
  const [loading, setLoading] = useState(false);
  const [fetching, setFetching] = useState(false);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [tmdbModalOpen, setTmdbModalOpen] = useState(false);
  const [tmdbSnapshot, setTmdbSnapshot] = useState<Record<string, any> | null>(null);
  const searchParams = useSearchParams();
  const router = useRouter();
  const id = searchParams.get("id");
  const readOnly = Boolean(id) && searchParams.get("readonly") === "1";
  const urlSourceId = searchParams.get("sourceId") || "";
  const urlCid = searchParams.get("cid") ? Number(searchParams.get("cid")) : undefined;
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
    setIsDirty(!isFormValuesEqual(current, baselineRef.current));
  }, [form]);

  const categoryTreeData = useMemo(() => formatCategoryNodes(categoryTreeNodes), [categoryTreeNodes]);
  const allCategoryList = useMemo(() => flattenCategoryNodes(categoryTreeNodes), [categoryTreeNodes]);
  const allCategoryListRef = useRef<any[]>([]);
  allCategoryListRef.current = allCategoryList;

  const loadCategoryTree = useCallback(async (targetSourceId: string) => {
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
  }, [message]);

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
      baselineRef.current = form.getFieldsValue();
      setIsDirty(false);
    },
    [form],
  );

  const handleTmdbPrefill = (data: any, fields?: string[]) => {
    setTmdbSnapshot((prev) => prev || form.getFieldsValue());
    const shouldFill = (name: string, ...aliases: string[]) => {
      if (!fields || fields.length === 0) return true;
      return [name, ...aliases].some((key) => fields.includes(key));
    };

    const updates: Record<string, any> = {};
    if (!form.getFieldValue("name") && data.name) updates.name = data.name;

    const mapping: Array<[string, any, string[]?]> = [
      ["picture", data.picture, ["poster"]],
      ["pictureSlide", data.pictureSlide, ["backdrop"]],
      ["subTitle", data.subTitle],
      ["director", data.director],
      ["actor", data.actor],
      ["writer", data.writer],
      ["releaseDate", data.releaseDate],
      ["year", data.year],
      ["area", data.area],
      ["lang", data.language, ["language"]],
      ["content", data.content, ["blurb"]],
      ["classTag", data.classTag],
    ];
    for (const [key, val, aliases] of mapping) {
      if (val && shouldFill(key, ...(aliases || []))) {
        updates[key] = val;
      }
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
    message.info("已撤销 TMDB 填充");
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
    let cancelled = false;
    if (!id) {
      loadCategoryTree(urlSourceId).then((data) => {
        if (cancelled) return;
        const resolvedSourceId =
          urlSourceId || data?.currentSourceId || data?.defaultSourceId || data?.sources?.[0]?.id || "";
        const initialValues: Record<string, any> = {
          sourceId: resolvedSourceId,
          followPosterSource: false,
        };
        if (urlCid && data?.allCategories) {
          const matched = data.allCategories.find((c: any) => Number(c.id) === urlCid);
          if (matched) {
            initialValues.cid = Number(matched.id);
            initialValues.pid = Number(matched.pid <= 0 ? matched.id : matched.pid);
            initialValues.cName = matched.name;
          }
        }
        form.setFieldsValue(initialValues);
        baselineRef.current = form.getFieldsValue();
        setIsDirty(false);
      });
    } else {
      setFetching(true);
      ApiGet(`/films/play-info`, { id, stored: 1 })
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
    }
    return () => {
      cancelled = true;
    };
  }, [id, urlSourceId, urlCid, form, loadCategoryTree, message, populateForm]);

  const handleSourceChange = async (newSourceId: string) => {
    form.setFieldsValue({
      sourceId: newSourceId,
      cid: undefined,
      pid: 0,
      cName: "",
    });
    checkDirty();
    await loadCategoryTree(newSourceId);
  };

  const handleClassChange = (value?: number) => {
    if (!value) {
      form.setFieldsValue({ cid: undefined, pid: 0, cName: "" });
      checkDirty();
      return;
    }
    const selected = allCategoryList.find((c) => c.id === value);
    if (selected) {
      form.setFieldsValue({
        cid: selected.id,
        pid: selected.pid <= 0 ? selected.id : selected.pid,
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
      const { playLink, playForm, ...restValues } = values;
      const payload: Record<string, any> = {
        ...restValues,
        sourceId: values.sourceId || "",
        language: restValues.lang || restValues.language || "",
        id: id ? Number(id) : 0,
        dbId: Number(restValues.dbId) || 0,
        hits: Number(restValues.hits) || 0,
        isCustomPicture: isCustom,
        customPicture: customPic,
        picture: isCustom ? (sourcePic || customPic) : "",
        customPictureSlide: customSlide,
      };
      if (!id) {
        payload.playLink = (playLink || "").trim();
        payload.playForm = playForm || "";
      }

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
        form.setFieldValue("picture", resp.data);
        onSuccess?.(resp.data);
        checkDirty();
      } else {
        message.error(resp.msg);
        onError?.(new Error(resp.msg));
      }
    } catch (err: any) {
      message.error(err.message || "上传图片失败");
      onError?.(err);
    }
  };

  if (fetching) {
    return (
      <div className={styles.loadingContainer}>
        <Spin size="large" />
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
            sourceId: urlSourceId || undefined,
            dbId: 0,
            hits: 0,
            followPosterSource: Boolean(id),
          }}
          requiredMark="optional"
        >
          <Space direction="vertical" size={16} className={styles.formSections}>
            <FilmBasicCard
              id={id}
              readOnly={readOnly}
              sources={sources}
              categoryTreeData={categoryTreeData}
              categoryLoading={categoryLoading}
              watchedFollowPosterSource={Boolean(watchedFollowPosterSource)}
              watchedPicture={String(watchedPicture || "")}
              loadedDetail={loadedDetail}
              canWrite={canWrite}
              tmdbEnabled={tmdbEnabled}
              tmdbSnapshot={tmdbSnapshot}
              onUndoTmdb={handleUndoTmdb}
              onOpenTmdb={() => setTmdbModalOpen(true)}
              onSourceChange={handleSourceChange}
              onClassChange={handleClassChange}
              onUpload={customUpload}
              onOpenPicker={() => setPickerOpen(true)}
              styles={styles}
            />

            <FilmMetaCards isNew={!id} styles={styles} />
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
