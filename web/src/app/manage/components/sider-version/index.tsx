"use client";

import React, { useEffect, useState } from "react";
import { Alert, Button, Modal, Space, Typography } from "antd";
import { CloudUploadOutlined } from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import styles from "./index.module.less";

interface AppVersionInfo {
  current: string;
  latest?: string;
  hasUpdate?: boolean;
  releaseUrl?: string;
  releaseName?: string;
  releaseNotes?: string;
  breaking?: boolean;
}

function buildUpgradeScript() {
  return "cd ~/ecohub\ndocker compose pull && docker compose up -d";
}

export default function SiderVersion({
  collapsed,
  canWrite,
}: {
  collapsed: boolean;
  canWrite: boolean;
}) {
  const [info, setInfo] = useState<AppVersionInfo | null>(null);
  const [open, setOpen] = useState(false);
  const [copying, setCopying] = useState(false);
  const { message } = useAppMessage();

  useEffect(() => {
    ApiGet("/manage/version")
      .then((resp) => {
        if (resp.code === 0 && resp.data) {
          setInfo(resp.data as AppVersionInfo);
        }
      })
      .catch(() => {});
  }, []);

  const current = info?.current || "";
  const hasUpdate = Boolean(info?.hasUpdate);
  const label = current ? `v${current.replace(/^v/i, "")}` : "—";

  const copyUpgrade = async () => {
    if (!info || !canWrite) {
      message.warning("访客仅可查看，无法一键升级");
      return;
    }
    const script = buildUpgradeScript();
    setCopying(true);
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(script);
      } else {
        const ta = document.createElement("textarea");
        ta.value = script;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
      }
      message.success("已复制升级命令，请到服务器安装目录执行");
      if (info.releaseUrl) {
        window.open(info.releaseUrl, "_blank", "noopener,noreferrer");
      }
    } catch {
      message.error("复制失败，请手动复制弹窗中的命令");
    } finally {
      setCopying(false);
    }
  };

  return (
    <>
      <button
        type="button"
        className={`${styles.trigger} ${collapsed ? styles.triggerCollapsed : ""}`}
        onClick={() => setOpen(true)}
        title={hasUpdate ? `发现新版本 ${info?.latest}` : "当前版本"}
      >
        <span className={styles.label}>
          {label}
          {hasUpdate ? <span className={styles.dot} /> : null}
        </span>
      </button>
      <Modal
        title={hasUpdate ? `发现新版本 ${info?.latest}` : "当前版本"}
        open={open}
        onCancel={() => setOpen(false)}
        footer={
          <Space>
            {info?.releaseUrl ? (
              <Button href={info.releaseUrl} target="_blank" rel="noopener noreferrer">
                打开 Release
              </Button>
            ) : null}
            {hasUpdate ? (
              <Button
                type="primary"
                icon={<CloudUploadOutlined />}
                loading={copying}
                disabled={!canWrite}
                onClick={copyUpgrade}
              >
                一键升级
              </Button>
            ) : null}
          </Space>
        }
      >
        <div className={styles.modalBody}>
          <Typography.Text type="secondary">
            当前 {label}
            {info?.latest ? `  ·  最新 ${info.latest}` : ""}
          </Typography.Text>
          {hasUpdate && info?.breaking ? (
            <Alert
              type="warning"
              showIcon
              title="本次为破坏性改动"
              description="已部署旧版须先按 Release 说明处理（例如拷贝素材到卷），再 pull。全新安装不必执行迁移。"
            />
          ) : null}
          {info?.releaseName || info?.releaseNotes ? (
            <pre className={styles.notes}>
              {[info.releaseName, info.releaseNotes].filter(Boolean).join("\n\n")}
            </pre>
          ) : (
            <Typography.Paragraph type="secondary">
              未能获取 GitHub Release（网络不可达时只显示当前版本）。
            </Typography.Paragraph>
          )}
          {hasUpdate ? (
            <>
              <Typography.Text type="secondary">
                容器无法自行替换镜像。一键升级会复制下面的命令，请到服务器安装目录执行：
              </Typography.Text>
              <pre className={styles.cmd}>{buildUpgradeScript()}</pre>
            </>
          ) : null}
        </div>
      </Modal>
    </>
  );
}
