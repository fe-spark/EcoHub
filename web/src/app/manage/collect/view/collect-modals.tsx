"use client";

import React from "react";
import BatchCollectDrawer, { type BatchCollectRow } from "./batch-collect-drawer";
import CategoryChangeModal from "./category-change-modal";
import CleanupInvalidModal from "./cleanup-invalid-modal";
import type { CategoryTreeChange } from "./use-collect-stream";
import type { CleanupSkippedItem, InvalidSourceItem } from "./types";

interface CollectModalsProps {
  batchOpen: boolean;
  batchRows: BatchCollectRow[];
  batchTime: number;
  isIndexing: boolean;
  onCancelBatch: () => void;
  onSubmitBatch: () => void;
  onBatchTimeChange: (time: number) => void;

  cleanupOpen: boolean;
  cleanupScanning: boolean;
  cleanupDeleting: boolean;
  invalidSources: InvalidSourceItem[];
  cleanupSkipped: CleanupSkippedItem[];
  onCancelCleanup: () => void;
  onConfirmCleanup: () => void;

  categoryConfirm: {
    open: boolean;
    sourceName: string;
    changes: CategoryTreeChange[];
    loading: boolean;
    closeConfirm: () => void;
    handleKeep: () => Promise<void>;
    handleClear: () => Promise<void>;
  };

  primaryHandoverNode: React.ReactNode;
}

export default function CollectModals({
  batchOpen,
  batchRows,
  batchTime,
  isIndexing,
  onCancelBatch,
  onSubmitBatch,
  onBatchTimeChange,
  cleanupOpen,
  cleanupScanning,
  cleanupDeleting,
  invalidSources,
  cleanupSkipped,
  onCancelCleanup,
  onConfirmCleanup,
  categoryConfirm,
  primaryHandoverNode,
}: CollectModalsProps) {
  return (
    <>
      <BatchCollectDrawer
        open={batchOpen}
        rows={batchRows}
        batchTime={batchTime}
        isIndexing={isIndexing}
        onCancel={onCancelBatch}
        onSubmit={onSubmitBatch}
        onBatchTimeChange={onBatchTimeChange}
      />
      <CleanupInvalidModal
        open={cleanupOpen}
        scanning={cleanupScanning}
        deleting={cleanupDeleting}
        invalidSources={invalidSources}
        skipped={cleanupSkipped}
        onCancel={onCancelCleanup}
        onConfirm={onConfirmCleanup}
      />
      <CategoryChangeModal
        open={categoryConfirm.open}
        sourceName={categoryConfirm.sourceName}
        changes={categoryConfirm.changes}
        loading={categoryConfirm.loading}
        onCancel={categoryConfirm.closeConfirm}
        onKeep={() => void categoryConfirm.handleKeep()}
        onClear={() => void categoryConfirm.handleClear()}
      />
      {primaryHandoverNode}
    </>
  );
}
