import React from "react";
import { Button, Popconfirm, Space, Tooltip } from "antd";
import { ThunderboltOutlined } from "@ant-design/icons";
import { ContentSectionCard } from "@/app/manage/components/content-shell";
import { collectIndexUpdatingReason } from "./collect-selection";

export interface CollectToolbarProps {
  totalCount: number;
  selectedCount: number;
  canWrite: boolean;
  batchStateUpdating: boolean;
  batchDeleting: boolean;
  hasBusySelected?: boolean;
  isIndexing?: boolean;
  deleteConfirmTitle: string;
  deleteConfirmDescription: string;
  onSelectAll: () => void;
  onInvertSelection: () => void;
  onClearSelection: () => void;
  onOpenBatchCollect: () => void;
  onBatchEnable: () => void;
  onBatchDisable: () => void;
  onBatchDelete: () => void;
  children?: React.ReactNode;
}

export default function CollectToolbar(props: CollectToolbarProps) {
  const {
    totalCount,
    selectedCount,
    canWrite,
    batchStateUpdating,
    batchDeleting,
    hasBusySelected = false,
    isIndexing = false,
    deleteConfirmTitle,
    deleteConfirmDescription,
    onSelectAll,
    onInvertSelection,
    onClearSelection,
    onOpenBatchCollect,
    onBatchEnable,
    onBatchDisable,
    onBatchDelete,
    children,
  } = props;

  const batchActionDisabled =
    !canWrite || selectedCount === 0 || batchStateUpdating || batchDeleting || hasBusySelected;

  return (
    <ContentSectionCard
      icon={<ThunderboltOutlined />}
      title="采集站列表"
      count={totalCount}
      extra={
        <Space size={6} wrap>
          <Button size="small" data-tour="collect-select-all" onClick={onSelectAll}>
            全选
          </Button>
          <Button size="small" onClick={onInvertSelection}>
            反选
          </Button>
          <Button size="small" disabled={selectedCount === 0} onClick={onClearSelection}>
            清空选择
          </Button>
        </Space>
      }
      actions={
        <Space size={8} wrap>
          <Tooltip title={isIndexing ? collectIndexUpdatingReason : undefined}>
            <span>
              <Button
                type="primary"
                disabled={batchActionDisabled || isIndexing}
                data-tour="collect-batch"
                onClick={onOpenBatchCollect}
              >
                批量采集{selectedCount > 0 ? ` (${selectedCount})` : ""}
              </Button>
            </span>
          </Tooltip>
          <Button
            loading={batchStateUpdating}
            disabled={batchActionDisabled || isIndexing}
            data-tour="collect-batch-enable"
            onClick={onBatchEnable}
          >
            批量启用{selectedCount > 0 ? ` (${selectedCount})` : ""}
          </Button>
          <Popconfirm
            title="禁用选中采集站？"
            okText="禁用"
            cancelText="取消"
            okButtonProps={{ danger: true }}
            disabled={batchActionDisabled || isIndexing}
            onConfirm={onBatchDisable}
          >
            <Button
              danger
              loading={batchStateUpdating}
              disabled={batchActionDisabled || isIndexing}
            >
              批量禁用{selectedCount > 0 ? ` (${selectedCount})` : ""}
            </Button>
          </Popconfirm>
          <Popconfirm
            title={deleteConfirmTitle}
            description={deleteConfirmDescription}
            okText="删除"
            cancelText="取消"
            okButtonProps={{ danger: true, loading: batchDeleting }}
            disabled={batchActionDisabled}
            onConfirm={onBatchDelete}
          >
            <Button
              danger
              loading={batchDeleting}
              disabled={batchActionDisabled}
            >
              批量删除{selectedCount > 0 ? ` (${selectedCount})` : ""}
            </Button>
          </Popconfirm>
        </Space>
      }
      bodyPadding
    >
      {children}
    </ContentSectionCard>
  );
}
