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

interface AccessDataStats {
  dailyStatsCount: number;
  dailyTopCount: number;
  redisKeyCount: number;
  earliestDay?: string;
  latestDay?: string;
  totalPv: number;
  totalUv: number;
}

interface ClearAccessDataCardProps {
  onCleanComplete?: () => void;
}

export default function ClearAccessDataCard({ onCleanComplete }: ClearAccessDataCardProps) {
  const { message } = useAppMessage();
  const { canWrite, isAdmin } = useManagePermission();

  const [status, setStatus] = useState<AccessStatus | null>(null);
  const [stats, setStats] = useState<AccessDataStats | null>(null);
  const [loadingStats, setLoadingStats] = useState(false);

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

  const fetchStats = useCallback(async () => {
    if (!isAdmin) return;
    setLoadingStats(true);
    try {
      const resp = await ApiGet<AccessDataStats>("/manage/access/stats");
      if (resp.code === 0 && resp.data) {
        setStats(resp.data);
      }
    } catch {
      // ignore
    } finally {
      setLoadingStats(false);
    }
  }, [isAdmin]);

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

  useEffect(() => {
    if (shouldShow && isAdmin) {
      void fetchStats();
    }
  }, [shouldShow, isAdmin, fetchStats]);

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
        if (isAdmin) {
          await fetchStats();
        }
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
            <span>数据分析数据清理</span>
          </Space>
        }
        extra={
          <Space size={8}>
            {status?.enabled ? (
              <Tag color="processing">数据分析已开启</Tag>
            ) : (
              <Tag color="default">数据分析未开启</Tag>
            )}
            {!isAdmin && <Tag color="default">仅超级管理员可操作</Tag>}
          </Space>
        }
      >
        <Flex vertical gap={16}>
          <div className={styles.sectionHead}>
            <div className={styles.sectionText}>
              <Typography.Text strong>清理历史积累数据</Typography.Text>
              <Typography.Text type="secondary">
                清理数据分析积累的按日汇总记录（MySQL 表 access_daily_stats）、Top 榜单（access_daily_top）以及 Redis 访问分析相关临时统计。支持全部清空或按保留天数清理。
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

          <div className={styles.statsGrid}>
            <div className={styles.statsItem}>
              <span className={styles.statsLabel}>汇总天数</span>
              <span className={styles.statsValue}>
                {stats != null
                  ? `${stats.dailyStatsCount} 天`
                  : loadingStats
                    ? "加载中..."
                    : "-"}
              </span>
            </div>
            <div className={styles.statsItem}>
              <span className={styles.statsLabel}>日期跨度</span>
              <span className={styles.statsValue}>
                {stats?.earliestDay && stats?.latestDay
                  ? stats.earliestDay === stats.latestDay
                    ? stats.earliestDay
                    : `${stats.earliestDay} ~ ${stats.latestDay}`
                  : "-"}
              </span>
            </div>
            <div className={styles.statsItem}>
              <span className={styles.statsLabel}>榜单明细</span>
              <span className={styles.statsValue}>
                {stats?.dailyTopCount != null
                  ? `${stats.dailyTopCount} 条`
                  : loadingStats
                    ? "加载中..."
                    : "-"}
              </span>
            </div>
            <div className={styles.statsItem}>
              <span className={styles.statsLabel}>累计流量</span>
              <span className={styles.statsValue}>
                {stats
                  ? `${stats.totalPv.toLocaleString()} PV · ${stats.totalUv.toLocaleString()} UV`
                  : loadingStats
                    ? "加载中..."
                    : "-"}
              </span>
            </div>
            <div className={styles.statsItem}>
              <span className={styles.statsLabel}>Redis 临时缓存</span>
              <span className={styles.statsValue}>
                {stats?.redisKeyCount != null
                  ? `${stats.redisKeyCount} 个键`
                  : loadingStats
                    ? "加载中..."
                    : "-"}
              </span>
            </div>
          </div>
        </Flex>
      </Card>

      <Modal
        title="清理数据分析数据"
        open={modalOpen}
        onCancel={closeModal}
        onOk={() => void handleClean()}
        okText="确认清理"
        confirmLoading={cleaning}
        okButtonProps={{ danger: true }}
        destroyOnHidden
        width={500}
      >
        <Flex vertical gap={16}>
          <Alert
            type="warning"
            showIcon
            title="该操作不可逆"
            description="清理后将永久删除所选范围内的数据分析按日汇总记录与临时缓存。若数据分析功能未开启且数据全部清空，此清理入口将自动隐藏。"
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
              <Radio value={0}>全部清空（删除所有历史落库记录与 Redis 临时缓存）</Radio>
              <Radio value={7}>保留最近 7 天（清理 7 天前的历史数据与缓存）</Radio>
              <Radio value={14}>保留最近 14 天（清理 14 天前的历史数据与缓存）</Radio>
              <Radio value={30}>保留最近 30 天（清理 30 天前的历史数据与缓存）</Radio>
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
