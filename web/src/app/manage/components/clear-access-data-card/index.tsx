"use client";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Alert,
  Button,
  Card,
  Flex,
  Input,
  Modal,
  Radio,
  Space,
  Tag,
  Typography,
} from "antd";
import { ClearOutlined, LineChartOutlined } from "@ant-design/icons";
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
  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();

  const [status, setStatus] = useState<AccessStatus | null>(null);
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
    }
  }, []);

  useEffect(() => {
    void fetchStatus();
  }, [fetchStatus]);

  // 展示规则：
  // 1. 数据分析开关打开 (enabled == true)：即使数据库里还没有数据（0 行），也必须展示该卡片！
  // 2. 数据分析开关没开启 (enabled == false)：如果数据库里有历史落库数据 (hasData == true / totalRows > 0)，也必须展示该卡片！
  // 3. 只有当「开关未开启 且 库里无数据」时，才在数据安全中隐藏该卡片。
  const shouldShow = useMemo(() => {
    if (!status) return false;
    return status.enabled || status.hasData || (status.totalRows ?? 0) > 0;
  }, [status]);

  if (!shouldShow) {
    return null;
  }

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
            <span>数据分析清理</span>
          </Space>
        }
        extra={
          <Space size={8}>
            {status?.enabled ? (
              <Tag color="processing">已开启</Tag>
            ) : (
              <Tag color="default">未开启</Tag>
            )}
            {!isAdmin && <Tag color="default">仅超级管理员可操作</Tag>}
          </Space>
        }
      >
        <div className={styles.sectionHead}>
          <div className={styles.sectionText}>
            <Typography.Text strong>清理历史统计数据</Typography.Text>
            <Typography.Text type="secondary">
              清空站点访问流量与统计数据，支持全部清空或按天数保留。
            </Typography.Text>
          </div>
          <Button
            danger
            icon={<ClearOutlined />}
            disabled={!canWrite || !isAdmin}
            onClick={openModal}
          >
            清理数据
          </Button>
        </div>
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
            description="清理后将永久删除所选时间范围内的访问与统计数据，无法恢复。"
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
