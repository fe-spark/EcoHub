"use client";

import React from "react";
import { Modal, Progress, Alert, Button, Space } from "antd";
import {
  ThunderboltOutlined,
  LoadingOutlined,
  CheckCircleFilled,
} from "@ant-design/icons";

export interface BannerProgressModalProps {
  open: boolean;
  title?: string;
  percent: number;
  status: "normal" | "active" | "success" | "exception";
  stepText: string;
  errorMsg?: string;
  onClose: () => void;
}

export default function BannerProgressModal({
  open,
  title = "自动智能排片与 TMDB 刮削",
  percent,
  status,
  stepText,
  errorMsg,
  onClose,
}: BannerProgressModalProps) {
  return (
    <Modal
      open={open}
      title={
        <Space size={8} align="center">
          {status === "active" ? (
            <LoadingOutlined style={{ color: "var(--ant-color-primary)" }} />
          ) : status === "success" ? (
            <CheckCircleFilled style={{ color: "#52c41a" }} />
          ) : (
            <ThunderboltOutlined style={{ color: "var(--ant-color-primary)" }} />
          )}
          <span>{title}</span>
        </Space>
      }
      footer={
        status === "exception" ? (
          <Button type="primary" size="middle" onClick={onClose}>
            关闭
          </Button>
        ) : null
      }
      closable={status === "exception"}
      maskClosable={false}
      centered
      width={460}
    >
      <div style={{ padding: "16px 0 8px" }}>
        <Progress
          percent={percent}
          status={status}
          strokeColor={status === "exception" ? "#ff4d4f" : undefined}
        />
        <div
          style={{
            marginTop: 14,
            fontSize: 13,
            color:
              status === "success"
                ? "#52c41a"
                : status === "exception"
                ? "#ff4d4f"
                : "var(--ant-color-text-secondary)",
          }}
        >
          {stepText}
        </div>
        {errorMsg && (
          <Alert
            style={{ marginTop: 16 }}
            type="error"
            showIcon
            message="生成失败"
            description={errorMsg}
          />
        )}
      </div>
    </Modal>
  );
}
