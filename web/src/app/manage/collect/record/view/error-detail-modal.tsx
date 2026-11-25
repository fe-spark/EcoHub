import React from "react";
import { Button, Modal } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import { parseCollectError } from "./record-format";
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
  const parsed = parseCollectError(text);
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
      width={560}
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
        {parsed.summary ? (
          <div className={styles.errorReasonBox}>
            <div className={styles.errorReasonMain}>{parsed.summary}</div>
            {parsed.detail ? <div className={styles.errorReasonRaw}>{parsed.detail}</div> : null}
          </div>
        ) : parsed.detail ? (
          <div className={styles.errorDetailPlain}>{parsed.detail}</div>
        ) : null}
        {parsed.url ? (
          <section className={styles.errorSection}>
            <div className={styles.errorSectionLabel}>请求地址</div>
            <div className={styles.errorUrl}>
              {parsed.method ? <span className={styles.errorMethod}>{parsed.method}</span> : null}
              <span className={styles.errorUrlText}>{parsed.url}</span>
            </div>
          </section>
        ) : null}
        {failedPages.length > 0 ? (
          <section className={styles.errorSection}>
            <div className={styles.errorSectionLabel}>失败页码 ({failedPages.length})</div>
            <div className={styles.errorPageList}>
              {failedPages.map((page, index) => (
                <span key={`${page}-${index}`} className={styles.errorPageChip}>
                  {page}
                </span>
              ))}
            </div>
          </section>
        ) : null}
      </div>
    </Modal>
  );
}
