import React from "react";
import { Space, Tag, Button, Tooltip, Popconfirm, Image as AntImage, Typography } from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  StarFilled,
  PictureOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { BannerRecord } from "./types";
import styles from "./index.module.less";

const { Text } = Typography;

export interface BannerColumnsProps {
  banners: BannerRecord[];
  canWrite: boolean;
  handleMoveOrder: (index: number, direction: "up" | "down") => void;
  openEditEditor: (record: BannerRecord) => void;
  handleDelete: (id: string) => void;
}

export function getBannerColumns(props: BannerColumnsProps): ColumnsType<BannerRecord> {
  const { banners, canWrite, handleMoveOrder, openEditEditor, handleDelete } = props;

  return [
    {
      title: "排序",
      key: "order",
      width: 90,
      fixed: "left",
      align: "center",
      render: (_, __, index) => {
        const isFirst = index === 0;
        const isLast = index === banners.length - 1;
        return (
          <Space size={4} align="center">
            <Tag color="purple" style={{ borderRadius: 4, fontWeight: 700, margin: 0 }}>
              #{String(index + 1).padStart(2, "0")}
            </Tag>
            <Space.Compact size="small" orientation="vertical">
              <Button
                size="small"
                type="text"
                icon={<ArrowUpOutlined style={{ fontSize: 10 }} />}
                disabled={!canWrite || isFirst}
                onClick={() => handleMoveOrder(index, "up")}
                style={{ height: 18, padding: 0 }}
              />
              <Button
                size="small"
                type="text"
                icon={<ArrowDownOutlined style={{ fontSize: 10 }} />}
                disabled={!canWrite || isLast}
                onClick={() => handleMoveOrder(index, "down")}
                style={{ height: 18, padding: 0 }}
              />
            </Space.Compact>
          </Space>
        );
      },
    },
    {
      title: "影片",
      key: "filmInfo",
      width: 280,
      render: (_, record) => {
        const posterUrl = record.picture || record.poster || record.pictureSlide || FALLBACK_IMG;
        return (
          <div className={styles.filmCell}>
            <div className={styles.posterThumbWrapper}>
              <AntImage
                src={posterUrl}
                width={52}
                height={72}
                className={styles.posterThumb}
                fallback={FALLBACK_IMG}
                preview={{ mask: "预览" }}
                style={{ objectFit: "cover", width: 52, height: 72, display: "block" }}
              />
            </div>

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
                {record.isCustomPic ? (
                  <Tag color="cyan" style={{ borderRadius: 4, margin: 0 }}>
                    自定义封面
                  </Tag>
                ) : record.isScraped ? (
                  <Tag color="#722ed1" style={{ borderRadius: 4, margin: 0 }}>
                    跟随刮削
                  </Tag>
                ) : (
                  <Tag color="default" style={{ borderRadius: 4, margin: 0 }}>
                    跟随海报源
                  </Tag>
                )}
                <span className={styles.filmId}>MID: {record.mid || record.id}</span>
              </div>
            </div>
          </div>
        );
      },
    },
    {
      title: "横屏大图",
      key: "pictureSlide",
      width: 160,
      align: "center",
      render: (_, record) => {
        const hasRealSlide = Boolean(
          record.pictureSlide &&
            record.pictureSlide.trim() !== "" &&
            record.pictureSlide !== record.picture &&
            record.pictureSlide !== record.poster,
        );

        if (hasRealSlide) {
          return (
            <div className={styles.slideCell}>
              <div className={styles.slideThumbWrapper}>
                <AntImage
                  src={record.pictureSlide}
                  width={96}
                  height={54}
                  className={styles.slideThumb}
                  fallback={FALLBACK_IMG}
                  preview={{ mask: "预览" }}
                  style={{ objectFit: "cover", width: 96, height: 54, display: "block" }}
                />
              </div>
            </div>
          );
        }

        return (
          <div className={styles.slideCell}>
            <div
              className={styles.emptySlideBox}
              onClick={() => openEditEditor(record)}
              style={{ cursor: "pointer" }}
            >
              <PictureOutlined style={{ fontSize: 14 }} />
              <span>暂无横图</span>
            </div>
          </div>
        );
      },
    },
    {
      title: "简介",
      key: "meta",
      width: 220,
      render: (_, record) => (
        <Space orientation="vertical" size={2} style={{ width: "100%" }}>
          <Space size={6} align="center">
            {record.score && record.score > 0 ? (
              <Tag color="gold" icon={<StarFilled />} style={{ margin: 0, borderRadius: 4 }}>
                {record.score.toFixed(1)} 分
              </Tag>
            ) : (
              <Tag style={{ margin: 0, borderRadius: 4 }}>暂无评分</Tag>
            )}
            {record.remark && (
              <Text type="secondary" style={{ fontSize: 12 }}>
                {record.remark}
              </Text>
            )}
          </Space>
          {record.blurb ? (
            <Text
              type="secondary"
              ellipsis={{ tooltip: record.blurb }}
              style={{ fontSize: 12, display: "block" }}
            >
              {record.blurb}
            </Text>
          ) : null}
        </Space>
      ),
    },
    {
      title: "操作",
      key: "action",
      align: "center",
      fixed: "right",
      width: 100,
      render: (_, record) => (
        <Space size={6}>
          <Tooltip title="编辑">
            <Button
              size="small"
              icon={<EditOutlined />}
              disabled={!canWrite}
              onClick={() => openEditEditor(record)}
            />
          </Tooltip>

          <Popconfirm
            title="确认移除"
            onConfirm={() => handleDelete(record.id)}
            okText="确定"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Tooltip title="移除">
              <Button
                size="small"
                danger
                icon={<DeleteOutlined />}
                disabled={!canWrite}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
      ),
    },
  ];
}
