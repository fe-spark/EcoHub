"use client";

import React, { useCallback, useEffect, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Divider,
  Flex,
  Input,
  Modal,
  Radio,
  Space,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  AreaChartOutlined,
  ArrowRightOutlined,
  ClearOutlined,
  LineChartOutlined,
} from "@ant-design/icons";
import { useRouter } from "next/navigation";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useAppMessage } from "@/lib/useAppMessage";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./index.module.less";

interface AccessStatus {
  enabled: boolean;
  hasData: boolean;
  totalRows?: number;
}

interface ClearAccessDataCardProps {
  onCleanComplete?: () => void;
}

export default function ClearAccessDataCard({ onCleanComplete }: ClearAccessDataCardProps) {
  const router = useRouter();
  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();

  const [status, setStatus] = useState<AccessStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [toggling, setToggling] = useState(false);
  const [modalOpen, setModalOpen] = useState(false);
  const [cleaning, setCleaning] = useState(false);
  const [password, setPassword] = useState("");
  const [retentionDays, setRetentionDays] = useState<number>(0);

  const fetchStatus = useCallback(async () => {
    try {
      const resp = await ApiGet<AccessStatus>("/manage/access/status");
      if (resp.code === 0 && resp.data) {
        setStatus(resp.data);
      }
    } catch {
      // ignore
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void fetchStatus();
    const handleConfigChange = () => {
      void fetchStatus();
    };
    window.addEventListener("ecohub:access-config-changed", handleConfigChange);
    return () => {
      window.removeEventListener("ecohub:access-config-changed", handleConfigChange);
    };
  }, [fetchStatus]);

  const hasData = Boolean(status?.hasData && (status?.totalRows ?? 0) > 0);

  const handleToggleEnabled = async (checked: boolean) => {
    if (!canWrite || !isAdmin) return;
    setToggling(true);
    try {
      const resp = await ApiPost<{ enabled: boolean }>("/manage/access/config", {
        enabled: checked,
      });
      if (resp.code === 0) {
        setStatus((prev) =>
          prev
            ? { ...prev, enabled: checked }
            : { enabled: checked, hasData: false, totalRows: 0 }
        );
        window.dispatchEvent(
          new CustomEvent("ecohub:access-config-changed", { detail: { enabled: checked } })
        );
        message.success(checked ? "数据分析已开启" : "数据分析已暂停");
      } else {
        message.error(resp.msg || "修改数据分析状态失败");
      }
    } catch {
      message.error("网络请求失败，请稍后重试");
    } finally {
      setToggling(false);
    }
  };

  const openModal = () => {
    setModalOpen(true);
    setPassword("");
    setRetentionDays(0);
  };

  const closeModal = () => {
    if (cleaning) return;
    setModalOpen(false);
    setPassword("");
  };

  const handleClean = async () => {
    if (!password.trim()) {
      message.error("请输入管理密码");
      return;
    }
    setCleaning(true);
    try {
      const resp = await ApiPost<{
        deletedDailyStats: number;
        deletedDailyTop: number;
        deletedRedisKeys: number;
      }>("/manage/access/clean", {
        password,
        retentionDays,
      });
      if (resp.code === 0) {
        message.success(resp.msg || "数据分析数据清理成功");
        setModalOpen(false);
        setPassword("");
        await fetchStatus();
        onCleanComplete?.();
        return;
      }
      message.error(resp.msg || "清理失败");
    } catch {
      message.error("清理请求失败，请检查网络或稍后重试");
    } finally {
      setCleaning(false);
    }
  };

  return (
    <>
      <Card
        className={styles.card}
        title={
          <Space size={8} align="center">
            <LineChartOutlined style={{ color: "var(--ant-color-primary)" }} />
            <span>数据分析</span>
          </Space>
        }
        extra={
          <Space size={8}>
            {status?.enabled && (
              <Button
                type="link"
                size="small"
                icon={<AreaChartOutlined />}
                onClick={() => router.push("/manage/access")}
                style={{ padding: "0 4px" }}
              >
                前往看板 <ArrowRightOutlined />
              </Button>
            )}
            {!isAdmin && <Tag color="default">仅超级管理员可操作</Tag>}
          </Space>
        }
      >
        <Flex vertical gap={16}>
          {/* 启停总开关 */}
          <div className={styles.sectionHead}>
            <div className={styles.sectionText}>
              <Space size={8} align="center">
                <Typography.Text strong>全站数据分析与行为统计</Typography.Text>
                {status?.enabled ? (
                  <Tag color="processing">运行中</Tag>
                ) : (
                  <Tag color="default">已暂停</Tag>
                )}
              </Space>
              <Typography.Text type="secondary">
                开启后系统记录全站访问量 (PV/UV)、播放热度、搜索词与客户端分布，并在侧边栏展示数据分析看板；关闭后停止记录，保持零 I/O 零性能开销。
              </Typography.Text>
            </div>
            <Switch
              checked={Boolean(status?.enabled)}
              loading={loading || toggling}
              disabled={loading || !canWrite || !isAdmin}
              onChange={handleToggleEnabled}
            />
          </div>

          <Divider style={{ margin: "4px 0" }} />

          {/* 清理历史统计数据 */}
          <div className={styles.sectionHead}>
            <div className={styles.sectionText}>
              <Typography.Text strong>清理历史统计数据</Typography.Text>
              <Typography.Text type="secondary">
                {loading
                  ? "正在检测历史统计数据..."
                  : hasData
                    ? `清空站点访问流量与统计数据，当前已累计保存 ${status?.totalRows} 条历史数据，支持全部清空或按天数保留（需管理密码）。`
                    : "当前暂无历史落库统计数据，无需清理。"}
              </Typography.Text>
            </div>
            <Tooltip
              title={
                loading
                  ? undefined
                  : !isAdmin
                    ? "仅超级管理员可操作"
                    : !canWrite
                      ? "当前账号无写入权限"
                      : !hasData
                        ? "当前暂无历史数据，无需清理"
                        : undefined
              }
            >
              <span style={{ display: "inline-block" }}>
                <Button
                  danger
                  icon={<ClearOutlined />}
                  disabled={loading || !canWrite || !isAdmin || !hasData}
                  onClick={openModal}
                >
                  清理数据
                </Button>
              </span>
            </Tooltip>
          </div>
        </Flex>
      </Card>

      <Modal
        title="清理数据分析"
        open={modalOpen}
        onCancel={closeModal}
        onOk={() => void handleClean()}
        okText="确认清理"
        confirmLoading={cleaning}
        okButtonProps={{ danger: true }}
        destroyOnHidden
        width={480}
      >
        <Flex vertical gap={16}>
          <Alert
            type="warning"
            showIcon
            title="该操作不可逆"
            description="删除所选时段的访问统计，不可恢复。"
          />
          <div>
            <Typography.Text strong style={{ display: "block", marginBottom: 8 }}>
              清理范围
            </Typography.Text>
            <Radio.Group
              value={retentionDays}
              onChange={(e) => setRetentionDays(e.target.value as number)}
              style={{ display: "flex", flexDirection: "column", gap: 10 }}
            >
              <Radio value={0}>全部清空（删除全部历史统计数据）</Radio>
              <Radio value={7}>保留最近 7 天</Radio>
              <Radio value={14}>保留最近 14 天</Radio>
              <Radio value={30}>保留最近 30 天</Radio>
            </Radio.Group>
          </div>
          <div>
            <Typography.Text strong style={{ display: "block", marginBottom: 8 }}>
              管理密码
            </Typography.Text>
            <Input.Password
              placeholder="请输入管理密码"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              onPressEnter={() => void handleClean()}
              autoComplete="current-password"
            />
          </div>
        </Flex>
      </Modal>
    </>
  );
}
