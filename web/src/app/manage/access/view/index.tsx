"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
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
} from "antd";
import {
  ClockCircleOutlined,
  CompassOutlined,
  DatabaseOutlined,
  DesktopOutlined,
  EyeOutlined,
  FireOutlined,
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
import LatencyChart from "./latency-chart";
import ScatterChart from "./scatter-chart";
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

const CLIENT_MAP: Record<string, { label: string; icon: React.ReactNode; color: string }> = {
  web: { label: "Web 网页", icon: <DesktopOutlined />, color: "#fa8c16" },
  app: { label: "App 客户端", icon: <MobileOutlined />, color: "#52c41a" },
  tvbox: { label: "TVBox 电视", icon: <DesktopOutlined />, color: "#1677ff" },
  crawler: { label: "爬虫 Bot", icon: <RobotOutlined />, color: "#faad14" },
  manage: { label: "管理后台", icon: <SettingOutlined />, color: "#722ed1" },
  unknown: { label: "其他", icon: <UserOutlined />, color: "#8c8c8c" },
};

const ACTION_MAP: Record<string, { label: string; icon: React.ReactNode; color: string }> = {
  play: { label: "影视点播", icon: <PlayCircleOutlined />, color: "#1677ff" },
  search: { label: "寻片检索", icon: <SearchOutlined />, color: "#13c2c2" },
  browse: { label: "漫游发现", icon: <CompassOutlined />, color: "#52c41a" },
  provide: { label: "设备同步", icon: <DatabaseOutlined />, color: "#fa8c16" },
  classify: { label: "分类筛选", icon: <CompassOutlined />, color: "#722ed1" },
  manage: { label: "后台管理", icon: <SettingOutlined />, color: "#eb2f96" },
  other: { label: "其他请求", icon: <ThunderboltOutlined />, color: "#8c8c8c" },
};

function formatClientPrefix(clientType: string) {
  switch (clientType) {
    case "tvbox":
      return "tvbox:";
    case "app":
    case "ios":
    case "android":
    case "ohos":
      return "app:";
    case "web":
      return "web:";
    case "crawler":
      return "bot:";
    case "manage":
      return "manage:";
    default:
      return "client:";
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

function resolveActionTag(row: LogRow) {
  const path = row.path || "";
  if (path.includes("/filmPlayInfo") || row.resource === "detail") {
    return <Tag color="blue" icon={<PlayCircleOutlined />}>点播</Tag>;
  }
  if (path.includes("/searchFilm") || path.includes("/filmClassifySearch")) {
    return <Tag color="cyan" icon={<SearchOutlined />}>搜索</Tag>;
  }
  if (path.includes("/index") || path.includes("/dailyUpdates") || path.includes("/navCategory")) {
    return <Tag color="green" icon={<CompassOutlined />}>漫游</Tag>;
  }
  if (path.startsWith("/api/provide/")) {
    if (row.resource === "list") return <Tag color="green" icon={<CompassOutlined />}>TVBox 漫游</Tag>;
    if (row.resource === "config") return <Tag color="orange" icon={<DatabaseOutlined />}>TVBox 配置</Tag>;
    return <Tag color="blue" icon={<PlayCircleOutlined />}>TVBox 点播</Tag>;
  }
  if (path.startsWith("/api/manage/")) {
    return <Tag color="default" icon={<SettingOutlined />}>后台</Tag>;
  }
  return <Tag color="default">API</Tag>;
}

function resolveTargetDescription(row: LogRow) {
  const path = row.path || "";
  if (row.resource) {
    if (path.includes("/filmPlayInfo")) {
      return `播放 #${row.resource}`;
    }
    if (path.includes("/searchFilm")) {
      return `搜索 "${row.resource}"`;
    }
    if (path.startsWith("/api/provide/vod")) {
      if (row.resource === "detail") return "TVBox 播放详情";
      if (row.resource === "list") return "TVBox 影片列表";
      return `TVBox (${row.resource})`;
    }
    if (path.startsWith("/api/provide/config")) {
      return "TVBox 订阅配置";
    }
  }
  if (path === "/api/index") return "首页数据";
  if (path === "/api/dailyUpdates" || path === "/api/index/dailyUpdates") return "每日更新";
  if (path === "/api/navCategory") return "分类导航";
  return path;
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
  const [latencyMode, setLatencyMode] = useState<"scatter" | "bar">("scatter");

  const dayParam = day.format("YYYY-MM-DD");

  const loadOverview = useCallback(async () => {
    setLoading(true);
    setError("");
    try {
      const [ov, search, play] = await Promise.all([
        ApiGet<Overview>("/manage/access/overview", { day: dayParam }),
        ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "search", limit: 10 }),
        ApiGet<{ items: TopItem[] }>("/manage/access/tops", { day: dayParam, kind: "play", limit: 10 }),
      ]);
      if (ov.code !== 0) {
        setError(ov.msg || "访问分析暂不可用");
        setOverview(null);
        return;
      }
      setOverview(ov.data);
      setSearchTops(search.data?.items || []);
      setPlayTops(play.data?.items || []);
    } catch {
      setError("访问分析暂不可用");
      setOverview(null);
    } finally {
      setLoading(false);
    }
  }, [dayParam]);

  const loadLogs = useCallback(async () => {
    setLogPage((p) => ({ ...p, current: 1 }));
    setLogLoading(true);
    setLogError("");
    try {
      const resp = await ApiGet<{ list: LogRow[] }>("/manage/access/logs", {
        source,
        status: status || undefined,
        client: client || undefined,
        q: appliedQ.trim() || undefined,
      });
      if (resp.code === 0) {
        setLogs(resp.data?.list || []);
      } else {
        setLogError(resp.msg || "访问日志暂不可用");
        setLogs([]);
      }
    } catch {
      setLogError("访问日志暂不可用");
      setLogs([]);
    } finally {
      setLogLoading(false);
    }
  }, [source, status, client, appliedQ]);

  useEffect(() => {
    void loadOverview();
  }, [loadOverview]);

  useEffect(() => {
    void loadLogs();
  }, [loadLogs]);

  // 终端设备分布饼图数据（直观归总为 Web、App、TVBox）
  const clientDonutData: DonutSlice[] = useMemo(() => {
    const raw = overview?.client || {};
    const data: Record<string, number> = {
      web: raw.web || 0,
      app: (raw.app || 0) + (raw.ohos || 0) + (raw.android || 0) + (raw.ios || 0),
      tvbox: raw.tvbox || 0,
      crawler: raw.crawler || 0,
    };
    const primaryOrder = ["web", "app", "tvbox"];
    const allOrder = ["web", "app", "tvbox", "crawler"];
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
    const allOrder = ["play", "search", "browse", "provide", "classify", "other"];
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

  // 今日核心指标计算
  const totalPlayCount = (overview?.action?.play || 0) + (overview?.provide?.pv || 0);
  const totalErrors = (overview?.err4 || 0) + (overview?.err5 || 0) + (overview?.provide?.err4 || 0) + (overview?.provide?.err5 || 0);
  const maxPlayTopCount = Math.max(1, ...(playTops.map((p) => p.count) || [1]));
  const maxSearchTopCount = Math.max(1, ...(searchTops.map((s) => s.count) || [1]));

  const columns: ColumnsType<LogRow> = [
    {
      title: "时间",
      dataIndex: "ts",
      width: 90,
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
      title: "场景",
      key: "actionTag",
      width: 100,
      render: (_, r) => resolveActionTag(r),
    },
    {
      title: "路由",
      key: "target",
      ellipsis: true,
      render: (_, r) => {
        const route = formatRoutePath(r);
        const desc = resolveTargetDescription(r);
        const prefix = formatClientPrefix(r.clientType);
        return (
          <div className={styles.logTargetCell}>
            <div className={styles.logRouteLine}>
              <span className={`${styles.clientPrefix} ${styles[`prefix_${r.clientType}`] || ""}`}>
                {prefix}
              </span>
              <span className={styles.logPath} title={r.path}>
                {route}
              </span>
            </div>
            <div className={styles.logDescLine}>{desc}</div>
          </div>
        );
      },
    },
    {
      title: "终端",
      dataIndex: "clientType",
      width: 110,
      render: (v: string) => {
        const normalizedKey = (v === "ios" || v === "android" || v === "ohos") ? "app" : v;
        const item = CLIENT_MAP[normalizedKey] || CLIENT_MAP.unknown;
        return (
          <Space orientation="horizontal" size={4}>
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
      title: "IP",
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
          <Space>
            <DatePicker
              value={day}
              allowClear={false}
              disabledDate={disabledAccessDay}
              onChange={(v) => v && setDay(v)}
            />
            <Button
              icon={<ReloadOutlined />}
              onClick={() => {
                void loadOverview();
                void loadLogs();
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

      {/* 第一行图表（左宽右窄）：24 小时走势 (宽) + 终端设备分布 (窄) */}
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
            <Space>
              <PieChartOutlined style={{ color: "#fa8c16" }} />
              <span>终端设备分布</span>
            </Space>
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          <DonutChart data={clientDonutData} title="终端总计" />
        </Card>
      </div>

      {/* 第二行图表（左窄右宽，交错呼应）：行为场景分布 (窄) + 服务响应耗时分布 (宽) */}
      <div className={styles.gridWideRight}>
        <Card
          className={styles.panelCard}
          title={
            <Space>
              <CompassOutlined style={{ color: "#52c41a" }} />
              <span>行为场景分布</span>
            </Space>
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          <DonutChart data={actionDonutData} title="行为总计" />
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
                { label: "散点分布", value: "scatter" },
                { label: "柱状梯队", value: "bar" },
              ]}
            />
          }
          loading={loading}
          styles={{ body: { padding: 16 } }}
        >
          {latencyMode === "scatter" ? (
            <ScatterChart logs={logs} />
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
              onClick={() => {
                setKeyword("");
                setAppliedQ("");
                setStatus("");
                setClient("");
                setSource("recent");
              }}
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
