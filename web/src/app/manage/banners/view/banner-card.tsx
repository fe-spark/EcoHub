"use client";

import React from "react";
import {
  Tag,
  Button,
  Tooltip,
  Popconfirm,
  Image as AntImage,
  Typography,
  Space,
} from "antd";
import {
  EditOutlined,
  DeleteOutlined,
  ArrowUpOutlined,
  ArrowDownOutlined,
  HolderOutlined,
  StarFilled,
} from "@ant-design/icons";
import { useSortable } from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { FALLBACK_IMG } from "@/lib/fallbackImg";
import { BannerRecord } from "./types";
import styles from "./index.module.less";

const { Text, Paragraph } = Typography;

export interface BannerCardProps {
  record: BannerRecord;
  index: number;
  totalCount: number;
  canWrite: boolean;
  headerDragProps?: React.HTMLAttributes<HTMLDivElement>;
  onEdit: (record: BannerRecord) => void;
  onDelete: (id: string) => void;
  onMoveOrder: (index: number, direction: "up" | "down") => void;
}

export function BannerCard({
  record,
  index,
  totalCount,
  canWrite,
  headerDragProps,
  onEdit,
  onDelete,
  onMoveOrder,
}: BannerCardProps) {
  const isFirst = index === 0;
  const isLast = index === totalCount - 1;

  const posterUrl =
    record.picture || record.poster || record.customPicture || FALLBACK_IMG;

  return (
    <div className={styles.bannerCard}>
      {/* 头部：拖拽手柄、序号及操作区 */}
      <div className={styles.cardHead} {...headerDragProps}>
        <div className={styles.cardHeadLeft}>
          <HolderOutlined className={styles.dragHandleIcon} />
          <Tag color="purple" className={styles.orderTag}>
            #{String(index + 1).padStart(2, "0")}
          </Tag>
        </div>

        <div
          className={styles.cardHeadActions}
          onPointerDown={(e) => e.stopPropagation()}
        >
          <Space.Compact size="small">
            <Button
              size="small"
              type="text"
              icon={<ArrowUpOutlined style={{ fontSize: 11 }} />}
              disabled={!canWrite || isFirst}
              onClick={() => onMoveOrder(index, "up")}
              title="上移"
            />
            <Button
              size="small"
              type="text"
              icon={<ArrowDownOutlined style={{ fontSize: 11 }} />}
              disabled={!canWrite || isLast}
              onClick={() => onMoveOrder(index, "down")}
              title="下移"
            />
          </Space.Compact>

          <Tooltip title="编辑">
            <Button
              size="small"
              type="text"
              icon={<EditOutlined style={{ fontSize: 13 }} />}
              disabled={!canWrite}
              onClick={() => onEdit(record)}
            />
          </Tooltip>

          <Popconfirm
            title="确认移除？"
            onConfirm={() => onDelete(record.id)}
            okText="确定"
            cancelText="取消"
            okButtonProps={{ danger: true }}
          >
            <Tooltip title="移除">
              <Button
                size="small"
                type="text"
                danger
                icon={<DeleteOutlined style={{ fontSize: 13 }} />}
                disabled={!canWrite}
              />
            </Tooltip>
          </Popconfirm>
        </div>
      </div>

      {/* 卡片正文：左侧大封面海报，右侧详细信息，高度约 220px 黄金比例 */}
      <div className={styles.cardBody} onClick={() => onEdit(record)}>
        <div
          className={styles.posterContainer}
          onClick={(e) => e.stopPropagation()}
        >
          <AntImage
            src={posterUrl}
            alt={record.name}
            fallback={FALLBACK_IMG}
            preview={{ mask: "预览" }}
            className={styles.posterImage}
            style={{
              width: "100%",
              height: "100%",
              objectFit: "cover",
              display: "block",
            }}
            styles={{
              root: {
                width: "100%",
                height: "100%",
                display: "block",
              },
            }}
          />
        </div>

        <div className={styles.cardMainInfo}>
          {/* 影视标题 */}
          <div className={styles.cardTitleRow}>
            <Paragraph
              className={styles.cardFilmTitle}
              ellipsis={{ rows: 2, tooltip: record.name }}
            >
              {record.name}
            </Paragraph>
          </div>

          {/* 标签栏：分类、年份、海报来源等 */}
          <div className={styles.cardTagsRow}>
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
            {record.isCustomPic && (
              <Tag color="cyan" style={{ borderRadius: 4, margin: 0 }}>
                自定义封面
              </Tag>
            )}
          </div>

          {/* 评分与剧集状态 */}
          <div className={styles.cardScoreRow}>
            {record.score && record.score > 0 ? (
              <Tag
                color="gold"
                icon={<StarFilled />}
                style={{ margin: 0, borderRadius: 4 }}
              >
                {record.score.toFixed(1)} 分
              </Tag>
            ) : (
              <Tag style={{ margin: 0, borderRadius: 4 }}>暂无评分</Tag>
            )}
            {record.remark && (
              <Text type="secondary" className={styles.remarkText} ellipsis={{ tooltip: record.remark }}>
                {record.remark}
              </Text>
            )}
          </div>

          {/* 简介剧情 */}
          {record.blurb ? (
            <Paragraph
              ellipsis={{ rows: 2, tooltip: record.blurb }}
              className={styles.blurbText}
            >
              {record.blurb}
            </Paragraph>
          ) : (
            <div className={styles.blurbTextEmpty}>
              暂无简介
            </div>
          )}

          {/* 底部元数据 */}
          <div className={styles.cardFooterRow}>
            <span className={styles.filmMetaMid}>MID: {record.mid || record.id}</span>
            {record.hits != null && record.hits > 0 ? (
              <span className={styles.filmMetaHits}>热度: {record.hits}</span>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}

export function SortableBannerCard(props: BannerCardProps) {
  const {
    attributes,
    listeners,
    setNodeRef,
    transform,
    transition,
    isDragging,
  } = useSortable({ id: props.record.id });

  const style: React.CSSProperties = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 10 : undefined,
  };

  return (
    <div
      ref={setNodeRef}
      style={style}
      className={isDragging ? styles.dragPlaceholder : undefined}
    >
      <BannerCard
        {...props}
        headerDragProps={{ ...attributes, ...listeners }}
      />
    </div>
  );
}
