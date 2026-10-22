"use client";

import React from "react";
import {
  Tag,
  Button,
  Tooltip,
  Popconfirm,
  Typography,
  Space,
} from "antd";
import {
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

const { Paragraph } = Typography;

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

  const metaParts = [
    record.cName,
    record.year ? String(record.year) : null,
    record.area,
    record.remark,
  ].filter(Boolean);

  return (
    <div className={styles.bannerCard}>
      {/* 卡片正文：左侧海报 + 右侧详情 */}
      <div className={styles.cardBody} onClick={() => onEdit(record)}>
        <div className={styles.posterBox}>
          <img
            src={posterUrl}
            alt={record.name}
            className={styles.posterImg}
            onError={(e) => {
              (e.currentTarget as HTMLImageElement).src = FALLBACK_IMG;
            }}
          />
          {record.score && record.score > 0 ? (
            <span className={styles.scoreBadge}>
              <StarFilled style={{ color: "#faad14" }} />
              {record.score.toFixed(1)}
            </span>
          ) : null}
        </div>

        <div className={styles.cardInfo}>
          <div>
            <div className={styles.cardHeadRow}>
              <span className={styles.orderNum}>
                {String(index + 1).padStart(2, "0")}
              </span>
              <Paragraph className={styles.cardTitle} ellipsis={{ rows: 1, tooltip: record.name }}>
                {record.name}
              </Paragraph>
              {record.isPinned && (
                <Tag color="magenta" style={{ margin: 0, borderRadius: 4, fontSize: 11 }}>
                  置顶
                </Tag>
              )}
            </div>

            <div className={styles.metaLine}>
              {metaParts.length > 0 ? metaParts.join(" · ") : "暂无分类信息"}
            </div>

            <div className={styles.cardBlurb} title={record.blurb}>
              {record.blurb || "暂无简介"}
            </div>
          </div>
        </div>
      </div>

      {/* 底部操作工具栏 */}
      <div className={styles.cardFooter}>
        <div className={styles.dragHandle} {...headerDragProps}>
          <HolderOutlined />
          <span>排序</span>
        </div>

        <Space size={6} onClick={(e) => e.stopPropagation()}>
          <Button
            size="small"
            type="text"
            disabled={!canWrite}
            onClick={() => onEdit(record)}
          >
            编辑
          </Button>

          <Tooltip title="上移">
            <Button
              size="small"
              type="text"
              icon={<ArrowUpOutlined style={{ fontSize: 11 }} />}
              disabled={!canWrite || isFirst}
              onClick={() => onMoveOrder(index, "up")}
            />
          </Tooltip>

          <Tooltip title="下移">
            <Button
              size="small"
              type="text"
              icon={<ArrowDownOutlined style={{ fontSize: 11 }} />}
              disabled={!canWrite || isLast}
              onClick={() => onMoveOrder(index, "down")}
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
                icon={<DeleteOutlined style={{ fontSize: 12 }} />}
                disabled={!canWrite}
              />
            </Tooltip>
          </Popconfirm>
        </Space>
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
