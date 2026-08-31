"use client";

import React, { useState, useEffect, useCallback } from "react";
import {
  Upload,
  Pagination,
  Image,
  Typography,
  Space,
  Empty,
  Popconfirm,
  Spin,
  Tooltip,
  Button,
  Input,
  Modal,
  Alert,
  DatePicker,
} from "antd";
import {
  PlusOutlined,
  DeleteOutlined,
  EyeOutlined,
  CloudUploadOutlined,
  CopyOutlined,
  FormOutlined,
  SearchOutlined,
  ReloadOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import {
  IMAGE_UPLOAD_ACCEPT,
  isAllowedImageFile,
} from "@/lib/imageUpload";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import ManagePageHeader from "@/app/manage/components/page-header";
import dayjs from "dayjs";
import styles from "./index.module.less";

const { Text } = Typography;
const { RangePicker } = DatePicker;

type DateRangeValue = [dayjs.Dayjs, dayjs.Dayjs] | null;

const RANGE_TIME_DEFAULT: [dayjs.Dayjs, dayjs.Dayjs] = [
  dayjs("00:00:00", "HH:mm:ss"),
  dayjs("23:59:59", "HH:mm:ss"),
];

function formatDateRange(range: DateRangeValue) {
  if (!range?.[0] || !range?.[1]) {
    return { beginTime: "", endTime: "" };
  }
  return {
    beginTime: range[0].format("YYYY-MM-DD HH:mm:ss"),
    endTime: range[1].format("YYYY-MM-DD HH:mm:ss"),
  };
}

interface PhotoItem {
  ID: number;
  link: string;
  name: string;
  fid: string;
  CreatedAt?: string;
}

interface StorageStatus {
  volumeMounted: boolean;
  missingCount: number;
}

interface StorageAlert {
  key: string;
  title: string;
  detail: string;
  steps: string[];
}

function buildStorageAlerts(storage?: StorageStatus | null): StorageAlert[] {
  if (!storage) {
    return [];
  }
  const alerts: StorageAlert[] = [];
  if (storage.volumeMounted === false) {
    alerts.push({
      key: "volume",
      title: "素材目录未挂载发布卷",
      detail:
        "当前写入容器可写层，升级重建会丢掉已上传素材。发布卷应为 /app/static/upload。",
      steps: [
        "发布版 compose 为 ecohub 增加卷：./data/uploads:/app/static/upload",
        "源码版 compose 为 server 增加卷：eco-server-uploads:/app/static/upload",
        "改完后执行 docker compose up -d 重启对应容器",
      ],
    });
  }
  if (storage.missingCount > 0) {
    alerts.push({
      key: "missing",
      title: `有 ${storage.missingCount} 条素材文件缺失`,
      detail: "在服务器 compose 目录执行（1Panel 若挂的是 ./uploads，把 data/uploads 改成 uploads）：",
      steps: [
        "mkdir -p ./data/uploads/gallery && docker cp Eco-hub:/app/server/static/upload/gallery/. ./data/uploads/gallery/",
        "拷不出来就重新上传这些图",
      ],
    });
  }
  return alerts;
}

export default function FileUploadPageView() {
  const [list, setList] = useState<PhotoItem[]>([]);
  const [storage, setStorage] = useState<StorageStatus | null>(null);
  const [loading, setLoading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [page, setPage] = useState({ current: 1, pageSize: 36, total: 0 });
  const [keyword, setKeyword] = useState("");
  const [inputValue, setInputValue] = useState("");
  const [dateRange, setDateRange] = useState<DateRangeValue>(null);
  const [appliedRange, setAppliedRange] = useState({
    beginTime: "",
    endTime: "",
  });
  const [renameTarget, setRenameTarget] = useState<PhotoItem | null>(null);
  const [renameValue, setRenameValue] = useState("");
  const [renaming, setRenaming] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [previewIndex, setPreviewIndex] = useState(0);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();
  const alerts = buildStorageAlerts(storage);

  const getPhotoList = useCallback(
    async (
      current = 1,
      name = keyword,
      range = appliedRange,
    ) => {
      setLoading(true);
      try {
        const resp = await ApiGet("/manage/file/list", {
          current,
          pageSize: 36,
          name,
          beginTime: range.beginTime,
          endTime: range.endTime,
        });
        if (resp.code === 0) {
          setList(resp.data.list || []);
          setStorage(resp.data.storage ?? null);
          if (resp.data.page) {
            setPage({
              current: resp.data.page.current,
              pageSize: resp.data.page.pageSize || 36,
              total: resp.data.page.total || 0,
            });
          }
        }
      } finally {
        setLoading(false);
      }
    },
    [keyword, appliedRange],
  );

  useEffect(() => {
    getPhotoList();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleSearch = () => {
    const kw = inputValue.trim();
    const range = formatDateRange(dateRange);
    setKeyword(kw);
    setAppliedRange(range);
    void getPhotoList(1, kw, range);
  };

  const handleReset = () => {
    const emptyRange = { beginTime: "", endTime: "" };
    setInputValue("");
    setKeyword("");
    setDateRange(null);
    setAppliedRange(emptyRange);
    void getPhotoList(1, "", emptyRange);
  };

  const copyLink = async (link: string) => {
    const fullLink =
      typeof window !== "undefined" && link.startsWith("/")
        ? `${window.location.origin}${link}`
        : link;
    const fallbackCopy = () => {
      const ta = document.createElement("textarea");
      ta.value = fullLink;
      ta.style.position = "fixed";
      ta.style.opacity = "0";
      document.body.appendChild(ta);
      ta.select();
      const ok = document.execCommand("copy");
      document.body.removeChild(ta);
      return ok;
    };
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(fullLink);
      } else if (!fallbackCopy()) {
        throw new Error("copy failed");
      }
      message.success("图片链接已复制");
    } catch {
      message.error("复制失败，请手动复制链接");
    }
  };

  const openRename = (item: PhotoItem) => {
    if (!canWrite) {
      message.warning("访客仅可查看，无法重命名素材");
      return;
    }
    setRenameTarget(item);
    setRenameValue(item.name || item.fid || "");
  };

  const submitRename = async () => {
    const name = renameValue.trim();
    if (!renameTarget) {
      return;
    }
    if (!name) {
      message.warning("素材名称不能为空");
      return;
    }
    setRenaming(true);
    try {
      const resp = await ApiPost("/manage/file/rename", {
        id: String(renameTarget.ID),
        name,
      });
      if (resp.code === 0) {
        message.success(resp.msg);
        setRenameTarget(null);
        getPhotoList(page.current);
      } else {
        message.error(resp.msg);
      }
    } finally {
      setRenaming(false);
    }
  };

  const handleDragEnter = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragging(true);
  };

  const handleDragLeave = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    const rect = e.currentTarget.getBoundingClientRect();
    if (
      e.clientX < rect.left ||
      e.clientX > rect.right ||
      e.clientY < rect.top ||
      e.clientY > rect.bottom
    ) {
      setDragging(false);
    }
  };

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault();
    e.stopPropagation();
    setDragging(false);
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
        onSuccess(resp.data);
        getPhotoList(1);
      } else {
        message.error(resp.msg);
        onError(resp.msg);
      }
    } catch (err: any) {
      // 拦截器已统一提示，避免重复弹窗
      onError(err);
    }
  };

  const delImage = async (item: PhotoItem) => {
    const resp = await ApiPost("/manage/file/del", { id: String(item.ID) });
    if (resp.code === 0) {
      message.success(resp.msg);
      getPhotoList(page.current);
    } else {
      message.error(resp.msg);
    }
  };

  return (
    <div className={styles.galleryPanel}>
      <ManagePageHeader
        title="素材中心"
        description="管理用户上传的海报与封面素材，可在影片/轮播编辑中选用覆盖封面。"
      />
      <div className={styles.toolbar}>
        <Space size={[8, 8]} wrap className={styles.filterBar}>
          <Input
            placeholder="搜索素材名称"
            allowClear
            value={inputValue}
            onChange={(e) => setInputValue(e.target.value)}
            onPressEnter={handleSearch}
            className={styles.searchInput}
          />
          <RangePicker
            showTime={{ defaultValue: RANGE_TIME_DEFAULT }}
            value={dateRange}
            onChange={(dates) => {
              setDateRange(
                dates && dates[0] && dates[1]
                  ? [dates[0], dates[1]]
                  : null,
              );
            }}
            className={styles.dateRange}
            placeholder={["开始时间", "结束时间"]}
          />
          <Button
            type="primary"
            icon={<SearchOutlined />}
            onClick={handleSearch}
          >
            搜索
          </Button>
          <Button icon={<ReloadOutlined />} onClick={handleReset}>
            重置
          </Button>
        </Space>
        <div className={styles.toolbarRight}>
          <Upload
            customRequest={customUpload}
            multiple
            showUploadList={false}
            accept={IMAGE_UPLOAD_ACCEPT}
            disabled={!canWrite}
          >
            <Button
              type="primary"
              icon={<PlusOutlined />}
              disabled={!canWrite}
            >
              上传图片
            </Button>
          </Upload>
        </div>
      </div>
      {alerts.map((item) => (
        <Alert
          key={item.key}
          type="error"
          showIcon
          title={item.title}
          description={
            <div className={styles.alertBody}>
              <div>{item.detail}</div>
              {item.steps?.length ? (
                <ol className={styles.alertSteps}>
                  {item.steps.map((step) => (
                    <li key={step}>{step}</li>
                  ))}
                </ol>
              ) : null}
            </div>
          }
        />
      ))}

      <div
        className={styles.container}
        onDragEnter={handleDragEnter}
        onDragOver={(e) => {
          e.preventDefault();
          e.stopPropagation();
          setDragging(true);
        }}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        <div
          className={`${styles.dropOverlay} ${dragging ? styles.dropOverlayActive : ""}`}
        >
          <Upload
            customRequest={customUpload}
            multiple
            showUploadList={false}
            accept={IMAGE_UPLOAD_ACCEPT}
            style={{ width: "100%", height: "100%" }}
            disabled={!canWrite}
          >
            <div style={{ pointerEvents: "none" }}>
              <CloudUploadOutlined className={styles.draggerIcon} />
              <div className={styles.dropText}>
                {canWrite ? "松开以开始上传" : "访客仅可查看"}
              </div>
              <Text style={{ color: "var(--ant-color-primary)", opacity: 0.8 }}>
                {canWrite
                  ? "支持批量上传 JPG/PNG/WebP/ICO"
                  : "当前账号为访客，不支持上传或修改素材"}
              </Text>
            </div>
          </Upload>
        </div>

        <div className={styles.gallerySection}>
          <Spin spinning={loading}>
            {list.length > 0 ? (
              <div className={styles.imageGrid}>
                <Image.PreviewGroup
                  items={list.map((item) => item.link)}
                  preview={{
                    open: previewOpen,
                    current: previewIndex,
                    onOpenChange: (open) => setPreviewOpen(open),
                    onChange: (current) => setPreviewIndex(current),
                  }}
                >
                  {list.map((item, index) => (
                    <div key={item.ID} className={styles.imageCard}>
                      <div className={styles.thumbnailWrapper}>
                        {/* 缩略图使用原生 img，兼容 jpg/png/webp/ico 展示 */}
                        {/* eslint-disable-next-line @next/next/no-img-element */}
                        <img
                          src={item.link}
                          alt={item.name || item.fid || "图库缩略图"}
                          className={styles.thumbnail}
                          loading="lazy"
                          decoding="async"
                        />
                        <div className={styles.overlay}>
                          <Space size="middle">
                            <Tooltip title="查看大图">
                              <div
                                className={styles.actionBtn}
                                onClick={() => {
                                  setPreviewIndex(index);
                                  setPreviewOpen(true);
                                }}
                              >
                                <EyeOutlined />
                              </div>
                            </Tooltip>
                            <Tooltip title="复制链接">
                              <div
                                className={styles.actionBtn}
                                onClick={() => copyLink(item.link)}
                              >
                                <CopyOutlined />
                              </div>
                            </Tooltip>
                            {!canWrite ? (
                              <Tooltip title="访客仅可查看，无法删除">
                                <div
                                  className={`${styles.actionBtn} ${styles.disabledBtn}`}
                                >
                                  <DeleteOutlined />
                                </div>
                              </Tooltip>
                            ) : (
                              <Tooltip title="彻底删除">
                                <Popconfirm
                                  title="确定要从服务器删除这张图片吗？"
                                  onConfirm={() => delImage(item)}
                                  okText="确定"
                                  cancelText="取消"
                                  placement="topRight"
                                >
                                  <div
                                    className={`${styles.actionBtn} ${styles.deleteBtn}`}
                                  >
                                    <DeleteOutlined />
                                  </div>
                                </Popconfirm>
                              </Tooltip>
                            )}
                          </Space>
                        </div>
                      </div>
                      {(item.name || item.fid || item.CreatedAt) && (
                        <div className={styles.cardMeta}>
                          {(item.name || item.fid) && (
                            <div
                              className={styles.nameBar}
                              title="点击重命名"
                              onClick={() => openRename(item)}
                            >
                              <span className={styles.nameText}>
                                {item.name || item.fid}
                              </span>
                              <FormOutlined className={styles.renameIcon} />
                            </div>
                          )}
                          {item.CreatedAt ? (
                            <div className={styles.dateBar} title="上传日期">
                              {dayjs(item.CreatedAt).format("YYYY-MM-DD HH:mm")}
                            </div>
                          ) : null}
                        </div>
                      )}
                    </div>
                  ))}
                </Image.PreviewGroup>
              </div>
            ) : (
              !loading && (
                <div className={styles.emptyState}>
                  <Empty
                    image={Empty.PRESENTED_IMAGE_SIMPLE}
                    description={
                      <Text type="secondary">
                        {keyword || appliedRange.beginTime || appliedRange.endTime
                          ? "没有符合条件的素材，可调整筛选或点击重置"
                          : "暂无上传素材，拖拽到内容区或点击工具栏上传"}
                      </Text>
                    }
                  />
                </div>
              )
            )}
          </Spin>

          <div className={styles.pagination}>
            <Pagination
              current={page.current}
              pageSize={page.pageSize}
              total={page.total}
              onChange={(p) => getPhotoList(p)}
              showSizeChanger={false}
              showTotal={(total) => `共 ${total} 条`}
            />
          </div>
        </div>
      </div>

      <Modal
        title="重命名素材"
        open={!!renameTarget}
        onCancel={() => setRenameTarget(null)}
        onOk={submitRename}
        okText="保存"
        cancelText="取消"
        confirmLoading={renaming}
        destroyOnHidden
        width={400}
      >
        <Input
          value={renameValue}
          onChange={(e) => setRenameValue(e.target.value)}
          placeholder="输入素材名称"
          maxLength={50}
          autoFocus
          onPressEnter={submitRename}
        />
      </Modal>
    </div>
  );
}
