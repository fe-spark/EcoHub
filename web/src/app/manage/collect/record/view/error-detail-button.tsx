import React, { useState } from "react";
import { Button, Modal } from "antd";
import { InfoCircleOutlined } from "@ant-design/icons";
import styles from "./index.module.less";

export function ErrorDetailButton({ text, title = "异常原因" }: { text: string; title?: string }) {
  const [open, setOpen] = useState(false);
  const message = text.trim();
  if (!message) return null;

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
      <Modal
        title={
          <span className={styles.withIcon}>
            <InfoCircleOutlined />
            {title}
          </span>
        }
        open={open}
        width={480}
        onCancel={() => setOpen(false)}
        footer={
          <Button type="primary" onClick={() => setOpen(false)}>
            关闭
          </Button>
        }
      >
        <div className={styles.errorDetail}>{message}</div>
      </Modal>
    </>
  );
}
