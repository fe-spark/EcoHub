import React from "react";
import { Space, Button, Popconfirm, Divider } from "antd";
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
  onAdd: () => void;
}

export function FilmBatchBar({
  selectedCount,
  canWrite,
  updating,
  deleting,
  onBatchUpdate,
  onBatchDelete,
  onAdd,
}: FilmBatchBarProps) {
  const hasSelected = selectedCount > 0;
  const isActionDisabled = !canWrite || !hasSelected;

  return (
    <Space size={[8, 8]} wrap align="center">
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
  );
}
