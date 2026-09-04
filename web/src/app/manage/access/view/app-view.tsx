"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { Card, Empty, Input, Space, Table } from "antd";
import {
  AndroidOutlined,
  AppleOutlined,
  CompassOutlined,
  FireOutlined,
  LineChartOutlined,
  MobileOutlined,
  PlayCircleOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { ApiGet } from "@/lib/client-api";
import TrendChart from "./trend-chart";
import DonutChart, { type DonutSlice } from "./donut-chart";
import BusinessRankings from "./business-rankings";
import type { Overview, TopItem, LogRow } from "./types";
import { PLATFORM_MAP, buildAppLogColumns } from "./app-log-columns";
import styles from "./index.module.less";

const PLATFORMS: { label: string; value: string; icon: React.ReactNode }[] = [
  { label: "全部", value: "all", icon: <MobileOutlined /> },
  { label: "Android", value: "android", icon: <AndroidOutlined /> },
  { label: "HarmonyOS", value: "harmony", icon: <MobileOutlined /> },
  { label: "iOS", value: "ios", icon: <AppleOutlined /> },
];

export default function AppAnalyticsView({ dayStr, refreshKey }: { dayStr: string; refreshKey?: number }) {
  const [platform, setPlatform] = useState<string>("all");
  const [overview, setOverview] = useState<Overview | null>(null);
  const [tops, setTops] = useState<TopItem[]>([]);
  const [playTops, setPlayTops] = useState<TopItem[]>([]);
  const [searchTops, setSearchTops] = useState<TopItem[]>([]);
  const [classifyTops, setClassifyTops] = useState<TopItem[]>([]);
  const [logs, setLogs] = useState<LogRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [logSearch, setLogSearch] = useState("");
  const isFirstMount = useRef(true);
  const lastStateRef = useRef({ dayStr, platform });
  // 竞态防护：静默轮询在途即跳过（避免请求无界叠加），响应只接受最新一批（防止慢响应覆盖新平台/日期结果）
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
      const pParam = platform === "all" ? "" : `&platform=${platform}`;
      const [ovRes, topRes, playRes, searchRes, classifyRes, logRes] = await Promise.all([
        ApiGet<Overview>(`/manage/access/overview?day=${dayStr}&module=app${pParam}`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=app&kind=page${pParam}&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=app&kind=play${pParam}&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=app&kind=search${pParam}&limit=10`),
        ApiGet<{ items: TopItem[] }>(`/manage/access/tops?day=${dayStr}&module=app${pParam}&kind=classify&limit=10`),
        ApiGet<{ list: LogRow[] }>(`/manage/access/logs?day=${dayStr}&module=app${pParam}&limit=100`),
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
          _key: `${item.ts}-${item.path}-${item.page}-${idx}`,
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
  }, [dayStr, platform]);

  useEffect(() => {
    const paramsChanged =
      lastStateRef.current.dayStr !== dayStr || lastStateRef.current.platform !== platform;
    lastStateRef.current = { dayStr, platform };
    const isInitial = isFirstMount.current || paramsChanged;
    isFirstMount.current = false;

    void fetchData(!isInitial);
  }, [fetchData, refreshKey, dayStr, platform]);

  const filteredLogs = logs.filter((log) => {
    if (!logSearch) return true;
    const q = logSearch.toLowerCase();
    return (
      (log.page && log.page.toLowerCase().includes(q)) ||
      (log.path && log.path.toLowerCase().includes(q)) ||
      (log.clientType && log.clientType.toLowerCase().includes(q)) ||
      (log.deviceModel && log.deviceModel.toLowerCase().includes(q)) ||
      (log.appVersion && log.appVersion.toLowerCase().includes(q)) ||
      (log.resource && log.resource.toLowerCase().includes(q)) ||
      (log.resourceTitle && log.resourceTitle.toLowerCase().includes(q)) ||
      (log.deviceId && log.deviceId.toLowerCase().includes(q))
    );
  });

  // 饼图：若是全部 App 显示平台占比；若是单平台显示版本占比
  let donutSlices: DonutSlice[] = [];
  let donutCenterLabel = "平台占比";

  if (platform === "all") {
    donutCenterLabel = "全端分布";
    const platforms = overview?.platforms || {};
    donutSlices = Object.entries(platforms)
      .filter(([_, count]) => count > 0)
      .map(([plat, count]) => {
        const info = PLATFORM_MAP[plat] || { label: plat, color: "#8c8c8c" };
        return {
          name: info.label,
          value: count,
          color: info.color,
        };
      });
  } else {
    donutCenterLabel = "版本分布";
    const versions = overview?.versions || {};
    const colors = [
      "var(--ant-color-info, #1677ff)",
      "var(--ant-color-success, #52c41a)",
      "var(--ant-color-primary, #fa8c16)",
      "var(--ant-color-purple, #722ed1)",
      "var(--ant-color-cyan, #13c2c2)",
    ];
    donutSlices = Object.entries(versions)
      .filter(([_, count]) => count > 0)
      .map(([ver, count], idx) => ({
        name: ver.startsWith("v") ? ver : `v${ver}`,
        value: count,
        color: colors[idx % colors.length],
      }));
  }

  const modelTops = Object.entries(overview?.models || {})
    .filter(([, count]) => count > 0)
    .sort((a, b) => b[1] - a[1])
    .slice(0, 10);

  const columns = buildAppLogColumns();

  return (
    <div className={styles.subModuleWrapper}>
      {/* 平台分流选择器 */}
      <div className={styles.platformSwitcherRow}>
        <div className={styles.pillNavWrapper} role="tablist" aria-label="客户端平台分流">
          {PLATFORMS.map((item) => {
            const active = platform === item.value;
            return (
              <button
                key={item.value}
                type="button"
                role="tab"
                aria-selected={active}
                className={`${styles.pillNavItem} ${active ? styles.pillNavItemActive : ""}`}
                onClick={() => setPlatform(item.value)}
              >
                {item.icon}
                <span>{item.label}</span>
              </button>
            );
          })}
        </div>
      </div>

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
              <span className={styles.statLabel}>页面流转量 (PV)</span>
              <div className={styles.statValue}>{overview?.pv?.toLocaleString() ?? 0}</div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconPv}`}>
              <MobileOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>活跃设备数 (UV)</span>
              <div className={styles.statValue}>{overview?.uv?.toLocaleString() ?? 0}</div>
            </div>
            <div className={`${styles.statIconWrap} ${styles.iconUv}`}>
              <UserOutlined />
            </div>
          </div>

          <div className={styles.statCard}>
            <div className={styles.statBody}>
              <span className={styles.statLabel}>人均屏幕流转数</span>
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
              <span className={styles.statLabel}>业务交互量</span>
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
        <Card
          title="24 小时流转走势"
          className={styles.chartCard}
          loading={loading}
        >
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
          module="app"
          platform={platform}
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
              <Empty description="暂无页面访问数据" />
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
                          <span className={styles.topPageLink}>{item.key}</span>
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
                <MobileOutlined style={{ color: "var(--ant-color-primary, #fa8c16)" }} />
                <span>{platform === "all" ? "平台分布" : "版本分布"}</span>
              </Space>
            }
            className={styles.halfCard}
            classNames={{ body: styles.centeredCardBody }}
            loading={loading}
          >
            {donutSlices.length === 0 ? (
              <Empty description="暂无分布数据" />
            ) : (
              <DonutChart
                slices={donutSlices}
                centerLabel={platform === "all" ? "移动端" : "版本分布"}
              />
            )}
          </Card>

          <Card
            title={
              <Space>
                <MobileOutlined style={{ color: "var(--ant-color-success, #52c41a)" }} />
                <span>机型 TOP 10</span>
              </Space>
            }
            className={styles.halfCard}
            loading={loading}
          >
            {modelTops.length === 0 ? (
              <Empty description="暂无机型数据" />
            ) : (
              <div className={styles.topList}>
                {modelTops.map(([name, count], idx) => {
                  const maxCount = modelTops[0]?.[1] || 1;
                  const pct = Math.round((count / maxCount) * 100);
                  return (
                    <div key={`${name}-${idx}`} className={styles.topListItem}>
                      <div className={styles.topRankBadge} data-top={idx < 3 ? idx + 1 : undefined}>
                        {idx + 1}
                      </div>
                      <div className={styles.topInfo}>
                        <div className={styles.topTitleRow}>
                          <span className={styles.topPageLink}>{name}</span>
                          <span className={styles.topCount}>{count.toLocaleString()} 次</span>
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
        </div>
      </div>

      {/* 板块四：实时页面流转流水 */}
      <div className={styles.sectionBlock}>
        <Card
          title={
            <div className={styles.tableTitleRow}>
              <span style={{ fontWeight: 600 }}>流转流水</span>
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
