"use client";

import React from "react";
import { Button, Space } from "antd";
import { ExclamationCircleFilled, SaveOutlined } from "@ant-design/icons";
import styles from "./index.module.less";

export interface UnsavedChangesBarProps {
  /** 是否处于未保存状态 */
  visible: boolean;
  /** 保存操作中 loading */
  saving?: boolean;
  /** 放弃更改回调 */
  onDiscard: () => void;
  /** 保存更改回调 */
  onSave: () => void;
  /** 自定义提示文字，默认 "有未保存的更改" */
  message?: React.ReactNode;
  /** 放弃按钮文字，默认 "放弃更改" */
  discardText?: string;
  /** 保存按钮文字，默认 "保存更改" */
  saveText?: string;
  /** 保存按钮是否禁用 */
  saveDisabled?: boolean;
  /** 放弃按钮是否禁用 */
  discardDisabled?: boolean;
  /** 额外元素 */
  extra?: React.ReactNode;
}

export default function UnsavedChangesBar({
  visible,
  saving = false,
  onDiscard,
  onSave,
  message = "有未保存的更改",
  discardText = "放弃更改",
  saveText = "保存更改",
  saveDisabled = false,
  discardDisabled = false,
  extra,
}: UnsavedChangesBarProps) {
  return (
    <aside
      className={`${styles.barContainer} ${visible ? styles.barVisible : ""}`}
      aria-hidden={!visible}
      aria-live="polite"
    >
      <div className={styles.messageArea}>
        <ExclamationCircleFilled className={styles.icon} />
        <span className={styles.text}>{message}</span>
      </div>
      <div className={styles.actionArea}>
        {extra}
        <Button
          size="middle"
          disabled={saving || discardDisabled}
          onClick={onDiscard}
        >
          {discardText}
        </Button>
        <Button
          size="middle"
          type="primary"
          icon={<SaveOutlined />}
          loading={saving}
          disabled={saveDisabled}
          onClick={onSave}
        >
          {saveText}
        </Button>
      </div>
    </aside>
  );
}
