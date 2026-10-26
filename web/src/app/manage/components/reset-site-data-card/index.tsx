import React, { useEffect, useRef, useState } from "react";
import { Alert, Button, Card, Flex, Input, Modal, Progress, Space, Tag, Typography } from "antd";
import { DeleteOutlined, WarningOutlined } from "@ant-design/icons";
import { fetchEventStream } from "@/lib/sse-client";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./index.module.less";

interface ResetProgress {
  running: boolean;
  percent: number;
  stage: string;
  error: string;
}

interface ResetSiteDataCardProps {
  /** 数据重置成功后触发（用于刷新页面上的影响面统计等） */
  onResetComplete?: () => void;
}

export default function ResetSiteDataCard({ onResetComplete }: ResetSiteDataCardProps) {
  const [resetOpen, setResetOpen] = useState(false);
  const [password, setPassword] = useState("");
  const [resetting, setResetting] = useState(false);
  const [resetProgress, setResetProgress] = useState<ResetProgress | null>(null);
  const postAbortRef = useRef<AbortController | null>(null);
  const hideTimerRef = useRef<number | null>(null);
  const seenRunningRef = useRef(false);
  const followRef = useRef<{ stop: () => void; start: () => void }>({
    stop: () => undefined,
    start: () => undefined,
  });
  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();
  const messageRef = useRef(message);
  const onResetCompleteRef = useRef(onResetComplete);
  const applyProgressRef = useRef<(data: ResetProgress) => void>(() => undefined);
  messageRef.current = message;
  onResetCompleteRef.current = onResetComplete;

  const applyProgress = (data: ResetProgress) => {
    if (!data.running && !seenRunningRef.current) {
      return;
    }
    setResetProgress({
      running: data.running,
      percent: data.percent ?? 0,
      stage: data.stage ?? "",
      error: data.error ?? "",
    });
    if (data.running) {
      seenRunningRef.current = true;
      return;
    }
    seenRunningRef.current = false;
    if (data.error) {
      messageRef.current.error(`数据重置失败：${data.error}`);
    } else {
      messageRef.current.success("数据重置完成");
      onResetCompleteRef.current?.();
    }
    if (hideTimerRef.current !== null) {
      window.clearTimeout(hideTimerRef.current);
    }
    hideTimerRef.current = window.setTimeout(() => setResetProgress(null), 1500);
  };
  applyProgressRef.current = applyProgress;

  useEffect(() => {
    if (!isAdmin) {
      followRef.current.stop();
      return;
    }
    let stopped = false;
    let timer: number | null = null;
    let ctrl: AbortController | null = null;

    const stop = () => {
      stopped = true;
      if (timer !== null) {
        window.clearTimeout(timer);
        timer = null;
      }
      ctrl?.abort();
      ctrl = null;
    };

    const run = async () => {
      if (stopped) {
        return;
      }
      const current = new AbortController();
      ctrl = current;
      let sawIdle = false;
      try {
        await fetchEventStream("/api/manage/spider/clear/progress", {
          method: "GET",
          signal: current.signal,
          onMessage: (data: ResetProgress) => {
            if (!data || typeof data.running !== "boolean") {
              return;
            }
            if (!data.running) {
              sawIdle = true;
            }
            applyProgressRef.current(data);
          },
        });
      } catch (err) {
        if (stopped || current.signal.aborted) {
          return;
        }
        const msg = err instanceof Error ? err.message : "";
        if (/401|403|鉴权|权限不足/.test(msg)) {
          return;
        }
      }
      if (stopped || current.signal.aborted || sawIdle) {
        return;
      }
      timer = window.setTimeout(() => void run(), 1500);
    };

    followRef.current = {
      stop,
      start: () => {
        stop();
        stopped = false;
        void run();
      },
    };
    void run();
    return () => {
      followRef.current = { stop: () => undefined, start: () => undefined };
      stop();
      postAbortRef.current?.abort();
      if (hideTimerRef.current !== null) {
        window.clearTimeout(hideTimerRef.current);
      }
    };
  }, [isAdmin]);

  const confirmReset = async () => {
    if (!password) {
      message.error("请输入管理密码");
      return;
    }
    followRef.current.stop();
    const ctrl = new AbortController();
    postAbortRef.current = ctrl;
    setResetting(true);
    let started = false;
    try {
      await fetchEventStream("/api/manage/spider/clear", {
        method: "POST",
        body: { password },
        signal: ctrl.signal,
        onMessage: (data: ResetProgress) => {
          if (!data || typeof data.running !== "boolean") {
            return;
          }
          if (!started) {
            started = true;
            setResetOpen(false);
            setPassword("");
          }
          applyProgress(data);
        },
      });
    } catch (err) {
      if (ctrl.signal.aborted) {
        return;
      }
      if (!started) {
        message.error(err instanceof Error ? err.message : "数据重置失败");
      }
    } finally {
      if (postAbortRef.current === ctrl) {
        postAbortRef.current = null;
      }
      setResetting(false);
      if (!ctrl.signal.aborted) {
        followRef.current.start();
      }
    }
  };

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
        extra={
          !isAdmin ? (
            <Tag color="default">仅超级管理员可操作</Tag>
          ) : null
        }
        className={styles.dangerCard}
      >

        <Flex vertical gap={12}>
          <Flex justify="space-between" align="center" gap={16} wrap="wrap">
            <Flex vertical gap={4} className={styles.dangerText}>
              <Typography.Text type="danger" strong>数据重置</Typography.Text>
              <Typography.Text type="secondary">
                清空影片档案、播放线路、失败记录和轮播，并关闭定时排片。账号、采集站、分类结构与排片规则保留。
              </Typography.Text>
            </Flex>
            <Button
              danger
              icon={<DeleteOutlined />}
              disabled={!canWrite || !isAdmin || resetting || Boolean(resetProgress?.running)}
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
            description="清空片库与分类，并关闭定时排片。采集站和账号保留。"
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
