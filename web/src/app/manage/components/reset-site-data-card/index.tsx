import React, { useCallback, useEffect, useRef, useState } from "react";
import { Alert, Button, Card, Flex, Input, Modal, Progress, Space, Typography } from "antd";
import { DeleteOutlined, WarningOutlined } from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./index.module.less";

interface ResetProgress {
  running: boolean;
  percent: number;
  stage: string;
  error: string;
}

const POLL_INTERVAL = 800;

interface ResetSiteDataCardProps {
  /** 数据重置成功后触发（用于刷新页面上的影响面统计等） */
  onResetComplete?: () => void;
}

export default function ResetSiteDataCard({ onResetComplete }: ResetSiteDataCardProps) {
  const [resetOpen, setResetOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [resetting, setResetting] = useState(false);
  const [resetProgress, setResetProgress] = useState<ResetProgress | null>(null);
  const pollTimerRef = useRef<number | null>(null);
  const hideTimerRef = useRef<number | null>(null);
  const { message } = useAppMessage();
  const { canWrite } = useManagePermission();

  const stopPolling = useCallback(() => {
    if (pollTimerRef.current !== null) {
      window.clearInterval(pollTimerRef.current);
      pollTimerRef.current = null;
    }
    if (hideTimerRef.current !== null) {
      window.clearTimeout(hideTimerRef.current);
      hideTimerRef.current = null;
    }
  }, []);

  // 轮询后端真实进度，直到重置结束（成功或失败）
  const pollResetProgress = useCallback(() => {
    const tick = async () => {
      try {
        const resp = await ApiGet<ResetProgress>("/manage/spider/clear/progress");
        if (resp.code !== 0 || !resp.data) return;
        const p = resp.data;
        setResetProgress({ running: p.running, percent: p.percent, stage: p.stage, error: p.error });
        if (!p.running) {
          stopPolling();
          if (p.error) {
            message.error(`数据重置失败：${p.error}`);
          } else {
            message.success("数据重置完成");
            onResetComplete?.();
          }
          hideTimerRef.current = window.setTimeout(() => setResetProgress(null), 1500);
        }
      } catch {
        // 单次轮询失败不中断，等待下次
      }
    };
    void tick();
    pollTimerRef.current = window.setInterval(tick, POLL_INTERVAL);
  }, [message, onResetComplete, stopPolling]);

  const confirmReset = async () => {
    if (!password) {
      message.error("请输入管理密码");
      return;
    }
    setResetting(true);
    try {
      const resp = await ApiPost("/manage/spider/clear", { password });
      if (resp.code === 0) {
        setResetOpen(false);
        setPassword("");
        message.success("数据重置已发起，正在后台执行");
        setResetProgress({ running: true, percent: 0, stage: "正在启动重置", error: "" });
        pollResetProgress();
        return;
      }
      message.error(resp.msg || "数据重置发起失败");
    } finally {
      setResetting(false);
    }
  };

  // 组件卸载时清理轮询定时器
  useEffect(() => () => stopPolling(), [stopPolling]);

  return (
    <>
      <Card
        size="small"
        title={
          <Space>
            <WarningOutlined style={{ color: "#ff4d4f" }} />
            <span>危险操作</span>
          </Space>
        }
        className={styles.dangerCard}
      >

        <Flex vertical gap={12}>
          <Flex justify="space-between" align="center" gap={16} wrap="wrap">
            <Flex vertical gap={4} className={styles.dangerText}>
              <Typography.Text type="danger" strong>数据重置</Typography.Text>
              <Typography.Text type="secondary">
                清空影视库存、快照、分类与失败记录等采集派生数据；账号与配置类数据保留。体量可在工作台查看。
              </Typography.Text>
            </Flex>
            <Button
              danger
              icon={<DeleteOutlined />}
              disabled={!canWrite}
              onClick={() => setResetOpen(true)}
            >
              数据重置
            </Button>
          </Flex>

          {resetProgress !== null && (
            <div className={styles.resetProgress}>
              <Flex justify="space-between" align="center">
                <Typography.Text type="danger">
                  {resetProgress.running ? resetProgress.stage || "正在重置数据..." : resetProgress.error ? "重置失败" : "重置完成"}
                </Typography.Text>
                <Typography.Text type="secondary">{resetProgress.percent}%</Typography.Text>
              </Flex>
              <Progress
                percent={resetProgress.percent}
                status={!resetProgress.running && resetProgress.error ? "exception" : resetProgress.percent >= 100 ? "success" : "active"}
                strokeColor={{ from: "#ff7875", to: "#ff4d4f" }}
              />
            </div>
          )}
        </Flex>
      </Card>

      <Modal
        title="数据重置"
        open={resetOpen}
        onCancel={() => {
          setResetOpen(false);
          setPassword("");
        }}
        onOk={() => void confirmReset()}
        okText="确认重置"
        confirmLoading={resetting}
        okButtonProps={{ danger: true }}
        destroyOnHidden
      >
        <Flex vertical gap={12}>
          <Alert
            showIcon
            type="error"
            title="该操作不可逆"
            description="将清空影视库存、列表快照、播放源映射、失败记录与分类等采集派生数据，且无法恢复。清空完成后会自动同步主站分类，便于立即重新采集。网站配置、采集站、账号等不受影响。"
          />
          <Input.Password
            placeholder="请输入管理密码"
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
        </Flex>
      </Modal>
    </>
  );
}
