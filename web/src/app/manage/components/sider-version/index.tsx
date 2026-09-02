"use client";

import React, { useEffect, useRef, useState } from "react";
import { Alert, Button, Modal, Space, Typography } from "antd";
import { CloudUploadOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
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
  canUpgrade?: boolean;
  upgradePhase?: string;
  upgradeError?: string;
}

const FALLBACK_CMD = "cd ~/ecohub\ndocker compose pull && docker compose up -d";

function sleep(ms: number) {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

export default function SiderVersion({
  collapsed,
  isAdmin = false,
}: {
  collapsed: boolean;
  isAdmin?: boolean;
}) {
  const [info, setInfo] = useState<AppVersionInfo | null>(null);
  const [open, setOpen] = useState(false);
  const [upgrading, setUpgrading] = useState(false);
  const [upgradeHint, setUpgradeHint] = useState("");
  const abortRef = useRef(false);
  const { message, modal } = useAppMessage();

  useEffect(() => {
    abortRef.current = false;
    // 是否查更新由服务端按 JWT 超管身份决定，不随 isAdmin 重拉
    ApiGet("/manage/version")
      .then((resp) => {
        if (resp.code === 0 && resp.data) {
          setInfo(resp.data as AppVersionInfo);
        }
      })
      .catch(() => {});
    return () => {
      abortRef.current = true;
    };
  }, []);

  const current = info?.current || "";
  const hasUpdate = Boolean(info?.hasUpdate) && isAdmin;
  const label = current ? `v${current.replace(/^v/i, "")}` : "—";

  const waitUntilBack = async () => {
    setUpgradeHint("正在拉取 latest…");
    let disconnected = false;
    for (let i = 0; i < 90 && !abortRef.current; i += 1) {
      await sleep(2000);
      try {
        const resp = await fetch("/api/manage/version", {
          credentials: "include",
          cache: "no-store",
        });
        if (!resp.ok) {
          disconnected = true;
          break;
        }
        const json = (await resp.json()) as {
          code?: number;
          data?: AppVersionInfo;
        };
        if (json.code === 0 && json.data?.upgradePhase === "failed") {
          throw new Error(json.data.upgradeError || "升级失败");
        }
        if (json.code === 0 && json.data?.upgradePhase === "recreating") {
          setUpgradeHint("正在重启容器…");
        }
      } catch (err) {
        if (err instanceof Error && err.message && err.message !== "Failed to fetch") {
          throw err;
        }
        disconnected = true;
        break;
      }
    }
    if (!disconnected) {
      throw new Error("仍在拉取镜像，请稍后重试或检查网络");
    }
    setUpgradeHint("服务重启中，等待恢复…");
    for (let i = 0; i < 60 && !abortRef.current; i += 1) {
      await sleep(2000);
      try {
        const resp = await fetch("/api/config/basic", { cache: "no-store" });
        if (resp.ok) {
          window.location.reload();
          return;
        }
      } catch {
        /* 重启窗口内连不上是预期 */
      }
    }
    throw new Error("重启后未恢复，请手动刷新页面");
  };

  const startUpgrade = async () => {
    if (!info || !isAdmin) {
      message.warning("仅超级管理员可执行版本升级");
      return;
    }
    if (info.breaking) {
      message.warning("本次为破坏性改动，请按 Release 说明手动升级");
      return;
    }
    if (!info.canUpgrade) {
      message.warning("当前环境未挂载 docker.sock，无法在线重启");
      return;
    }
    abortRef.current = false;
    setUpgrading(true);
    setUpgradeHint("已提交升级");
    try {
      const resp = await ApiPost("/manage/version/upgrade");
      if (resp.code !== 0) {
        throw new Error(resp.msg || "升级请求失败");
      }
      message.success(resp.msg || "已开始升级");
      await waitUntilBack();
    } catch (err) {
      const text = err instanceof Error ? err.message : "升级失败";
      message.error(text);
      setUpgrading(false);
      setUpgradeHint("");
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
            {isAdmin && info?.releaseUrl ? (
              <Button href={info.releaseUrl} target="_blank" rel="noopener noreferrer">
                打开 Release
              </Button>
            ) : null}
            {hasUpdate && isAdmin ? (
              <Button
                type="primary"
                icon={<CloudUploadOutlined />}
                loading={upgrading}
                disabled={
                  !isAdmin || !info?.canUpgrade || !!info?.breaking || upgrading
                }
                onClick={() => {
                  modal.confirm({
                    title: "立即升级并重启？",
                    content: "将拉取 latest 并重建当前容器，页面会短暂断开。",
                    okText: "升级",
                    cancelText: "取消",
                    onOk: () => startUpgrade(),
                  });
                }}
              >
                {upgrading ? "正在升级" : "立即升级并重启"}
              </Button>
            ) : null}
          </Space>
        }
      >
        <div className={styles.modalBody}>
          <Typography.Text type="secondary">
            当前 {label}
            {isAdmin && info?.latest ? `  ·  最新 ${info.latest}` : ""}
          </Typography.Text>
          {isAdmin && hasUpdate && info?.breaking ? (
            <Alert
              type="warning"
              showIcon
              title="本次为破坏性改动"
              description="请先按 Release 说明处理后再升级，不能使用在线重启。"
            />
          ) : null}
          {isAdmin && hasUpdate && !info?.canUpgrade && !info?.breaking ? (
            <Alert
              type="warning"
              showIcon
              title="当前环境不能在线重启"
              description="发布版需挂载 /var/run/docker.sock。也可在服务器执行下方命令。"
            />
          ) : null}
          {isAdmin && upgrading && upgradeHint ? (
            <Alert type="info" showIcon title="升级进行中" description={upgradeHint} />
          ) : null}
          {isAdmin ? (
            info?.releaseName || info?.releaseNotes ? (
              <pre className={styles.notes}>
                {[info.releaseName, info.releaseNotes].filter(Boolean).join("\n\n")}
              </pre>
            ) : (
              <Typography.Paragraph type="secondary">
                未能获取 GitHub Release（网络不可达时只显示当前版本）。
              </Typography.Paragraph>
            )
          ) : null}
          {isAdmin && hasUpdate && (!info?.canUpgrade || info?.breaking) ? (
            <>
              <Typography.Text type="secondary">
                到服务器安装目录执行：
              </Typography.Text>
              <pre className={styles.cmd}>{FALLBACK_CMD}</pre>
            </>
          ) : null}
        </div>
      </Modal>
    </>
  );
}
