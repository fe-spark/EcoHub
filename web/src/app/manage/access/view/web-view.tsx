"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import Link from "next/link";
import { Card, Empty, Input, Space, Table, Tag, Typography } from "antd";
import {
  CompassOutlined,
  DesktopOutlined,
  EyeOutlined,
  FireOutlined,
  LineChartOutlined,
  PlayCircleOutlined,
  SearchOutlined,
  UserOutlined,
} from "@ant-design/icons";
import type { ColumnsType } from "antd/es/table";
import dayjs from "dayjs";
import { ApiGet } from "@/lib/client-api";
import TrendChart from "./trend-chart";
import DonutChart, { type DonutSlice } from "./donut-chart";
import BusinessRankings from "./business-rankings";
import type { Overview, TopItem, LogRow } from "./types";
import ResourceCell from "./resource-cell";
import styles from "./index.module.less";

const ACTION_MAP: Record<string, { label: string; icon: React.ReactNode; color: string }> = {
  play: { label: "影视点播", icon: <PlayCircleOutlined />, color: "var(--ant-color-primary, #fa8c16)" },
  search: { label: "寻片搜索", icon: <SearchOutlined />, color: "#fa541c" },
  browse: { label: "页面浏览", icon: <CompassOutlined />, color: "#52c41a" },
  classify: { label: "分类筛选", icon: <CompassOutlined />, color: "#722ed1" },
};

const BROWSER_MAP: Record<string, { label: string; color: string }> = {
  chrome: { label: "Chrome", color: "var(--ant-color-info, #1677ff)" },
  safari: { label: "Safari", color: "var(--ant-color-success, #52c41a)" },
  edge: { label: "Edge", color: "var(--ant-color-cyan, #13c2c2)" },
  firefox: { label: "Firefox", color: "var(--ant-color-error, #fa541c)" },
  other: { label: "其他浏览器", color: "var(--ant-color-text-tertiary, #8c8c8c)" },
};

const OS_MAP: Record<string, { label: string; color: string }> = {
  windows: { label: "Windows", color: "var(--ant-color-info, #1677ff)" },
  macos: { label: "macOS", color: "var(--ant-color-text, #8c8c8c)" },
  android: { label: "Android", color: "var(--ant-color-success, #52c41a)" },
  ios: { label: "iOS", color: "var(--ant-color-text, #000000)" },
  harmonyos: { label: "HarmonyOS", color: "var(--ant-color-info, #1677ff)" },
  linux: { label: "Linux", color: "var(--ant-color-warning, #fa8c16)" },
  other: { label: "其他系统", color: "var(--ant-color-text-tertiary, #8c8c8c)" },
};

// 页面值来自公开无鉴权埋点接口（/api/stat/view），只能作为站内相对路径渲染成链接，
// 拒绝协议相对外链（//）与任何 URL scheme（javascript:/data:/https: 等），否则回退纯文本。
function isSafeInternalHref(v: string): boolean {
  const s = String(v ?? "").trim();
  if (!s || s.startsWith("//")) return false;
  if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(s)) return false;
  return s.startsWith("/");
}


export default function WebAnalyticsView({ dayStr, refreshKey }: { dayStr: string; refreshKey?: number }) {
  const [overview, setOverview] = useState<Overview | null>(null);
  const [tops, setTops] = useState<TopItem[]>([]);
  const [playTops, setPlayTops] = useState<TopItem[]>([]);
  const [searchTops, setSearchTops] = useState<TopItem[]>([]);
  const [classifyTops, setClassifyTops] = useState<TopItem[]>([]);
  const [logs, setLogs] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [logSearch, setLogSearch] = useState("");
  const isFirstMount = useRef(true);
  const lastDayRef = useRef(dayStr);
  // 竞态防护：静默轮询在途即跳过（避免请求无界叠加），响应只接受最新一批（防止慢响应覆盖新筛选结果）
  const reqSeqRef = useRef(0);
  const inFlightRef = useRef(false);

  const fetchData = useCallback(async (silent = false) => {
    if (silent && inFlightRef.current) {
      return;
    }
    inFlightRef.current = true;
    const seq = ++reqSeqRef.current;
    if (!silent) {
      setLoading(true);
    }
    try {
      const [ovRes, topRes, playRes, searchRes, classifyRes, logRes] = await Promise.all([
        ApiGet<Overview>(`/manage/access/overview?day=${dayStr}&module=web`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=web&kind=path&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=web&kind=play&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=web&kind=search&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=web&kind=classify&limit=10`),
        ApiGet<{ list: LogRow[] }>(`/manage/access/logs?day=${dayStr}&module=web&limit=100`),
      ]);
      if (seq !== reqSeqRef.current) {
        return;
      }
      if (ovRes.code === 0 && ovRes.data) {
        setOverview(ovRes.data);
      }
      if (topRes.code === 0 && topRes.data) {
        setTops(topRes.data.items || []);
      }
      if (playRes.code === 0 && playRes.data) {
        setPlayTops(playRes.data.items || []);
      }
      if (searchRes.code === 0 && searchRes.data) {
        setSearchTops(searchRes.data.items || []);
      }
      if (classifyRes.code === 0 && classifyRes.data) {
        setClassifyTops(classifyRes.data.items || []);
      }
      if (logRes.code === 0 && logRes.data) {
        const list = (logRes.data.list || []).map((item, idx) => ({
          ...item,
          _key: `${item.ts}-${item.path}-${item.ipPreview}-${idx}`,
        }));
        setLogs(list);
      }
    } catch {
      // 忽略请求异常
    } finally {
      if (seq === reqSeqRef.current) {
        inFlightRef.current = false;
        if (!silent) {
          setLoading(false);
        }
      }
    }
  }, [dayStr]);

  useEffect(() => {
    // 首次加载或日期切换触发 loading 占位；定时轮询触发 silent 静默刷新，避免卸载图表导致漂移
    const dayChanged = lastDayRef.current !== dayStr;
    lastDayRef.current = dayStr;
    const isInitial = isFirstMount.current || dayChanged;
    isFirstMount.current = false;

    void fetchData(!isInitial);
  }, [fetchData, refreshKey, dayStr]);

  const filteredLogs = logs.filter((log) => {
    if (!logSearch) return true;
    const q = logSearch.toLowerCase();
    return (
      (log.path && log.path.toLowerCase().includes(q)) ||
      (log.page && log.page.toLowerCase().includes(q)) ||
      (log.pageTitle && log.pageTitle.toLowerCase().includes(q)) ||
      (log.resource && log.resource.toLowerCase().includes(q)) ||
      (log.resourceTitle && log.resourceTitle.toLowerCase().includes(q)) ||
      (log.deviceId && log.deviceId.toLowerCase().includes(q)) ||
      (log.ipPreview && log.ipPreview.toLowerCase().includes(q))
    );
  });

  const browserMap =
    overview?.browsers && Object.keys(overview.browsers).length > 0
      ? overview.browsers
      : {};
  const effectiveBrowsers: Record<string, number> = { ...browserMap };
  if (Object.keys(effectiveBrowsers).length === 0 && logs.length > 0) {
    for (const log of logs) {
      const b = log.uaFamily || "other";
      effectiveBrowsers[b] = (effectiveBrowsers[b] || 0) + 1;
    }
  }

  const browserSlices: DonutSlice[] = Object.entries(effectiveBrowsers)
    .filter(([_, count]) => count > 0)
    .map(([key, count]) => {
      const lower = key.toLowerCase();
      const info = BROWSER_MAP[lower] || {
        label: key.charAt(0).toUpperCase() + key.slice(1),
        color: "var(--ant-color-info, #1677ff)",
      };
      return {
        key: lower,
        name: info.label,
        value: count,
        color: info.color,
      };
    });

  const osSlices: DonutSlice[] = Object.entries(overview?.os || {})
    .filter(([_, count]) => count > 0)
    .map(([key, count]) => {
      const lower = key.toLowerCase();
      const info = OS_MAP[lower] || {
        label: key,
        color: "var(--ant-color-text-tertiary, #8c8c8c)",
      };
      return {
        key: lower,
        name: info.label,
        value: count,
        color: info.color,
      };
    });

  const columns: ColumnsType<LogRow> = [
    {
      title: "访问时间",
      dataIndex: "ts",
      key: "ts",
      width: 170,
      render: (ts: string) => dayjs(ts).format("YYYY-MM-DD HH:mm:ss"),
    },
    {
      title: "事件行为",
      dataIndex: "action",
      key: "action",
      width: 120,
      render: (action: string) => {
        const item = ACTION_MAP[action] || { label: action || "页面浏览", icon: <CompassOutlined />, color: "#52c41a" };
        return (
          <Tag color={item.color} icon={item.icon}>
            {item.label}
          </Tag>
        );
      },
    },
    {
      title: "访问页面 (URL)",
      dataIndex: "path",
      key: "path",
      render: (path: string, record) => {
        const target = record.page || path || "/";
        return (
          <Space orientation="vertical" size={2}>
            {isSafeInternalHref(target) ? (
              <Link href={target} target="_blank" className={styles.topPageLink}>
                {target}
              </Link>
            ) : (
              <span className={styles.topPageLink}>{target}</span>
            )}
            {record.pageTitle ? (
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                {record.pageTitle}
              </Typography.Text>
            ) : null}
          </Space>
        );
      },
    },
    {
      title: "关联资源",
      dataIndex: "resource",
      key: "resource",
      width: 240,
      render: (_, record) => <ResourceCell record={record} />,
    },
    {
      title: "设备 ID",
      dataIndex: "deviceId",
      key: "deviceId",
      width: 220,
      render: (did?: string) =>
        did ? (
          <Typography.Text
            copyable={{ text: did }}
            code
            style={{ fontSize: 12, whiteSpace: "nowrap" }}
          >
            {did}
          </Typography.Text>
        ) : (
          <span style={{ color: "#bbb" }}>-</span>
        ),
    },
    {
      title: "访客 IP",
      dataIndex: "ipPreview",
      key: "ipPreview",
      width: 140,
      render: (ip: string) => <Typography.Text code>{ip || "local"}</Typography.Text>,
    },
    {
      title: "客户端环境",
      dataIndex: "uaFamily",
      key: "uaFamily",
      width: 130,
      render: (ua: string) => {
        const info = BROWSER_MAP[(ua || "").toLowerCase()];
        return <Tag color={info ? "blue" : undefined}>{info ? info.label : ua || "Web"}</Tag>;
      },
    },
  ];

  return (
    <div className={styles.subModuleWrapper}>
      {/* 板块一：流量趋势 */}
      <div className={styles.sectionBlock}>
        <div className={styles.sectionHeader}>
          <span className={styles.sectionTitle}>
            <LineChartOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
            流量趋势
          </span>
        </div>

        {/* 核心指标 4 列横排卡片 */}
        <div className={styles.statsGrid}>
          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>今日浏览量 (PV)</span>
              <div className={styles.statValue}>{overview?.pv?.toLocaleString() ?? 0}</div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconPv}`}>
              <EyeOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>独立访客 (UV)</span>
              <div className={styles.statValue}>{overview?.uv?.toLocaleString() ?? 0}</div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconUv}`}>
              <UserOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>人均浏览深度</span>
              <div className={styles.statValue}>
                {overview?.uv ? (overview.pv / overview.uv).toFixed(1) : "0.0"}
              </div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconDepth}`}>
              <CompassOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>影视互动总量</span>
              <div className={styles.statValue}>
                {((overview?.action?.play ?? 0) + (overview?.action?.search ?? 0)).toLocaleString()}
              </div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconAction}`}>
              <PlayCircleOutlined />
            </div>
          </div>
        </div>

        {/* 24 小时流量走势全宽卡片 */}
        <Card title="24 小时流量走势" className={styles.chartCard} loading={loading}>
          {overview?.series && overview.series.length > 0 ? (
            <TrendChart series={overview.series} activeTab="all" />
          ) : (
            <Empty description="暂无流量趋势数据" />
          )}
        </Card>
      </div>

      {/* 板块二：影视与搜索热度 */}
      <div className={styles.sectionBlock}>
        <div className={styles.sectionHeader}>
          <span className={styles.sectionTitle}>
            <FireOutlined style={{ color: "#fa541c" }} />
            影视与搜索热度
          </span>
        </div>

        <BusinessRankings
          playTops={playTops}
          searchTops={searchTops}
          classifyTops={classifyTops}
          loading={loading}
          dayStr={dayStr}
          module="web"
        />
      </div>

      {/* 板块三：页面与终端 */}
      <div className={styles.sectionBlock}>
        <div className={styles.sectionHeader}>
          <span className={styles.sectionTitle}>
            <CompassOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
            页面与终端
          </span>
        </div>

        <div className={styles.rankingsGridRow}>
          <Card
            title={
              <Space>
                <FireOutlined style={{ color: "var(--ant-color-error, #ff4d4f)" }} />
                <span>热门页面 TOP 10</span>
              </Space>
            }
            className={styles.halfCard}
            loading={loading}
          >
            {tops.length === 0 ? (
              <Empty description="暂无访问数据" />
            ) : (
              <div className={styles.topList}>
                {tops.map((item, idx) => {
                  const maxCount = tops[0]?.count || 1;
                  const pct = Math.round((item.count / maxCount) * 100);
                  return (
                    <div key={`${item.key}-${idx}`} className={styles.topListItem}>
                      <div className={styles.topRankBadge} data-top={idx < 3 ? idx + 1 : undefined}>
                        {idx + 1}
                      </div>
                      <div className={styles.topInfo}>
                        <div className={styles.topTitleRow}>
                          {isSafeInternalHref(item.key) ? (
                            <Link href={item.key} target="_blank" className={styles.topPageLink}>
                              {item.key}
                            </Link>
                          ) : (
                            <span className={styles.topPageLink}>{item.key}</span>
                          )}
                          <span className={styles.topCount}>{item.count.toLocaleString()} 次</span>
                        </div>
                        <div className={styles.progressBar}>
                          <div className={styles.progressFill} style={{ width: `${pct}%` }} />
                        </div>
                      </div>
                    </div>
                  );
                })}
              </div>
            )}
          </Card>

          <Card
            title={
              <Space>
                <CompassOutlined style={{ color: "var(--ant-color-info, #1677ff)" }} />
                <span>浏览器分布</span>
              </Space>
            }
            className={styles.halfCard}
            classNames={{ body: styles.centeredCardBody }}
            loading={loading}
          >
            {browserSlices.length === 0 ? (
              <Empty description="暂无分布数据" />
            ) : (
              <DonutChart slices={browserSlices} centerLabel="浏览器" />
            )}
          </Card>

          <Card
            title={
              <Space>
                <DesktopOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
                <span>操作系统分布</span>
              </Space>
            }
            className={styles.halfCard}
            classNames={{ body: styles.centeredCardBody }}
            loading={loading}
          >
            {osSlices.length === 0 ? (
              <Empty description="暂无系统分布" />
            ) : (
              <DonutChart slices={osSlices} centerLabel="操作系统" />
            )}
          </Card>
        </div>
      </div>

      {/* 板块四：实时访问流水 */}
      <div className={styles.sectionBlock}>
        <Card
          title={
            <div className={styles.tableTitleRow}>
              <span style={{ fontWeight: 600 }}>访问流水</span>
              <Space>
                <Input.Search
                  placeholder="搜索页面 / 关键词 / IP"
                  allowClear
                  value={logSearch}
                  onChange={(e) => setLogSearch(e.target.value)}
                  style={{ width: 240 }}
                />
              </Space>
            </div>
          }
          className={styles.tableCard}
        >
          <Table
            rowKey="_key"
            columns={columns}
            dataSource={filteredLogs}
            loading={loading}
            pagination={{ pageSize: 10, showTotal: (t) => `共 ${t} 条记录` }}
            size="middle"
          />
        </Card>
      </div>
    </div>
  );
}
