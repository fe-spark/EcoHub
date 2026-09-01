"use client";

import React, { useState, useEffect, useCallback, useMemo } from "react";
import {
  Table,
  Button,
  Tag,
  Space,
  Tooltip,
  Popconfirm,
  Image as AntImage,
  Typography,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  PlusOutlined,
  SyncOutlined,
  LockOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";

import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import ManagePageHeader from "@/app/manage/components/page-header";
import BannerModal from "./banner-modal";
import { BannerRecord, EditorMode } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;

export default function BannersPageView() {
  const [banners, setBanners] = useState<BannerRecord[]>([]);
  const [loading, setLoading] = useState(false);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const [editorVisible, setEditorVisible] = useState(false);
  const [editorMode, setEditorMode] = useState<EditorMode>("create");
  const [currentRow, setCurrentRow] = useState<BannerRecord | null>(null);

  const fetchBanners = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await ApiGet("/manage/banner/list");
      if (resp.code === 0) {
        setBanners((resp.data || []) as BannerRecord[]);
      } else {
        message.error(resp.msg);
      }
    } finally {
      setLoading(false);
    }
  }, [message]);

  useEffect(() => {
    void fetchBanners();
  }, [fetchBanners]);

  const handleDelete = useCallback(async (id: string) => {
    const resp = await ApiPost("/manage/banner/del", { id: String(id) });
    if (resp.code === 0) {
      message.success(resp.msg);
      fetchBanners();
    } else {
      message.error(resp.msg);
    }
  }, [message, fetchBanners]);

  const openCreateEditor = () => {
    setCurrentRow(null);
    setEditorMode("create");
    setEditorVisible(true);
  };

  const openEditEditor = (record: BannerRecord) => {
    setCurrentRow(record);
    setEditorMode("edit");
    setEditorVisible(true);
  };

  const columns = useMemo<ColumnsType<BannerRecord>>(
    () => [
      {
        title: "排片位次",
        key: "index",
        width: 90,
        fixed: "left",
        align: "center",
        render: (_, __, index) => (
          <Tag color="purple" style={{ borderRadius: 4, fontWeight: 600 }}>
            #{String(index + 1).padStart(2, "0")}
          </Tag>
        ),
      },
      {
        title: "影片与竖屏海报",
        key: "filmInfo",
        width: 280,
        align: "left",
        render: (_, record) => {
          const posterUrl =
            record.picture || record.poster || record.pictureSlide || FALLBACK_IMG;

          return (
            <div className={styles.filmCell}>
              <AntImage
                src={posterUrl}
                width={52}
                height={72}
                className={styles.posterThumb}
                fallback={FALLBACK_IMG}
                preview={{ mask: "预览海报" }}
              />
              <div className={styles.filmInfo}>
                <Text className={styles.filmName} ellipsis={{ tooltip: record.name }}>
                  {record.name}
                </Text>
                <div className={styles.filmMeta}>
                  {record.cName && (
                    <Tag color="orange" style={{ borderRadius: 4, margin: 0 }}>
                      {record.cName}
                    </Tag>
                  )}
                  {record.year ? (
                    <Tag color="blue" style={{ borderRadius: 4, margin: 0 }}>
                      {record.year}
                    </Tag>
                  ) : null}
                  <span className={styles.filmId}>MID: {record.mid || record.id}</span>
                </div>
              </div>
            </div>
          );
        },
      },
      {
        title: "封面保护模式",
        key: "posterMode",
        align: "center",
        width: 140,
        render: (_, record) =>
          record.isCustomPic ? (
            <Tooltip title="自定义独立封面，不受采集与海报图源覆盖">
              <Tag color="warning" icon={<LockOutlined />} style={{ borderRadius: 4 }}>
                自定义锁定
              </Tag>
            </Tooltip>
          ) : (
            <Tooltip title="海报与源站及海报图源自动联动同步">
              <Tag color="processing" icon={<SyncOutlined />} style={{ borderRadius: 4 }}>
                海报源联动
              </Tag>
            </Tooltip>
          ),
      },
      {
        title: "排序权重",
        dataIndex: "sort",
        key: "sort",
        width: 100,
        align: "center",
        render: (s: number) => (
          <Tag color={s > 0 ? "orange" : "default"} style={{ borderRadius: 4 }}>
            {s ?? 0}
          </Tag>
        ),
      },
      {
        title: "操作",
        key: "action",
        align: "center",
        fixed: "right",
        width: 120,
        render: (_, record) => (
          <Space size={8}>
            <Tooltip title="修改轮播信息">
              <Button
                type="primary"
                shape="circle"
                size="small"
                style={{ background: "#1890ff", borderColor: "#1890ff" }}
                icon={<EditOutlined />}
                disabled={!canWrite}
                onClick={() => openEditEditor(record)}
              />
            </Tooltip>
            <Popconfirm
              title="确认删除该轮播图？"
              description="删除后首页将不再轮播展示该影片。"
              onConfirm={() => handleDelete(record.id)}
              okText="确定"
              cancelText="取消"
              okButtonProps={{ danger: true }}
            >
              <Tooltip title="删除轮播">
                <Button
                  type="primary"
                  danger
                  shape="circle"
                  size="small"
                  icon={<DeleteOutlined />}
                  disabled={!canWrite}
                />
              </Tooltip>
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [canWrite, handleDelete],
  );

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="首页轮播"
        description="维护前台首页顶置大轮播排片列表，支持海报源自动联动、自定义封面锁定与排序权重。"
      />

      <Table
        bordered
        dataSource={banners}
        columns={columns}
        rowKey="id"
        loading={loading}
        size="middle"
        pagination={false}
        scroll={{ x: 730 }}
        title={() => (
          <div className={styles.tableHeader}>
            <div className={styles.tableTitle}>
              <span>轮播排片列表</span>
              <span className={styles.countBadge}>{banners.length} 项排片</span>
            </div>
            <div className={styles.tableActions}>
              <Button
                type="primary"
                icon={<PlusOutlined />}
                onClick={openCreateEditor}
                disabled={!canWrite}
              >
                添加轮播
              </Button>
            </div>
          </div>
        )}
      />

      <BannerModal
        open={editorVisible}
        mode={editorMode}
        currentRow={currentRow}
        onClose={() => setEditorVisible(false)}
        onSuccess={fetchBanners}
      />
    </div>
  );
}
