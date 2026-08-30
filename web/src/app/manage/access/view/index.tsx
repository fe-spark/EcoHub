"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import Link from "next/link";
import {
  Alert,
  Button,
  Card,
  DatePicker,
  Empty,
  Input,
  Radio,
  Select,
  Segmented,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from "antd";
import {
  ClockCircleOutlined,
  CompassOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  EyeOutlined,
  FireOutlined,
  InfoCircleOutlined,
  MobileOutlined,
  PieChartOutlined,
  PlayCircleOutlined,
  ReloadOutlined,
  RobotOutlined,
  SearchOutlined,
  SettingOutlined,
  ThunderboltOutlined,
  UserOutlined,
  VideoCameraOutlined,
  WarningOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs, { type Dayjs } from "dayjs";
import { ApiGet } from "@/lib/client-api";
import ManagePageHeader from "@/app/manage/components/page-header";
import TrendChart, { type SeriesPoint } from "./trend-chart";
import DonutChart, { type DonutSlice } from "./donut-chart";
import ClientChannelCard, { type ClientChannelItem } from "./client-channel-card";
import LatencyChart from "./latency-chart";
import LatencyHeatmap from "./latency-heatmap";
import styles from "./index.module.less";

type Overview = {
  day: string;
  pv: number;
  uv: number;
  err4: number;
  err5: number;
  p95Ms: number;
  dropped: number;
  provide?: { pv: number; err4: number; err5: number };
  client?: Record<string, number>;
  action?: Record<string, number>;
  hist?: Record<string, number>;
  series?: SeriesPoint[];
};

type TopItem = {
  key: string;
  count: number;
  title?: string;
  category?: string;
  poster?: string;
  year?: number;
};

type LogRow = {
  ts: string;
  method: string;
  path: string;
  status: number;
  latencyMs: number;
  clientType: string;
  internal?: string;
  ipPreview: string;
  uaFamily: string;
  resource?: string;
  action?: string;
};

const CLIENT_MAP: Record<string, { label: string; icon: React.ReactNode; color: string; desc?: string }> = {
  web: { label: "Web 网页端", icon: <DesktopOutlined />, color: "#fa8c16", desc: "桌面与手机浏览器 · 页面浏览与点播" },
  tvbox: { label: "TVBox 电视", icon: <DesktopOutlined />, color: "#1677ff", desc: "影视仓与电视盒子 · 订阅源与播放同步" },
  app: { label: "App 客户端", icon: <MobileOutlined />, color: "#52c41a", desc: "移动端原生应用 · 手机与鸿蒙客户端" },
  crawler: { label: "爬虫 Bot", icon: <RobotOutlined />, color: "#faad14", desc: "搜索引擎与自动化爬虫 · 索引抓取" },
  manage: { label: "管理后台", icon: <SettingOutlined />, color: "#722ed1", desc: "管理员后台系统操作 · 配置与调度" },
  unknown: { label: "其他来源", icon: <UserOutlined />, color: "#8c8c8c", desc: "未识别设备或未知客户端请求" },
};

const ACTION_MAP: Record<string, { label: string; icon: React.ReactNode; color: string }> = {
  play: { label: "影视点播", icon: <PlayCircleOutlined />, color: "#1677ff" },
  search: { label: "寻片搜索", icon: <SearchOutlined />, color: "#13c2c2" },
  browse: { label: "浏览探索", icon: <CompassOutlined />, color: "#52c41a" },
  provide: { label: "TVBox 同步", icon: <DatabaseOutlined />, color: "#fa8c16" },
  classify: { label: "分类筛选", icon: <CompassOutlined />, color: "#722ed1" },
  manage: { label: "管理后台", icon: <SettingOutlined />, color: "#eb2f96" },
  other: { label: "其他请求", icon: <ThunderboltOutlined />, color: "#8c8c8c" },
};

function formatClientPrefix(clientType: string) {
  switch (clientType) {
    case "tvbox":
      return "tvbox";
    case "app":
    case "ios":
    case "android":
    case "ohos":
      return "app";
    case "web":
      return "web";
    case "crawler":
      return "bot";
    case "manage":
      return "manage";
    default:
      return "client";
  }
}

function formatRoutePath(row: LogRow) {
  const path = row.path || "";
  if (row.resource) {
    if (path.includes("/filmPlayInfo")) {
      return `/api/filmPlayInfo?id=${row.resource}`;
    }
    if (path.includes("/searchFilm")) {
      return `/api/searchFilm?keyword=${encodeURIComponent(row.resource)}`;
    }
    if (path.startsWith("/api/provide/vod")) {
      return `/api/provide/vod?ac=${row.resource}`;
    }
  }
  return path;
}

function fmtNum(n?: number) {
  if (n === undefined || n === null || Number.isNaN(Number(n))) return "0";
  return Number(n).toLocaleString("zh-CN");
}

function statusTag(status: number) {
  if (status >= 500) return <Tag color="error">{status}</Tag>;
  if (status >= 400) return <Tag color="warning">{status}</Tag>;
  return <Tag color="success">{status}</Tag>;
}

function resolveActionInfo(row: LogRow): { tag: React.ReactNode; desc: string } {
  const path = row.path || "";
  const resource = row.resource || "";

  // 1. 点播
  if (path.includes("/filmPlayInfo")) {
    return {
      tag: <Tag color="blue" icon={<PlayCircleOutlined />}>影视点播</Tag>,
      desc: resource ? `播放影片 #${resource}` : "请求影片播放数据",
    };
  }

  // 2. 搜索
  if (path.includes("/searchFilm")) {
    return {
      tag: <Tag color="cyan" icon={<SearchOutlined />}>寻片搜索</Tag>,
      desc: resource ? `搜索关键词 "${resource}"` : "影片关键字检索",
    };
  }
  if (path.includes("/hotKeywords")) {
    return {
      tag: <Tag color="cyan" icon={<FireOutlined />}>热搜词榜</Tag>,
      desc: "获取实时热搜词榜单",
    };
  }

  // 3. TVBox 接口
  if (path.startsWith("/api/provide/vod")) {
    if (resource === "detail" || row.action === "play") {
      return {
        tag: <Tag color="blue" icon={<PlayCircleOutlined />}>TVBox 点播</Tag>,
        desc: "TVBox 播放详情与解析",
      };
    }
    if (resource === "list" || !resource) {
      return {
        tag: <Tag color="orange" icon={<DesktopOutlined />}>TVBox 列表</Tag>,
        desc: "TVBox 影片列表与分类同步",
      };
    }
    return {
      tag: <Tag color="cyan" icon={<SearchOutlined />}>TVBox 搜索</Tag>,
      desc: `TVBox 寻片搜索 (${resource})`,
    };
  }
  if (path.startsWith("/api/provide/config") || path.startsWith("/api/provide")) {
    return {
      tag: <Tag color="orange" icon={<DatabaseOutlined />}>TVBox 订阅</Tag>,
      desc: "TVBox 订阅源配置加载",
    };
  }

  // 4. 浏览探索
  if (path === "/api/index") {
    return {
      tag: <Tag color="green" icon={<CompassOutlined />}>首页浏览</Tag>,
      desc: "加载首页推荐与排片",
    };
  }
  if (path.includes("/dailyUpdates")) {
    return {
      tag: <Tag color="green" icon={<CompassOutlined />}>每日更新</Tag>,
      desc: "查看每日最新更新影片",
    };
  }
  if (path.includes("/navCategory")) {
    return {
      tag: <Tag color="green" icon={<CompassOutlined />}>分类导航</Tag>,
      desc: "获取顶部大分类导航",
    };
  }
  if (path.includes("/filmClassify")) {
    return {
      tag: <Tag color="purple" icon={<CompassOutlined />}>分类筛选</Tag>,
      desc: "多维度分类标签筛选",
    };
  }
  if (path.includes("/filmRelate")) {
    return {
      tag: <Tag color="green" icon={<CompassOutlined />}>推荐相关</Tag>,
      desc: "获取相关联影片推荐",
    };
  }

  // 5. 账户认证
  if (path.includes("/login")) {
    return {
      tag: <Tag color="volcano" icon={<UserOutlined />}>管理员登录</Tag>,
      desc: "管理后台登录身份认证",
    };
  }
  if (path.includes("/logout")) {
    return {
      tag: <Tag color="default" icon={<UserOutlined />}>退出登录</Tag>,
      desc: "注销管理会话",
    };
  }

  // 6. 管理后台
  if (path.startsWith("/api/manage/")) {
    let subDesc = "管理后台系统请求";
    if (path.includes("/spider") || path.includes("/collect")) subDesc = "采集源与爬虫调度管理";
    else if (path.includes("/film")) subDesc = "影片库存与类目管理";
    else if (path.includes("/config")) subDesc = "系统参数与通知配置";
    else if (path.includes("/cron")) subDesc = "定时任务管理与触发";
    else if (path.includes("/access")) subDesc = "访问大盘与数据分析";
    else if (path.includes("/user")) subDesc = "管理员账号与权限管理";
    else if (path.includes("/mapping")) subDesc = "分类映射规则管理";
    else if (path.includes("/banner")) subDesc = "轮播海报管理";
    return {
      tag: <Tag color="purple" icon={<SettingOutlined />}>管理后台</Tag>,
      desc: subDesc,
    };
  }

  if (path.includes("/stat/view")) {
    return {
      tag: <Tag color="geekblue" icon={<EyeOutlined />}>页面曝光</Tag>,
      desc: resource ? `页面曝光 [${resource}]` : "前端页面访问统计埋点",
    };
  }

  if (path.includes("/config/basic")) {
    return {
      tag: <Tag color="default" icon={<SettingOutlined />}>站点配置</Tag>,
      desc: "获取站点基础配置信息",
    };
  }

  return {
    tag: <Tag color="default">通用接口</Tag>,
    desc: path || "接口调用",
  };
}

function disabledAccessDay(d: Dayjs) {
  return d.isAfter(dayjs(), "day") || d.isBefore(dayjs().subtract(13, "day"), "day");
}

export default function AccessPageView() {
  const [day, setDay] = useState<Dayjs>(dayjs());
  const [overview, setOverview] = useState<Overview | null>(null);
  const [searchTops, setSearchTops] = useState<TopItem[]>([]);
  const [playTops, setPlayTops] = useState<TopItem[]>([]);
  const [logs, setLogs] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [logLoading, setLogLoading] = useState(false);
  const [error, setError] = useState("");
  const [logError, setLogError] = useState("");
  const [source, setSource] = useState<string>("recent");
  const [status, setStatus] = useState<string>("");
  const [client, setClient] = useState<string>("");
  const [keyword, setKeyword] = useState("");
  const [appliedQ, setAppliedQ] = useState("");
  const [logPage, setLogPage] = useState({ current: 1, pageSize: 20 });
  const [latencyMode, setLatencyMode] = useState<"heat" | "bar">("heat");
  const [refreshInterval, setRefreshInterval] = useState<number>(15);
  const [lastRefreshedAt, setLastRefreshedAt] = useState<string>("");
  const [nowMs, setNowMs] = useState(() => Date.now());
  const overviewGen = useRef(0);
  const logsGen = useRef(0);
  const overviewInFlight = useRef(false);
  const logsInFlight = useRef(false);

  const dayParam = day.format("YYYY-MM-DD");
  const isToday = day.isSame(dayjs(nowMs), "day");

  useEffect(() => {
    const ms = Math.max(dayjs(nowMs).endOf("day").diff(dayjs(nowMs)) + 50, 50);
    const timer = window.setTimeout(() => setNowMs(Date.now()), ms);
    return () => {
      window.clearTimeout(timer);
    };
  }, [nowMs]);

  const loadOverview = useCallback(
    async (silent = false) => {
      if (silent && overviewInFlight.current) return;
      overviewInFlight.current = true;
      const gen = ++overviewGen.current;
      if (!silent) {
        setLoading(true);
        setError("");
      }
      try {
        const [ov, search, play] = await Promise.all([
          ApiGet<Overview>("/manage/access/overview", { day: dayParam }),
          ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "search", limit: 10 }),
          ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "play", limit: 10 }),
        ]);
        if (gen !== overviewGen.current) return;
        if (ov.code !== 0) {
          if (!silent) {
            setError(ov.msg || "访问分析暂不可用");
            setOverview(null);
          }
          return;
        }
        setOverview(ov.data);
        setSearchTops(search.data?.items || []);
        setPlayTops(play.data?.items || []);
        setLastRefreshedAt(dayjs().format("HH:mm:ss"));
        setError("");
      } catch {
        if (gen !== overviewGen.current) return;
        if (!silent) {
          setError("访问分析暂不可用");
          setOverview(null);
        }
      } finally {
        if (gen === overviewGen.current) {
          overviewInFlight.current = false;
          if (!silent) {
            setLoading(false);
          }
        }
      }
    },
    [dayParam],
  );

  const loadLogs = useCallback(
    async (
      silent = false,
      overrideParams?: { day?: string; source?: string; status?: string; client?: string; q?: string },
    ) => {
      if (silent && logsInFlight.current) return;
      logsInFlight.current = true;
      const gen = ++logsGen.current;
      if (!silent) {
        setLogLoading(true);
        setLogError("");
      }
      try {
        const queryDay = overrideParams?.day ?? dayParam;
        const querySource = overrideParams?.source ?? source;
        const queryStatus = overrideParams?.status !== undefined ? overrideParams.status : status;
        const queryClient = overrideParams?.client !== undefined ? overrideParams.client : client;
        const queryQ = overrideParams?.q !== undefined ? overrideParams.q : appliedQ.trim();

        const resp = await ApiGet<{ list: LogRow[] }>("/manage/access/logs", {
          day: queryDay,
          source: querySource,
          status: queryStatus || undefined,
          client: queryClient || undefined,
          q: queryQ || undefined,
        });
        if (gen !== logsGen.current) return;
        if (resp.code === 0) {
          setLogs(resp.data?.list || []);
          setLogError("");
        } else if (!silent) {
          setLogError(resp.msg || "访问日志暂不可用");
          setLogs([]);
        }
      } catch {
        if (gen !== logsGen.current) return;
        if (!silent) {
          setLogError("访问日志暂不可用");
          setLogs([]);
        }
      } finally {
        if (gen === logsGen.current) {
          logsInFlight.current = false;
          if (!silent) {
            setLogLoading(false);
          }
        }
      }
    },
    [dayParam, source, status, client, appliedQ],
  );

  useEffect(() => {
    void loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    setLogPage((p) => ({ ...p, current: 1 }));
    void loadLogs();
  }, [loadLogs]);

  useEffect(() => {
    if (!isToday || refreshInterval <= 0) return;

    const refresh = () => {
      if (document.visibilityState !== "visible") return;
      void loadOverview(true);
      void loadLogs(true);
    };

    const timer = window.setInterval(refresh, refreshInterval * 1000);
    document.addEventListener("visibilitychange", refresh);
    return () => {
      window.clearInterval(timer);
      document.removeEventListener("visibilitychange", refresh);
    };
  }, [isToday, refreshInterval, loadOverview, loadLogs]);

  const handleResetLogs = () => {
    const hasFilterChange =
      appliedQ !== "" ||
      status !== "" ||
      client !== "" ||
      source !== "recent";

    setKeyword("");
    setAppliedQ("");
    setStatus("");
    setClient("");
    setSource("recent");
    setLogPage((p) => ({ ...p, current: 1 }));

    if (!hasFilterChange) {
      void loadLogs(false, { day: dayParam, source: "recent", status: "", client: "", q: "" });
    }
  };

  // 终端设备渠道数据（直观归总为 Web、TVBox、App、爬虫等）
  const clientChannelData: ClientChannelItem[] = useMemo(() => {
    const raw = overview?.client || {};
    const data: Record<string, number> = {
      web: raw.web || 0,
      tvbox: raw.tvbox || 0,
      app: (raw.app || 0) + (raw.ohos || 0) + (raw.android || 0) + (raw.ios || 0),
      crawler: raw.crawler || 0,
      manage: raw.manage || 0,
    };
    const primaryOrder = ["web", "tvbox", "app"];
    const allOrder = ["web", "tvbox", "app", "crawler", "manage"];
    const total = allOrder.reduce((s, k) => s + (data[k] || 0), 0);
    return allOrder
      .filter((k) => primaryOrder.includes(k) || (data[k] || 0) > 0)
      .map((k) => {
        const count = data[k] || 0;
        const info = CLIENT_MAP[k] || CLIENT_MAP.unknown;
        return {
          key: k,
          label: info.label,
          icon: info.icon,
          desc: info.desc,
          count,
          pct: total > 0 && count > 0 ? Math.round((count / total) * 100) : 0,
          color: info.color,
        };
      });
  }, [overview]);

  // 业务行为分布饼图数据
  const actionDonutData: DonutSlice[] = useMemo(() => {
    const data = overview?.action || {};
    const primaryOrder = ["play", "search", "browse", "provide", "classify"];
    const allOrder = ["play", "search", "browse", "provide", "classify", "manage", "other"];
    const total = allOrder.reduce((s, k) => s + (data[k] || 0), 0);
    return allOrder
      .filter((k) => primaryOrder.includes(k) || (data[k] || 0) > 0)
      .map((k) => {
        const count = data[k] || 0;
        const info = ACTION_MAP[k] || ACTION_MAP.other;
        return {
          key: k,
          label: info.label,
          icon: info.icon,
          count,
          pct: total > 0 && count > 0 ? Math.round((count / total) * 100) : 0,
          color: info.color,
        };
      });
  }, [overview]);

  const clientTotal = useMemo(() => {
    return clientChannelData.reduce((s, item) => s + item.count, 0);
  }, [clientChannelData]);

  const actionTotal = useMemo(() => {
    return actionDonutData.reduce((s, item) => s + item.count, 0);
  }, [actionDonutData]);

  // 今日核心指标计算
  const totalPlayCount = (overview?.action?.play || 0) + (overview?.provide?.pv || 0);
  const totalErrors = (overview?.err4 || 0) + (overview?.err5 || 0) + (overview?.provide?.err4 || 0) + (overview?.provide?.err5 || 0);
  const maxPlayTopCount = Math.max(1, ...(playTops.map((p) => p.count) || [1]));
  const maxSearchTopCount = Math.max(1, ...(searchTops.map((s) => s.count) || [1]));

  const columns: ColumnsType<LogRow> = [
    {
      title: "时间",
      dataIndex: "ts",
      width: 85,
      render: (v: string) => {
        const t = dayjs(v);
        return (
          <Tooltip title={t.format("YYYY-MM-DD HH:mm:ss")}>
            <span style={{ fontVariantNumeric: "tabular-nums" }}>{t.format("HH:mm:ss")}</span>
          </Tooltip>
        );
      },
    },
    {
      title: "业务场景",
      key: "actionTag",
      width: 115,
      render: (_, r) => resolveActionInfo(r).tag,
    },
    {
      title: "请求操作与路径",
      key: "target",
      ellipsis: true,
      render: (_, r) => {
        const route = formatRoutePath(r);
        const { desc } = resolveActionInfo(r);
        const prefix = formatClientPrefix(r.clientType);
        return (
          <div className={styles.logTargetCell}>
            <div className={styles.logDescLine} title={desc}>
              {desc}
            </div>
            <div className={styles.logRouteLine}>
              <span className={styles.methodTag}>{r.method || "GET"}</span>
              <span className={`${styles.clientPrefix} ${styles[`prefix_${r.clientType}`] || ""}`}>
                {prefix}
              </span>
              <span className={styles.logPath} title={r.path}>
                {route}
              </span>
            </div>
          </div>
        );
      },
    },
    {
      title: "终端设备",
      dataIndex: "clientType",
      width: 115,
      render: (v: string) => {
        const normalizedKey = v === "ios" || v === "android" || v === "ohos" ? "app" : v;
        const item = CLIENT_MAP[normalizedKey] || CLIENT_MAP.unknown;
        return (
          <Space size={6} align="center">
            {item.icon}
            <span>{item.label}</span>
          </Space>
        );
      },
    },
    {
      title: "耗时",
      dataIndex: "latencyMs",
      width: 80,
      render: (v: number) => {
        let cls = styles.latencyNormal;
        if (v <= 50) cls = styles.latencyFast;
        if (v >= 500) cls = styles.latencySlow;
        return <span className={cls}>{v}ms</span>;
      },
    },
    {
      title: "状态",
      dataIndex: "status",
      width: 70,
      render: (v: number) => statusTag(v),
    },
    {
      title: "客户端 IP",
      dataIndex: "ipPreview",
      width: 120,
      render: (v: string) => <span className={v === "local" ? styles.ipLocal : undefined}>{v}</span>,
    },
  ];

  return (
    <div className={styles.pageStack}>
      <ManagePageHeader
        title="访问分析"
        actions={
          <Space wrap size={[8, 8]}>
            {lastRefreshedAt && isToday && (
              <Typography.Text type="secondary" style={{ fontSize: 13, marginRight: 4 }}>
                最后更新 {lastRefreshedAt}
              </Typography.Text>
            )}
            {isToday ? (
              <Select
                value={refreshInterval}
                onChange={setRefreshInterval}
                options={[
                  { label: "关闭自动刷新", value: 0 },
                  { label: "5秒 自动刷新", value: 5 },
                  { label: "10秒 自动刷新", value: 10 },
                  { label: "15秒 自动刷新", value: 15 },
                ]}
                style={{ width: 160 }}
              />
            ) : null}
            <DatePicker
              value={day}
              allowClear={false}
              disabledDate={disabledAccessDay}
              onChange={(v) => {
                if (!v) return;
                setDay(v);
                setOverview(null);
                setSearchTops([]);
                setPlayTops([]);
                setLogs([]);
                setLoading(true);
                setLogLoading(true);
                setError("");
                setLogError("");
                setLastRefreshedAt("");
              }}
            />
            <Button
              icon={<ReloadOutlined />}
              loading={loading || logLoading}
              onClick={() => {
                void loadOverview(false);
                void loadLogs(false);
              }}
            >
              刷新
            </Button>
          </Space>
        }
      />

      {error ? <Alert type="error" showIcon title={error} /> : null}
      {!error && logError ? <Alert type="error" showIcon title={logError} /> : null}
      {overview && overview.dropped > 0 ? (
        <Alert
          type="warning"
          showIcon
          title={`分析队列丢弃 ${overview.dropped} 条事件`}
        />
      ) : null}

      {/* 顶部大盘概览 */}
      <Card
        className={styles.panelCard}
        title="大盘概览"
        loading={loading}
        styles={{ body: { padding: 16 } }}
      >
        <div className={styles.statsGrid}>
          <div className={styles.statCard}>
            <div className={`${styles.statIconWrap} ${styles.iconPlay}`}>
              <PlayCircleOutlined />
            </div>
            <div className={styles.statBody}>
              <div className={styles.statValue}>{fmtNum(totalPlayCount)}</div>
              <div className={styles.statLabel}>点播量</div>
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={`${styles.statIconWrap} ${styles.iconUv}`}>
              <UserOutlined />
            </div>
            <div className={styles.statBody}>
              <div className={styles.statValue}>{fmtNum(overview?.uv)}</div>
              <div className={styles.statLabel}>独立访客 (UV)</div>
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={`${styles.statIconWrap} ${styles.iconPv}`}>
              <EyeOutlined />
            </div>
            <div className={styles.statBody}>
              <div className={styles.statValue}>{fmtNum(overview?.pv)}</div>
              <div className={styles.statLabel}>浏览量 (PV)</div>
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={`${styles.statIconWrap} ${totalErrors > 0 ? styles.iconErr : styles.iconHealth}`}>
              {totalErrors > 0 ? <WarningOutlined /> : <ClockCircleOutlined />}
            </div>
            <div className={styles.statBody}>
              <div className={styles.statValue}>
                {overview ? `${overview.p95Ms}ms` : "—"}
              </div>
              <div className={styles.statLabel}>
                {totalErrors > 0 ? `P95 (异常 ${totalErrors})` : "P95 响应"}
              </div>
            </div>
          </div>
        </div>

        {/* TVBox 快捷横条 */}
        <div className={styles.provideSummaryBar}>
          <span className={styles.provideBadge}>
            <DesktopOutlined /> TVBox
          </span>
          <span>
            点播拉流 <b className={styles.provideValue}>{fmtNum(overview?.provide?.pv)}</b>
          </span>
          <span>
            异常 <b className={styles.provideValue}>{fmtNum((overview?.provide?.err4 || 0) + (overview?.provide?.err5 || 0))}</b>
          </span>
        </div>
      </Card>

      {/* 第一行图表（左宽右窄）：24 小时走势 (宽) + 终端渠道分布 (窄) */}
      <div className={styles.gridWideLeft}>
        <Card
          className={styles.panelCard}
          title="24 小时走势"
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          <TrendChart series={overview?.series} />
        </Card>

        <Card
          className={styles.panelCard}
          title={
            <Space size={6}>
              <PieChartOutlined style={{ color: "#fa8c16" }} />
              <span>终端渠道分布</span>
              <Tooltip title="按访问来源设备统计请求总量与占比（Web网页端、TVBox电视、App移动端等）">
                <InfoCircleOutlined style={{ color: "var(--ant-color-text-tertiary)", cursor: "pointer", fontSize: 13 }} />
              </Tooltip>
            </Space>
          }
          extra={
            clientTotal > 0 ? (
              <span className={styles.hint}>
                总请求量 <b style={{ color: "var(--ant-color-text)", fontWeight: 600 }}>{fmtNum(clientTotal)}</b> 次
              </span>
            ) : null
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          <ClientChannelCard data={clientChannelData} />
        </Card>
      </div>

      {/* 第二行图表（左窄右宽，交错呼应）：业务场景画像 (窄) + 服务响应耗时分布 (宽) */}
      <div className={styles.gridWideRight}>
        <Card
          className={styles.panelCard}
          title={
            <Space size={6}>
              <CompassOutlined style={{ color: "#52c41a" }} />
              <span>业务场景画像</span>
              <Tooltip title="按业务功能分类统计全站请求操作量与占比（影视点播、寻片搜索、分类浏览、TVBox同步等）">
                <InfoCircleOutlined style={{ color: "var(--ant-color-text-tertiary)", cursor: "pointer", fontSize: 13 }} />
              </Tooltip>
            </Space>
          }
          extra={
            actionTotal > 0 ? (
              <span className={styles.hint}>
                操作总量 <b style={{ color: "var(--ant-color-text)", fontWeight: 600 }}>{fmtNum(actionTotal)}</b> 次
              </span>
            ) : null
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          <DonutChart data={actionDonutData} title="操作总量" unit="次" />
        </Card>

        <Card
          className={styles.panelCard}
          title={
            <Space>
              <ThunderboltOutlined style={{ color: "#1677ff" }} />
              <span>服务响应耗时分布</span>
            </Space>
          }
          extra={
            <Radio.Group
              size="small"
              value={latencyMode}
              onChange={(e) => setLatencyMode(e.target.value)}
              optionType="button"
              buttonStyle="solid"
              options={[
                { label: "时序热力", value: "heat" },
                { label: "柱状分布", value: "bar" },
              ]}
            />
          }
          loading={loading || logLoading}
          styles={{ body: { padding: 16 } }}
        >
          {latencyMode === "heat" ? (
            <LatencyHeatmap logs={logs} />
          ) : (
            <LatencyChart hist={overview?.hist} />
          )}
        </Card>
      </div>

      {/* 第三行排行：热门点播 TOP 10 + 热门搜索 TOP 10 */}
      <div className={styles.chartsGridEqual}>
        {/* 热门点播 TOP 10 */}
        <Card
          className={styles.panelCard}
          title={
            <Space>
              <VideoCameraOutlined style={{ color: "#1677ff" }} />
              <span>热门点播 TOP 10</span>
            </Space>
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          {playTops.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无数据" />
          ) : (
            <div className={styles.hotPlayList}>
              {playTops.map((item, idx) => {
                const rankClass = idx === 0 ? styles.rank1 : idx === 1 ? styles.rank2 : idx === 2 ? styles.rank3 : "";
                const rawFilmId = item.key.replace(/^id\s+/, "");
                const displayName = item.title || `影片 #${rawFilmId}`;
                const barWidthPct = Math.max(4, Math.round((item.count / maxPlayTopCount) * 100));

                return (
                  <div className={styles.hotPlayItem} key={`${item.key}-${idx}`}>
                    <div className={`${styles.rankBadge} ${rankClass}`}>{idx + 1}</div>
                    {item.poster ? (
                      <img
                        src={item.poster}
                        alt={displayName}
                        className={styles.posterThumb}
                        loading="lazy"
                      />
                    ) : (
                      <div className={styles.posterPlaceholder}>
                        <VideoCameraOutlined />
                      </div>
                    )}
                    <div className={styles.filmInfo}>
                      <div className={styles.filmTitleRow}>
                        <Link
                          href={`/play?id=${rawFilmId}`}
                          target="_blank"
                          className={styles.filmTitle}
                        >
                          {displayName}
                        </Link>
                        {item.category && (
                          <Tag color="blue" className={styles.filmCategoryTag}>
                            {item.category}
                          </Tag>
                        )}
                        {item.year ? (
                          <span className={styles.hint}>({item.year})</span>
                        ) : null}
                      </div>
                      <div className={styles.filmCountBarWrap}>
                        <div className={styles.filmCountBar}>
                          <div
                            className={styles.filmCountBarFill}
                            style={{ width: `${barWidthPct}%` }}
                          />
                        </div>
                        <span className={styles.filmCount}>{fmtNum(item.count)} 次</span>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Card>

        {/* 热门搜索 TOP 10 */}
        <Card
          className={styles.panelCard}
          title={
            <Space>
              <FireOutlined style={{ color: "#fa541c" }} />
              <span>热门搜索 TOP 10</span>
            </Space>
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          {searchTops.length === 0 ? (
            <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="暂无数据" />
          ) : (
            <div className={styles.hotPlayList}>
              {searchTops.map((item, idx) => {
                const rankClass = idx === 0 ? styles.rank1 : idx === 1 ? styles.rank2 : idx === 2 ? styles.rank3 : "";
                const barWidthPct = Math.max(4, Math.round((item.count / maxSearchTopCount) * 100));

                return (
                  <div className={styles.hotPlayItem} key={`${item.key}-${idx}`}>
                    <div className={`${styles.rankBadge} ${rankClass}`}>{idx + 1}</div>
                    <div className={styles.filmInfo}>
                      <div className={styles.filmTitleRow}>
                        <Link
                          href={`/search?search=${encodeURIComponent(item.key)}`}
                          target="_blank"
                          className={styles.filmTitle}
                        >
                          {item.key}
                        </Link>
                      </div>
                      <div className={styles.filmCountBarWrap}>
                        <div className={styles.filmCountBar}>
                          <div
                            className={styles.filmCountBarFill}
                            style={{
                              width: `${barWidthPct}%`,
                              background: "linear-gradient(90deg, #fa541c, #ff7a45)",
                            }}
                          />
                        </div>
                        <span className={styles.filmCount} style={{ color: "#fa541c" }}>
                          {fmtNum(item.count)} 次
                        </span>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </Card>
      </div>

      {/* 访问日志 */}
      <Card
        className={styles.panelCard}
        title="访问日志"
        styles={{ body: { padding: 16 } }}
      >
        <div className={styles.toolbar}>
          <div className={styles.toolbarLeft}>
            <Segmented
              value={source}
              onChange={(v) => setSource(String(v))}
              options={[
                { label: "全部", value: "recent" },
                { label: "慢请求", value: "slow" },
                { label: "异常", value: "error" },
              ]}
            />
          </div>

          <div className={styles.toolbarRight}>
            <Select
              value={status || "all"}
              onChange={(v) => setStatus(v === "all" ? "" : v)}
              options={[
                { value: "all", label: "全部状态" },
                { value: "2xx", label: "2xx" },
                { value: "4xx", label: "4xx" },
                { value: "5xx", label: "5xx" },
              ]}
              style={{ width: 110 }}
            />

            <Select
              value={client || "all"}
              onChange={(v) => setClient(v === "all" ? "" : v)}
              options={[
                { value: "all", label: "全部终端" },
                ...Object.keys(CLIENT_MAP).map((k) => ({
                  value: k,
                  label: CLIENT_MAP[k].label,
                })),
              ]}
              style={{ width: 120 }}
            />

            <Input.Search
              placeholder="按路径/关键词过滤"
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              onSearch={(v) => setAppliedQ(v)}
              style={{ width: 220 }}
              allowClear
              enterButton="查询"
            />

            <Button
              icon={<ReloadOutlined />}
              onClick={handleResetLogs}
            >
              重置
            </Button>
          </div>
        </div>

        <Table
          rowKey={(r, i) => `${r.ts}-${r.path}-${i}`}
          columns={columns}
          dataSource={logs}
          loading={logLoading}
          pagination={{
            current: logPage.current,
            pageSize: logPage.pageSize,
            total: logs.length,
            showSizeChanger: true,
            pageSizeOptions: [10, 20, 50],
            showTotal: (total) => `共 ${total} 条`,
            onChange: (current, pageSize) => setLogPage({ current, pageSize }),
          }}
          size="middle"
          locale={{
            emptyText: (
              <Empty
                image={Empty.PRESENTED_IMAGE_SIMPLE}
                description="暂无访问记录"
              />
            ),
          }}
          scroll={{ x: 960 }}
        />
      </Card>
    </div>
  );
}
