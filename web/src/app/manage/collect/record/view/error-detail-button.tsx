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

const FETCH_ERROR = /^(GET|POST|PUT|DELETE|HEAD|PATCH)\s+"([^"]+)"\s*:\s*([\s\S]+)$/i;

const REASON_LABELS: Array<[RegExp, string]> = [
  [/context deadline exceeded|client\.timeout exceeded|i\/o timeout/i, "请求超时"],
  [/context canceled/i, "请求已取消"],
  [/connection refused/i, "连接被拒绝"],
  [/no such host/i, "域名无法解析"],
  [/tls handshake timeout/i, "TLS 握手超时"],
  [/unexpected eof|^eof$/i, "连接中断"],
  [/certificate/i, "证书校验失败"],
  [/network is unreachable|no route to host/i, "网络不可达"],
];

export function parseCollectError(text: string): {
  summary: string;
  detail: string;
  method: string;
  url: string;
} {
  const message = collectErrorReason(text);
  const matched = message.match(FETCH_ERROR);
  const detail = (matched?.[3] ?? message).trim();
  const summary = REASON_LABELS.find(([pattern]) => pattern.test(detail))?.[1] ?? "";
  return {
    summary,
    detail,
    method: matched ? matched[1].toUpperCase() : "",
    url: matched?.[2] ?? "",
  };
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
          <div className={styles.errorReason}>
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
            <div className={styles.errorSectionLabel}>失败页 {failedPages.length}</div>
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
