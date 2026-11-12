import React, { useState } from "react";
import { Button, Modal } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import styles from "./index.module.less";

export function ErrorDetailModal({
  open,
  title = "异常原因",
  text,
  pages,
  onClose,
}: {
  open: boolean;
  title?: string;
  text: string;
  pages?: number[];
  onClose: () => void;
}) {
  const message = text.trim();
  const failedPages = (pages ?? []).filter((page) => Number.isFinite(page));
  if (!open) return null;

  return (
    <Modal
      title={
        <span className={styles.withIcon}>
          <InfoCircleOutlined />
          {title}
        </span>
      }
      open={open}
      width={720}
      zIndex={2000}
      centered
      destroyOnHidden
      onCancel={onClose}
      footer={
        <Button type="primary" onClick={onClose}>
          关闭
        </Button>
      }
    >
      <div className={styles.errorModalBody}>
        {failedPages.length > 0 ? (
          <div className={styles.errorPages}>
            <div className={styles.errorPagesLabel}>失败页 {failedPages.length} 个</div>
            <div className={styles.errorPagesBody}>{failedPages.join("、")}</div>
          </div>
        ) : null}
        {message ? <div className={styles.errorDetail}>{message}</div> : null}
      </div>
    </Modal>
  );
}

export function ErrorDetailButton({
  text,
  title = "异常原因",
  pages,
}: {
  text: string;
  title?: string;
  pages?: number[];
}) {
  const [open, setOpen] = useState(false);
  const message = text.trim();
  const failedPages = pages ?? [];
  if (!message && failedPages.length === 0) return null;

  return (
    <>
      <Button
        type="text"
        size="small"
        icon={<InfoCircleOutlined />}
        className={styles.actionButton}
        onClick={() => setOpen(true)}
      >
        详情
      </Button>
      <ErrorDetailModal
        open={open}
        title={title}
        text={message}
        pages={failedPages}
        onClose={() => setOpen(false)}
      />
    </>
  );
}
