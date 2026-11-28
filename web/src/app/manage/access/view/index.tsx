"use client";

import { useEffect, useMemo, useState } from "react";
import { Badge, Button, DatePicker, Dropdown, Segmented, Space, Switch, Tabs, Tag } from "antd";
import type { MenuProps, TabsProps } from "antd";
import {
  AreaChartOutlined,
  DownOutlined,
  FireOutlined,
  ReloadOutlined,
  UnorderedListOutlined,
} from "@ant-design/icons";
import dayjs, { type Dayjs } from "dayjs";
import ManagePageHeader from "@/app/manage/components/page-header";
import { MODULE_TABS, PLATFORM_TABS } from "./constants";
import type { AnalyticsModule, AppPlatform } from "./types";
import { useAnalyticsDashboard } from "./use-analytics-dashboard";
import KpiOverviewGrid from "./components/KpiOverviewGrid";
import TrafficChartsSection from "./components/TrafficChartsSection";
import TopRankingsSection from "./components/TopRankingsSection";
import AccessLogsCard from "./components/AccessLogsCard";
import styles from "./index.module.less";

type WorkspaceTab = "traffic" | "content" | "logs";

const REFRESH_INTERVAL_KEY = "eh_access_refresh_interval";
const INTERVAL_OPTIONS = [
  { label: "5 秒", value: 5 },
  { label: "10 秒", value: 10 },
  { label: "15 秒", value: 15 },
  { label: "30 秒", value: 30 },
];

function disabledFutureDay(d: Dayjs) {
  return d.isAfter(dayjs(), "day");
}

export default function AccessPageView() {
  const [activeTab, setActiveTab] = useState<WorkspaceTab>("traffic");
  const [module, setModule] = useState<AnalyticsModule>("all");
  const [platform, setPlatform] = useState<AppPlatform>("all");
  const [selectedDay, setSelectedDay] = useState<Dayjs>(dayjs());
  const [refreshKey, setRefreshKey] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [isRefreshing, setIsRefreshing] = useState(false);

  const [refreshInterval, setRefreshInterval] = useState<number>(() => {
    if (typeof window !== "undefined") {
      const saved = Number(localStorage.getItem(REFRESH_INTERVAL_KEY));
      if ([5, 10, 15, 30].includes(saved)) return saved;
    }
    return 15;
  });

  const dayStr = selectedDay.format("YYYY-MM-DD");
  const todayStr = dayjs().format("YYYY-MM-DD");
  const yesterdayStr = dayjs().subtract(1, "day").format("YYYY-MM-DD");
  const isToday = dayStr === todayStr;

  const {
    data,
    loading,
    logs,
    logsTotal,
    logsLoading,
    logAction,
    logKeyword,
    logPage,
    logPageSize,
    setLogPage,
    setLogPageSize,
    handleLogFilterChange,
  } = useAnalyticsDashboard({ dayStr, module, platform, refreshKey });

  useEffect(() => {
    if (!isToday || !autoRefresh || refreshInterval <= 0) return;
    const timer = window.setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) return;
      setRefreshKey((prev) => prev + 1);
    }, refreshInterval * 1000);
    return () => window.clearInterval(timer);
  }, [isToday, autoRefresh, refreshInterval]);

  const availableDaySet = useMemo(
    () => new Set(data?.availableDays || []),
    [data?.availableDays],
  );

  const triggerManualRefresh = () => {
    setIsRefreshing(true);
    setRefreshKey((prev) => prev + 1);
    window.setTimeout(() => setIsRefreshing(false), 500);
  };

  const handleDateChange = (d: Dayjs | null) => {
    if (!d) return;
    setSelectedDay(d);
    setAutoRefresh(d.format("YYYY-MM-DD") === todayStr);
    setLogPage(1);
    setRefreshKey((prev) => prev + 1);
  };

  const refreshMenuItems: MenuProps["items"] = [
    {
      key: "toggle",
      label: (
        <div
          style={{
            display: "flex",
            alignItems: "center",
            justifyContent: "space-between",
            minWidth: 130,
            gap: 12,
          }}
          onClick={(e) => e.stopPropagation()}
        >
          <span>自动刷新</span>
          <Switch size="small" checked={autoRefresh} onChange={setAutoRefresh} />
        </div>
      ),
    },
    { type: "divider" },
    {
      type: "group",
      label: "刷新间隔",
      children: INTERVAL_OPTIONS.map((opt) => ({
        key: String(opt.value),
        label: opt.label,
        disabled: !autoRefresh,
      })),
    },
  ];

  const handleRefreshMenuClick: MenuProps["onClick"] = ({ key }) => {
    if (key === "toggle") return;
    const val = Number(key);
    if (!Number.isNaN(val)) {
      setRefreshInterval(val);
      try {
        localStorage.setItem(REFRESH_INTERVAL_KEY, String(val));
      } catch {
        // 忽略本地存储写入受限异常
      }
    }
  };

  const tabItems: TabsProps["items"] = [
    {
      key: "traffic",
      label: (
        <Space size={6}>
          <AreaChartOutlined />
          <span>流量与终端</span>
        </Space>
      ),
    },
    {
      key: "content",
      label: (
        <Space size={6}>
          <FireOutlined />
          <span>内容与片源</span>
        </Space>
      ),
    },
    {
      key: "logs",
      label: (
        <Space size={6}>
          <UnorderedListOutlined />
          <span>实时流水</span>
          {logsTotal > 0 && (
            <Tag style={{ margin: 0, lineHeight: "18px", fontSize: 11 }}>
              {logsTotal > 999 ? "999+" : logsTotal}
            </Tag>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        className={styles.pageHeader}
        title="数据分析"
        description="全站流量趋势、终端构成、片源热度与实时访问流水。"
        actions={
          <Tag color={isToday ? "processing" : "default"} style={{ margin: 0 }}>
            {isToday ? "实时统计" : `历史归档 · ${dayStr}`}
          </Tag>
        }
      />

      {/* 吸顶全局控制栏：统一收拢终端范围、时间选择与刷新控制 */}
      <div className={styles.stickyBar}>
        <div className={styles.toolbarInner}>
          <Space size={10} wrap align="center">
            <Segmented
              value={module}
              options={MODULE_TABS}
              onChange={(val) => {
                setModule(val as AnalyticsModule);
                setLogPage(1);
              }}
            />
            {module === "app" && (
              <Segmented
                size="small"
                value={platform}
                options={PLATFORM_TABS}
                onChange={(val) => {
                  setPlatform(val as AppPlatform);
                  setLogPage(1);
                }}
              />
            )}
          </Space>

          <Space size={8} wrap align="center">
            <Button
              size="middle"
              type={dayStr === todayStr ? "primary" : "default"}
              ghost={dayStr === todayStr}
              onClick={() => handleDateChange(dayjs())}
            >
              今天
            </Button>
            <Button
              size="middle"
              type={dayStr === yesterdayStr ? "primary" : "default"}
              ghost={dayStr === yesterdayStr}
              onClick={() => handleDateChange(dayjs().subtract(1, "day"))}
            >
              昨天
            </Button>
            <DatePicker
              allowClear={false}
              value={selectedDay}
              onChange={handleDateChange}
              disabledDate={disabledFutureDay}
              cellRender={(current, info) => {
                if (info.type !== "date") return info.originNode;
                const d = current as Dayjs;
                const hasData = availableDaySet.has(d.format("YYYY-MM-DD"));
                return (
                  <div className="ant-picker-cell-inner">
                    {d.date()}
                    {hasData && <span className={styles.dateDot} />}
                  </div>
                );
              }}
              style={{ width: 136 }}
            />
            {isToday ? (
              <Space.Compact>
                <Button onClick={triggerManualRefresh}>
                  <Space size={6} align="center">
                    <ReloadOutlined spin={isRefreshing} />
                    <span>刷新</span>
                    {autoRefresh && (
                      <span style={{ fontSize: 12, color: "var(--ant-color-text-tertiary)" }}>
                        {refreshInterval}s
                      </span>
                    )}
                    <Badge status={autoRefresh ? "processing" : "default"} />
                  </Space>
                </Button>
                <Dropdown
                  placement="bottomRight"
                  menu={{
                    items: refreshMenuItems,
                    selectable: true,
                    selectedKeys: autoRefresh ? [String(refreshInterval)] : [],
                    onClick: handleRefreshMenuClick,
                  }}
                >
                  <Button icon={<DownOutlined />} />
                </Dropdown>
              </Space.Compact>
            ) : (
              <Button icon={<ReloadOutlined spin={isRefreshing} />} onClick={triggerManualRefresh}>
                刷新
              </Button>
            )}
          </Space>
        </div>
      </div>

      {/* 核心 KPI 条：始终可见，提供跨视角全局体感 */}
      <KpiOverviewGrid
        globalOverview={data?.global}
        scopedOverview={data?.scoped}
        module={module}
        platform={platform}
        loading={loading}
      />

      {/* 三视角工作区导航 */}
      <div className={styles.workspaceTabs}>
        <Tabs
          activeKey={activeTab}
          items={tabItems}
          onChange={(key) => setActiveTab(key as WorkspaceTab)}
          tabBarStyle={{ marginBottom: 0 }}
        />
      </div>

      {/* 按当前视角条件渲染，避免不可见图表无谓挂载及零宽度问题 */}
      {activeTab === "traffic" && (
        <TrafficChartsSection
          globalOverview={data?.global}
          scopedOverview={data?.scoped}
          module={module}
          platform={platform}
          loading={loading}
        />
      )}

      {activeTab === "content" && (
        <TopRankingsSection
          sources={data?.sources || []}
          playTops={data?.playTops || []}
          searchTops={data?.searchTops || []}
          classifyTops={data?.classifyTops || []}
          pageTops={data?.pageTops || []}
          dayStr={dayStr}
          module={module}
          platform={platform}
          loading={loading}
        />
      )}

      {activeTab === "logs" && (
        <AccessLogsCard
          module={module}
          logs={logs}
          total={logsTotal}
          loading={logsLoading || (loading && logs.length === 0)}
          action={logAction}
          keyword={logKeyword}
          page={logPage}
          pageSize={logPageSize}
          onFilterChange={handleLogFilterChange}
          onPageChange={(nextPage, nextSize) => {
            setLogPage(nextPage);
            setLogPageSize(nextSize);
          }}
        />
      )}
    </div>
  );
}
