"use client";

import React from "react";
import { Space, Button, Popconfirm, Tag, Divider } from "antd";
import {
  PlusOutlined,
  ReloadOutlined,
  DeleteOutlined,
} from "@ant-design/icons";

interface FilmBatchBarProps {
  selectedCount: number;
  canWrite: boolean;
  updating: boolean;
  deleting: boolean;
  onBatchUpdate: () => void;
  onBatchDelete: () => void;
  onClearSelection: () => void;
  onAdd: () => void;
  styles: any;
}

export function FilmBatchBar({
  selectedCount,
  canWrite,
  updating,
  deleting,
  onBatchUpdate,
  onBatchDelete,
  onClearSelection,
  onAdd,
  styles,
}: FilmBatchBarProps) {
  const hasSelected = selectedCount > 0;
  const isActionDisabled = !canWrite || !hasSelected;

  return (
    <div className={styles.tableHeader}>
      <div className={styles.tableTitleGroup}>
        <span className={styles.tableTitle}>影片列表</span>
        {hasSelected && (
          <Tag
            color="processing"
            closable
            onClose={(e) => {
              e.preventDefault();
              onClearSelection();
            }}
            style={{ borderRadius: 4, margin: 0 }}
          >
            已选择 {selectedCount} 项
          </Tag>
        )}
      </div>

      <Space size={[8, 8]} wrap align="center" className={styles.tableActions}>
        <Button
          type="primary"
          style={!isActionDisabled ? { background: "#52c41a", borderColor: "#52c41a" } : undefined}
          icon={<ReloadOutlined />}
          disabled={isActionDisabled}
          loading={updating}
          onClick={onBatchUpdate}
        >
          批量更新
        </Button>
        {!isActionDisabled ? (
          <Popconfirm
            title="确定删除？"
            okText="确定"
            cancelText="取消"
            onConfirm={onBatchDelete}
          >
            <Button
              danger
              icon={<DeleteOutlined />}
              loading={deleting}
            >
              批量删除
            </Button>
          </Popconfirm>
        ) : (
          <Button
            danger
            icon={<DeleteOutlined />}
            disabled
          >
            批量删除
          </Button>
        )}
        <Divider orientation="vertical" style={{ height: 20, margin: "0 4px" }} />
        <Button
          type="primary"
          icon={<PlusOutlined />}
          disabled={!canWrite}
          onClick={onAdd}
        >
          新增
        </Button>
      </Space>
    </div>
  );
}
