"use client";

import React, { useMemo, useState, useSyncExternalStore } from "react";
import { Button, Modal } from "antd";
import { BellOutlined } from "@ant-design/icons";
import { DEFAULT_NOTICE_TITLE, type NoticeConfig } from "@/lib/notice";
import styles from "./index.module.less";

interface NoticeModalProps {
  notice?: NoticeConfig | null;
  /** 受控模式弹窗开关（如后台预览） */
  open?: boolean;
  /** 受控模式关闭回调 */
  onClose?: () => void;
}

function getNoticeDismissKey(notice?: NoticeConfig | null): string {
  if (!notice) return "";
  const title = (notice.title || "").trim();
  const content = (notice.content || "").trim();
  return `ecohub_notice_dismissed_${title}_${content.length}_${content.slice(0, 24)}`;
}

const emptySubscribe = () => () => {};

export default function NoticeModal({ notice, open: controlledOpen, onClose }: NoticeModalProps) {
  const isControlled = controlledOpen !== undefined;
  const [localClosedKey, setLocalClosedKey] = useState<string | null>(null);

  const noticeKey = useMemo(() => getNoticeDismissKey(notice), [notice]);

  const isSessionDismissed = useSyncExternalStore(
    emptySubscribe,
    () => {
      if (!noticeKey || typeof window === "undefined") return false;
      try {
        return window.sessionStorage.getItem(noticeKey) === "1";
      } catch {
        return false;
      }
    },
    () => true,
  );

  const title = notice?.title?.trim() || DEFAULT_NOTICE_TITLE;
  const content = (notice?.content || "").trim();
  const isNoticeActive = Boolean(notice?.enabled && notice.showInWeb !== false && content);

  const isVisible = isControlled
    ? controlledOpen
    : isNoticeActive && !isSessionDismissed && localClosedKey !== noticeKey;

  const handleClose = () => {
    if (isControlled) {
      onClose?.();
      return;
    }

    if (noticeKey) {
      setLocalClosedKey(noticeKey);
      try {
        if (typeof window !== "undefined") {
          window.sessionStorage.setItem(noticeKey, "1");
        }
      } catch {
        // ignore sessionStorage errors
      }
    }
  };

  if (!isControlled && !content) return null;

  return (
    <Modal
      open={isVisible}
      onCancel={handleClose}
      footer={null}
      centered
      width={480}
      className={styles.modal}
      destroyOnHidden
    >
      <div className={styles.head}>
        <div className={styles.iconBadge}>
          <BellOutlined />
        </div>
        <h3 className={styles.title}>{title}</h3>
      </div>
      <div className={styles.body}>
        <p className={styles.content}>
          {content || "（暂无公告正文内容）"}
        </p>
      </div>
      <div className={styles.footer}>
        <Button
          type="primary"
          className={styles.confirmBtn}
          onClick={handleClose}
        >
          我知道了
        </Button>
      </div>
    </Modal>
  );
}
