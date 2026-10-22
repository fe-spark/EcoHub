import React from "react";
import { Modal, Input } from "antd";
import type { PhotoItem } from "./types";

interface RenameModalProps {
  target: PhotoItem | null;
  value: string;
  loading: boolean;
  onChange: (value: string) => void;
  onCancel: () => void;
  onOk: () => void;
}

export default function RenameModal({
  target,
  value,
  loading,
  onChange,
  onCancel,
  onOk,
}: RenameModalProps) {
  return (
    <Modal
      title="重命名素材"
      open={!!target}
      onCancel={onCancel}
      onOk={onOk}
      okText="保存"
      cancelText="取消"
      confirmLoading={loading}
      destroyOnHidden
      width={400}
    >
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="输入素材名称"
        maxLength={50}
        autoFocus
        onPressEnter={onOk}
      />
    </Modal>
  );
}
