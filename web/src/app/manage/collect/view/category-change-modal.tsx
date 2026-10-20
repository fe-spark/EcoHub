import React from "react";
import { Button, Modal, Space, Typography } from "antd";
import type { CategoryTreeChange } from "./use-collect-stream";

const { Text } = Typography;

export interface CategoryChangeModalProps {
  open: boolean;
  sourceName?: string;
  changes: CategoryTreeChange[];
  loading: boolean;
  onCancel: () => void;
  onKeep: () => void;
  onClear: () => void;
}

export function formatCategoryChangeText(change: CategoryTreeChange): string {
  if (change.kind === "renamed") {
    return `${change.from || ""} → ${change.to || ""}`;
  }
  if (change.kind === "moved") {
    return `${change.name || ""}：${change.from || "顶级"} → ${change.to || "顶级"}`;
  }
  if (change.kind === "removed") {
    return `删除 ${change.name || ""}`;
  }
  return change.name || "";
}

export default function CategoryChangeModal(props: CategoryChangeModalProps) {
  const { open, sourceName, changes, loading, onCancel, onKeep, onClear } = props;

  const displayChanges = changes.slice(0, 8);
  const remainingCount = changes.length - 8;

  return (
    <Modal
      open={open}
      title={sourceName ? `${sourceName}：分类树已变化` : "分类树已变化"}
      onCancel={loading ? undefined : onCancel}
      footer={[
        <Button key="cancel" disabled={loading} onClick={onCancel}>
          取消
        </Button>,
        <Button key="keep" type="primary" loading={loading} onClick={onKeep}>
          保留数据
        </Button>,
        <Button key="clear" danger type="primary" loading={loading} onClick={onClear}>
          清空该站数据
        </Button>,
      ]}
      destroyOnHidden
    >
      <div style={{ display: "flex", flexDirection: "column", gap: 12, padding: "12px 0" }}>
        <Text type="secondary">选择处理方式</Text>
        <div
          style={{
            maxHeight: 280,
            overflowY: "auto",
            background: "rgba(0, 0, 0, 0.02)",
            padding: 12,
            borderRadius: 6,
            border: "1px solid rgba(0, 0, 0, 0.06)",
          }}
        >
          <Space orientation="vertical" size={6} style={{ width: "100%" }}>
            {displayChanges.map((change, idx) => (
              <div key={`${change.typeId}-${change.kind}-${idx}`}>
                <Text>{formatCategoryChangeText(change)}</Text>
              </div>
            ))}
            {remainingCount > 0 && (
              <Text type="secondary">还有 {remainingCount} 处</Text>
            )}
          </Space>
        </div>
      </div>
    </Modal>
  );
}
