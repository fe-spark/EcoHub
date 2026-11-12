import React, { useState } from "react";
import { Button, Modal } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import styles from "./index.module.less";

// 旧记录把失败页缩成「第 1, 2... 页」，并把重复的 url= 截断。页码由 failedPages 完整展示，这里只留抓取原因。
export function collectErrorReason(text: string): string {
  let message = text.trim();
  const marker = "抓取失败:";
  const at = message.lastIndexOf(marker);
  if (at >= 0) {
    message = message.slice(at + marker.length).trim();
  }
  message = message.replace(/共\s*\d+\s*页失败\s*[（(]第[\s\S]*?\.\.\.\s*页[）)]/g, "").trim();
  message = message.replace(/,\s*url=https?:\/\/\S*\.\.\.$/i, "").trim();
  return message;
}

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
  const message = collectErrorReason(text);
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
        {message ? (
          <div>
            <div className={styles.errorPagesLabel}>抓取原因</div>
            <div className={styles.errorDetail}>{message}</div>
          </div>
        ) : null}
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
