import React from "react";
import { Button, Card, Popconfirm, Space } from "antd";
import styles from "./index.module.less";

export interface CollectToolbarProps {
  totalCount: number;
  selectedCount: number;
  canWrite: boolean;
  batchStateUpdating: boolean;
  batchDeleting: boolean;
  deleteConfirmTitle: string;
  deleteConfirmDescription: string;
  onSelectAll: () => void;
  onInvertSelection: () => void;
  onClearSelection: () => void;
  onOpenBatchCollect: () => void;
  onBatchEnable: () => void;
  onBatchDisable: () => void;
  onBatchDelete: () => void;
}

export default function CollectToolbar(props: CollectToolbarProps) {
  const {
    totalCount,
    selectedCount,
    canWrite,
    batchStateUpdating,
    batchDeleting,
    deleteConfirmTitle,
    deleteConfirmDescription,
    onSelectAll,
    onInvertSelection,
    onClearSelection,
    onOpenBatchCollect,
    onBatchEnable,
    onBatchDisable,
    onBatchDelete,
  } = props;

  return (
    <Card size="small" className={styles.toolbarCard} styles={{ body: { padding: 12 } }}>
      <div className={styles.toolbar}>
        <Space size={[8, 8]} wrap>
          <span className={styles.toolbarHint}>共 {totalCount} 个</span>
          <Button size="small" data-tour="collect-select-all" onClick={onSelectAll}>
            全选
          </Button>
          <Button size="small" onClick={onInvertSelection}>
            反选
          </Button>
          <Button size="small" disabled={selectedCount === 0} onClick={onClearSelection}>
            清空
          </Button>
        </Space>
        <Space size={8} wrap className={styles.toolbarActions}>
          <Button
            type="primary"
            disabled={!canWrite || selectedCount === 0}
            data-tour="collect-batch"
            onClick={onOpenBatchCollect}
          >
            批量采集{selectedCount > 0 ? ` (${selectedCount})` : ""}
          </Button>
          <Button
            loading={batchStateUpdating}
            disabled={!canWrite || selectedCount === 0}
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
            disabled={selectedCount === 0}
            onConfirm={onBatchDisable}
          >
            <Button
              danger
              loading={batchStateUpdating}
              disabled={!canWrite || selectedCount === 0}
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
            disabled={!canWrite || selectedCount === 0 || batchDeleting}
            onConfirm={onBatchDelete}
          >
            <Button
              danger
              loading={batchDeleting}
              disabled={!canWrite || selectedCount === 0}
            >
              批量删除{selectedCount > 0 ? ` (${selectedCount})` : ""}
            </Button>
          </Popconfirm>
        </Space>
      </div>
    </Card>
  );
}
