"use client";

import { useEffect, useState } from "react";
import { Badge, Button, DatePicker, Dropdown, Space, Switch } from "antd";
import type { MenuProps } from "antd";
import { DesktopOutlined, DownOutlined, MobileOutlined, PlaySquareOutlined, ReloadOutlined } from "@ant-design/icons";
import dayjs, { type Dayjs } from "dayjs";
import ManagePageHeader from "@/app/manage/components/page-header";
import GlobalOverviewBar from "./global-overview-bar";
import WebAnalyticsView from "./web-view";
import AppAnalyticsView from "./app-view";
import TvboxAnalyticsView from "./tvbox-view";
import styles from "./index.module.less";

function disabledAccessDay(d: Dayjs) {
  return d.isAfter(dayjs(), "day") || d.isBefore(dayjs().subtract(13, "day"), "day");
}

const REFRESH_INTERVAL_STORAGE_KEY = "eh_access_refresh_interval";

const INTERVAL_OPTIONS = [
  { label: "5 秒", value: 5 },
  { label: "10 秒", value: 10 },
  { label: "15 秒", value: 15 },
  { label: "30 秒", value: 30 },
];

const MODULE_OPTIONS: { key: "web" | "app" | "tvbox"; label: string; icon: React.ReactNode }[] = [
  {
    key: "web",
    label: "Web",
    icon: <DesktopOutlined />,
  },
  {
    key: "app",
    label: "App",
    icon: <MobileOutlined />,
  },
  {
    key: "tvbox",
    label: "TVBox",
    icon: <PlaySquareOutlined />,
  },
];

export default function AccessPageView() {
  const [activeModule, setActiveModule] = useState<"web" | "app" | "tvbox">("web");
  const [selectedDay, setSelectedDay] = useState<Dayjs>(dayjs());
  const [refreshKey, setRefreshKey] = useState(0);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [refreshInterval, setRefreshInterval] = useState<number>(() => {
    if (typeof window !== "undefined") {
      const saved = Number(localStorage.getItem(REFRESH_INTERVAL_STORAGE_KEY));
      if ([5, 10, 15, 30].includes(saved)) {
        return saved;
      }
    }
    return 15;
  });
  const dayStr = selectedDay.format("YYYY-MM-DD");
  const isToday = dayStr === dayjs().format("YYYY-MM-DD");

  const handleIntervalChange = (val: number) => {
    setRefreshInterval(val);
    try {
      localStorage.setItem(REFRESH_INTERVAL_STORAGE_KEY, String(val));
    } catch {
      // ignore
    }
  };

  // 自动刷新逻辑：仅在查看“今日”且开启自动刷新时，按指定周期轮询更新看板
  useEffect(() => {
    if (!isToday || !autoRefresh || refreshInterval <= 0) {
      return;
    }
    const timer = window.setInterval(() => {
      if (typeof document !== "undefined" && document.hidden) {
        return;
      }
      setRefreshKey((prev) => prev + 1);
    }, refreshInterval * 1000);

    return () => {
      window.clearInterval(timer);
    };
  }, [isToday, autoRefresh, refreshInterval, dayStr]);

  const [isRefreshing, setIsRefreshing] = useState(false);

  const handleDateChange = (d: Dayjs | null) => {
    if (!d) return;
    setSelectedDay(d);
    // 切换日期：重置定时刷新为开启，并重新开始计时与立即刷新
    setAutoRefresh(true);
    setRefreshKey((prev) => prev + 1);
    setIsRefreshing(true);
    setTimeout(() => {
      setIsRefreshing(false);
    }, 600);
  };

  const handleManualRefresh = () => {
    setIsRefreshing(true);
    setRefreshKey((prev) => prev + 1);
    setTimeout(() => {
      setIsRefreshing(false);
    }, 600);
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
            padding: "2px 0",
          }}
          onClick={(e) => e.stopPropagation()}
        >
          <span>自动刷新</span>
          <Switch
            size="small"
            checked={autoRefresh}
            onChange={(checked) => setAutoRefresh(checked)}
          />
        </div>
      ),
    },
    {
      type: "divider",
    },
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

  const handleMenuClick: MenuProps["onClick"] = ({ key }) => {
    if (key === "toggle") return;
    const interval = Number(key);
    if (!Number.isNaN(interval)) {
      handleIntervalChange(interval);
    }
  };

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="数据分析"
        actions={
          <Space size={12} align="center">
            {/* 日期选择：默认当天 */}
            <DatePicker
              allowClear={false}
              value={selectedDay}
              onChange={handleDateChange}
              disabledDate={disabledAccessDay}
              style={{ width: 130 }}
            />

            {/* 刷新与自动轮询组合 */}
            {isToday ? (
              <Dropdown.Button
                icon={<DownOutlined />}
                onClick={handleManualRefresh}
                menu={{
                  items: refreshMenuItems,
                  selectable: true,
                  selectedKeys: autoRefresh ? [String(refreshInterval)] : [],
                  onClick: handleMenuClick,
                }}
              >
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
              </Dropdown.Button>
            ) : (
              <Button icon={<ReloadOutlined spin={isRefreshing} />} onClick={handleManualRefresh}>
                刷新
              </Button>
            )}
          </Space>
        }
      />

      <GlobalOverviewBar
        dayStr={dayStr}
        refreshKey={refreshKey}
      />

      <div className={styles.moduleNavWrapper} role="tablist" aria-label="数据分析客户端分类">
        {MODULE_OPTIONS.map((item) => {
          const active = activeModule === item.key;
          return (
            <button
              key={item.key}
              type="button"
              role="tab"
              aria-selected={active}
              className={`${styles.moduleNavItem} ${active ? styles.moduleNavItemActive : ""}`}
              onClick={() => setActiveModule(item.key)}
            >
              <span className={styles.moduleNavIcon}>{item.icon}</span>
              <span className={styles.moduleNavLabel}>{item.label}</span>
            </button>
          );
        })}
      </div>

      {activeModule === "web" ? (
        <WebAnalyticsView dayStr={dayStr} refreshKey={refreshKey} />
      ) : activeModule === "app" ? (
        <AppAnalyticsView dayStr={dayStr} refreshKey={refreshKey} />
      ) : (
        <TvboxAnalyticsView dayStr={dayStr} refreshKey={refreshKey} />
      )}
    </div>
  );
}
