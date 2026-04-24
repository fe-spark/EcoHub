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
  Tag,
  FloatButton,
} from "antd";
import {
  PlusOutlined,
  DeleteOutlined,
  EyeOutlined,
  CloudUploadOutlined,
  FileImageOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import ManagePageShell from "../../components/page-shell";
import styles from "./index.module.less";

const { Text } = Typography;

interface PhotoItem {
  ID: number;
  link: string;
}

export default function FileUploadPageView() {
  const [list, setList] = useState<PhotoItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [page, setPage] = useState({ current: 1, pageSize: 36, total: 0 });
  const { message } = useAppMessage();

  const getPhotoList = useCallback(async (current = 1) => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/file/list", { current, pageSize: 36 });
      if (resp.code === 0) {
        setList(resp.data.list || []);
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
  }, []);

  useEffect(() => {
    getPhotoList();
  }, [getPhotoList]);

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
      message.error("上传失败");
      onError(err);
    }
  };

  const delImage = async (id: number) => {
    const resp = await ApiPost("/manage/file/del", { id: String(id) });
    if (resp.code === 0) {
      message.success(resp.msg);
      getPhotoList(page.current);
    } else {
      message.error(resp.msg);
    }
  };

  return (
    <ManagePageShell
      eyebrow="图片素材"
      title="图片素材"
      description="集中管理全站影视海报、封面素材与图库资源，支持拖拽到页面区域直接上传。"
      extra={
        <div className={styles.headerSection}>
          <div className={styles.titleArea}>
            <Text type="secondary">
              <FileImageOutlined style={{ marginRight: 6 }} />
              管理全站影视海报、封面素材与图库资源 (支持拖拽至内容区上传)
            </Text>
          </div>
          <div className={styles.stats}>
            <Tag color="processing">共计 {page.total} 张图片</Tag>
          </div>
        </div>
      }
      panelClassName={styles.galleryPanel}
    >
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
      <div className={`${styles.dropOverlay} ${dragging ? styles.dropOverlayActive : ""}`}>
        <Upload customRequest={customUpload} multiple showUploadList={false} style={{ width: "100%", height: "100%" }}>
          <div style={{ pointerEvents: "none" }}>
            <CloudUploadOutlined className={styles.draggerIcon} />
            <div className={styles.dropText}>松开以开始上传</div>
            <Text style={{ color: "var(--ant-color-primary)", opacity: 0.8 }}>
              支持批量上传多张海报图片到当前内容库
            </Text>
          </div>
        </Upload>
      </div>

      <Upload customRequest={customUpload} multiple showUploadList={false}>
        <Tooltip title="添加图片资源" placement="left">
          <FloatButton icon={<PlusOutlined />} type="primary" style={{ right: 40, bottom: 40 }} />
        </Tooltip>
      </Upload>

      <div className={styles.gallerySection}>
        <Spin spinning={loading}>
          {list.length > 0 ? (
            <div className={styles.imageGrid}>
              <Image.PreviewGroup>
                {list.map((item) => (
                  <div key={item.ID} className={styles.imageCard}>
                    <div className={styles.thumbnailWrapper}>
                      {/* 管理端图库使用 AntD PreviewGroup，缩略图维持原生 img 更直接 */}
                      {/* eslint-disable-next-line @next/next/no-img-element */}
                      <img src={item.link} alt="图库缩略图" className={styles.thumbnail} />
                    </div>
                    <div className={styles.overlay}>
                      <Space size="large">
                        <Tooltip title="查看大图">
                          <div className={styles.actionBtn}>
                            <EyeOutlined />
                          </div>
                        </Tooltip>
                        <Tooltip title="彻底删除">
                          <Popconfirm
                            title="确定要从服务器删除这张图片吗？"
                            onConfirm={() => delImage(item.ID)}
                            okText="确定"
                            cancelText="取消"
                            placement="topRight"
                          >
                            <div className={`${styles.actionBtn} ${styles.deleteBtn}`}>
                              <DeleteOutlined />
                            </div>
                          </Popconfirm>
                        </Tooltip>
                      </Space>
                    </div>
                    <div style={{ display: "none" }}>
                      <Image src={item.link} alt="图库预览图" />
                    </div>
                  </div>
                ))}
              </Image.PreviewGroup>
            </div>
          ) : (
            !loading && (
              <div className={styles.emptyState}>
                <Empty
                  image={Empty.PRESENTED_IMAGE_SIMPLE}
                  description={<Text type="secondary">内容库暂无图片，直接拖拽或点击右下角按钮上传</Text>}
                />
              </div>
            )
          )}
        </Spin>

        {page.total > page.pageSize && (
          <div className={styles.pagination}>
            <Pagination
              current={page.current}
              pageSize={page.pageSize}
              total={page.total}
              onChange={(p) => getPhotoList(p)}
              showSizeChanger={false}
              hideOnSinglePage
            />
          </div>
        )}
      </div>
      </div>
    </ManagePageShell>
  );
}
