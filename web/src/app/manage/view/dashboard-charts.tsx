"use client";

import React, { useEffect, useState, useMemo } from "react";
import { Card, Tag, Typography, Spin, Button, Switch, message } from "antd";
import {
  DatabaseOutlined,
  ReloadOutlined,
  FileTextOutlined,
  VideoCameraOutlined,
  ClockCircleOutlined,
  ThunderboltOutlined,
  SyncOutlined,
  ClearOutlined,
} from "@ant-design/icons";
import { ApiGet, ApiPost } from "@/lib/client-api";
import { useManagePermission } from "@/lib/manage-permission";
import styles from "./dashboard-charts.module.less";

const { Text } = Typography;

interface CronTaskItem {
  id: string;
  time: number;
  spec: string;
  model: number;
  state: boolean;
  remark: string;
  cid?: number;
}

interface InventoryStats {
  films: number;
  snapshots: number;
  categories: number;
  failures: number;
}

export default function DashboardCharts() {
  const { canWrite } = useManagePermission();
  const [cronTasks, setCronTasks] = useState<CronTaskItem[]>([]);
  const [stats, setStats] = useState<InventoryStats | null>(null);
  const [loading, setLoading] = useState(true);

  const fetchRealData = async () => {
    setLoading(true);
    try {
      const [cronResp, statsResp] = await Promise.all([
        ApiGet<CronTaskItem[]>("/manage/cron/list").catch(() => null),
        ApiGet<InventoryStats>("/manage/spider/clear/stats").catch(() => null),
      ]);

      if (cronResp?.code === 0 && Array.isArray(cronResp.data)) {
        setCronTasks(cronResp.data);
      }
      if (statsResp?.code === 0 && statsResp.data) {
        setStats(statsResp.data);
      }
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void fetchRealData();
  }, []);

  const handleToggleTask = async (task: CronTaskItem) => {
    try {
      const resp = await ApiPost("/manage/cron/change", {
        id: task.id,
        state: !task.state,
      });
      if (resp.code === 0) {
        message.success(`${task.remark || "任务"} 状态已切换`);
        void fetchRealData();
      } else {
        message.error(resp.msg || "操作失败");
      }
    } catch {
      // 拦截器已统一提示，避免重复弹窗
    }
  };

  // 1. 真实定时任务调度计算
  const taskModelMap: Record<number, { title: string; icon: React.ReactNode; defaultSpecText: string }> = {
    0: { title: "自动增量采集", icon: <SyncOutlined style={{ color: "#fa8c16" }} />, defaultSpecText: "每20分钟执行" },
    1: { title: "全量更新任务", icon: <ThunderboltOutlined style={{ color: "#3b82f6" }} />, defaultSpecText: "自定义周期" },
    2: { title: "失败记录重试", icon: <ClockCircleOutlined style={{ color: "#10b981" }} />, defaultSpecText: "每周重试" },
    3: { title: "孤儿清洗任务", icon: <ClearOutlined style={{ color: "#8b5cf6" }} />, defaultSpecText: "每天清洗" },
  };

  // 2. 真实影视数据规模占比计算
  const inventoryMetrics = useMemo(() => {
    const films = stats?.films ?? 0;
    const snapshots = stats?.snapshots ?? 0;
    const failures = stats?.failures ?? 0;
    const categories = stats?.categories ?? 0;

    const totalRecords = films + snapshots + failures || 1;

    const filmsPct = Math.round((films / totalRecords) * 100);
    const snapshotsPct = Math.round((snapshots / totalRecords) * 100);
    const failuresPct = Math.max(0, 100 - filmsPct - snapshotsPct);

    return {
      films,
      snapshots,
      failures,
      categories,
      totalRecords,
      filmsPct,
      snapshotsPct,
      failuresPct,
    };
  }, [stats]);

  if (loading && cronTasks.length === 0 && !stats) {
    return (
      <div className={styles.loadingWrapper}>
        <Spin description="正在加载系统实时状态数据..." />
      </div>
    );
  }

  return (
    <div className={styles.chartsGrid}>
      {/* 模块 1: 定时任务与自动化调度 */}
      <Card
        className={styles.chartCard}
        title={
          <div className={styles.cardHeaderTitle}>
            <span>定时任务自动化调度</span>
          </div>
        }
        extra={
          <Button
            type="text"
            size="small"
            icon={<ReloadOutlined />}
            onClick={() => void fetchRealData()}
            loading={loading}
          >
            刷新
          </Button>
        }
      >
        <div className={styles.cronLayout}>
          <div className={styles.cronList}>
            {cronTasks.length > 0 ? (
              cronTasks.map((task) => {
                const meta = taskModelMap[task.model] || {
                  title: task.remark || "定时任务",
                  icon: <ClockCircleOutlined style={{ color: "#fa8c16" }} />,
                  defaultSpecText: task.spec,
                };
                return (
                  <div key={task.id} className={styles.cronItem}>
                    <div className={styles.cronItemMain}>
                      <div className={styles.cronIconWrap}>{meta.icon}</div>
                      <div className={styles.cronInfo}>
                        <div className={styles.cronTitleGroup}>
                          <span className={styles.cronTitle}>
                            {meta.title}
                          </span>
                          <Tag color="orange" variant="filled" className={styles.cronSpecTag}>
                            {task.spec}
                          </Tag>
                        </div>
                        <div className={styles.cronDesc}>
                          {task.remark || meta.defaultSpecText}
                        </div>
                      </div>
                    </div>
                    <div className={styles.cronAction}>
                      <Switch
                        size="small"
                        checked={task.state}
                        disabled={!canWrite}
                        onChange={() => void handleToggleTask(task)}
                      />
                    </div>
                  </div>
                );
              })
            ) : (
              <div className={styles.emptyCronNotice}>
                <Text type="secondary" style={{ fontSize: 12 }}>
                  暂未注册后台 Cron 任务，默认每 20 分钟定期轮询
                </Text>
              </div>
            )}
          </div>

          <div className={styles.donutFooterRow}>
            <Text type="secondary" className={styles.donutFooterText}>
              根据系统真实接口 `/manage/cron/list` 实时调度
            </Text>
          </div>
        </div>
      </Card>

      {/* 模块 2: 真实影视数据体量构成 */}
      <Card
        className={styles.chartCard}
        title={
          <div className={styles.cardHeaderTitle}>
            <span>影视数据体量构成</span>
          </div>
        }
        extra={
          <Tag color="orange" variant="filled">
            {inventoryMetrics.categories} 个分类
          </Tag>
        }
      >
        <div className={styles.inventoryLayout}>
          <div className={styles.inventoryKpiRow}>
            <div className={styles.invKpiCard}>
              <div className={styles.invKpiHeader}>
                <VideoCameraOutlined style={{ color: "#fa8c16" }} />
                <span>影视库存</span>
              </div>
              <div className={styles.invKpiValue}>
                {inventoryMetrics.films.toLocaleString()}
              </div>
            </div>

            <div className={styles.invKpiCard}>
              <div className={styles.invKpiHeader}>
                <DatabaseOutlined style={{ color: "#3b82f6" }} />
                <span>列表快照</span>
              </div>
              <div className={styles.invKpiValue}>
                {inventoryMetrics.snapshots.toLocaleString()}
              </div>
            </div>

            <div className={styles.invKpiCard}>
              <div className={styles.invKpiHeader}>
                <FileTextOutlined style={{ color: "#ef4444" }} />
                <span>失败记录</span>
              </div>
              <div className={styles.invKpiValue}>
                {inventoryMetrics.failures.toLocaleString()}
              </div>
            </div>
          </div>

          <div className={styles.progressSection}>
            <div className={styles.progressHeader}>
              <Text type="secondary" style={{ fontSize: 12 }}>
                存储资源体量占比比例
              </Text>
            </div>
            {/* 综合组合进度条 */}
            <div className={styles.stackedBar}>
              <div
                className={styles.stackedSegment}
                style={{
                  width: `${inventoryMetrics.filmsPct}%`,
                  backgroundColor: "#fa8c16",
                }}
                title={`影视库存: ${inventoryMetrics.filmsPct}%`}
              />
              <div
                className={styles.stackedSegment}
                style={{
                  width: `${inventoryMetrics.snapshotsPct}%`,
                  backgroundColor: "#3b82f6",
                }}
                title={`列表快照: ${inventoryMetrics.snapshotsPct}%`}
              />
              <div
                className={styles.stackedSegment}
                style={{
                  width: `${inventoryMetrics.failuresPct}%`,
                  backgroundColor: "#ef4444",
                }}
                title={`失败记录: ${inventoryMetrics.failuresPct}%`}
              />
            </div>
          </div>

          <div className={styles.donutLegendList}>
            <div className={styles.donutLegendItem}>
              <div className={styles.legendNameGroup}>
                <span
                  className={styles.legendColorBadge}
                  style={{ backgroundColor: "#fa8c16" }}
                />
                <span className={styles.legendName}>影视库存</span>
              </div>
              <div className={styles.legendMetrics}>
                <span className={styles.legendPercent}>
                  {inventoryMetrics.filmsPct}%
                </span>
                <span className={styles.legendCount}>
                  {inventoryMetrics.films.toLocaleString()} 条
                </span>
              </div>
            </div>

            <div className={styles.donutLegendItem}>
              <div className={styles.legendNameGroup}>
                <span
                  className={styles.legendColorBadge}
                  style={{ backgroundColor: "#3b82f6" }}
                />
                <span className={styles.legendName}>列表快照</span>
              </div>
              <div className={styles.legendMetrics}>
                <span className={styles.legendPercent}>
                  {inventoryMetrics.snapshotsPct}%
                </span>
                <span className={styles.legendCount}>
                  {inventoryMetrics.snapshots.toLocaleString()} 条
                </span>
              </div>
            </div>

            <div className={styles.donutLegendItem}>
              <div className={styles.legendNameGroup}>
                <span
                  className={styles.legendColorBadge}
                  style={{ backgroundColor: "#ef4444" }}
                />
                <span className={styles.legendName}>失败记录</span>
              </div>
              <div className={styles.legendMetrics}>
                <span className={styles.legendPercent}>
                  {inventoryMetrics.failuresPct}%
                </span>
                <span className={styles.legendCount}>
                  {inventoryMetrics.failures.toLocaleString()} 条
                </span>
              </div>
            </div>
          </div>

          <div className={styles.donutFooterRow}>
            <Text type="secondary" className={styles.donutFooterText}>
              根据系统真实接口 `/manage/spider/clear/stats` 实时统计
            </Text>
          </div>
        </div>
      </Card>
    </div>
  );
}
